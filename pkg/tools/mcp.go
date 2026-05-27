package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"klyra/pkg/llm"
)

type MCPServerConfig struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
}

type MCPTool struct {
	server   MCPServerConfig
	toolName string
	spec     llm.ToolSpec
	timeout  time.Duration
}

func RegisterMCPServers(ctx context.Context, registry *Registry, servers []MCPServerConfig) error {
	for _, server := range servers {
		if strings.TrimSpace(server.Command) == "" {
			continue
		}
		if strings.TrimSpace(server.Name) == "" {
			server.Name = "server"
		}
		listCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		specs, err := listMCPTools(listCtx, server)
		cancel()
		if err != nil {
			return fmt.Errorf("mcp %s: %w", server.Name, err)
		}
		for _, spec := range specs {
			toolName := spec.Name
			spec.Name = "mcp_" + sanitizeToolName(server.Name) + "_" + sanitizeToolName(toolName)
			if strings.TrimSpace(spec.Description) == "" {
				spec.Description = "External MCP tool " + toolName + " from server " + server.Name + "."
			} else {
				spec.Description = "External MCP tool " + toolName + " from server " + server.Name + ": " + spec.Description
			}
			registry.Register(MCPTool{
				server:   server,
				toolName: toolName,
				spec:     spec,
				timeout:  60 * time.Second,
			})
		}
	}
	return nil
}

func (m MCPTool) Spec() llm.ToolSpec {
	return m.spec
}

func (m MCPTool) Run(ctx context.Context, inv Invocation) (Result, error) {
	timeout := m.timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, isError, err := callMCPTool(callCtx, m.server, m.toolName, inv.Args)
	if err != nil {
		return Result{Output: output}, err
	}
	if isError {
		return Result{Output: output}, fmt.Errorf("mcp tool %s returned error", m.toolName)
	}
	return Result{Output: output}, nil
}

type mcpSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	stderr bytes.Buffer
	nextID int
}

type mcpRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpListToolsResult struct {
	Tools []mcpToolSpec `json:"tools"`
}

type mcpToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type mcpCallToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

func listMCPTools(ctx context.Context, server MCPServerConfig) ([]llm.ToolSpec, error) {
	session, err := startMCPSession(ctx, server)
	if err != nil {
		return nil, err
	}
	defer session.close()
	if err := session.initialize(ctx); err != nil {
		return nil, err
	}
	var result mcpListToolsResult
	if err := session.request(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	specs := make([]llm.ToolSpec, 0, len(result.Tools))
	for _, tool := range result.Tools {
		schema := tool.InputSchema
		if schema == nil {
			schema = objectSchema(map[string]any{})
		}
		specs = append(specs, llm.ToolSpec{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  schema,
		})
	}
	return specs, nil
}

func callMCPTool(ctx context.Context, server MCPServerConfig, toolName string, args map[string]any) (string, bool, error) {
	session, err := startMCPSession(ctx, server)
	if err != nil {
		return "", false, err
	}
	defer session.close()
	if err := session.initialize(ctx); err != nil {
		return "", false, err
	}
	var result mcpCallToolResult
	err = session.request(ctx, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": args,
	}, &result)
	if err != nil {
		return "", false, err
	}
	var parts []string
	for _, item := range result.Content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "\n"), result.IsError, nil
}

func startMCPSession(ctx context.Context, server MCPServerConfig) (*mcpSession, error) {
	cmd := exec.CommandContext(ctx, server.Command, server.Args...)
	cmd.Env = os.Environ()
	for key, value := range server.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	session := &mcpSession{
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
		nextID: 1,
	}
	cmd.Stderr = &session.stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *mcpSession) initialize(ctx context.Context) error {
	if err := s.request(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "klyra",
			"version": "0.1.0",
		},
	}, nil); err != nil {
		return err
	}
	return s.notify(ctx, "notifications/initialized", map[string]any{})
}

func (s *mcpSession) request(ctx context.Context, method string, params any, out any) error {
	id := s.nextID
	s.nextID++
	if err := s.write(ctx, mcpRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return err
	}
	resp, err := s.read(ctx)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s", method, resp.Error.Message)
	}
	if out != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return err
		}
	}
	return nil
}

func (s *mcpSession) notify(ctx context.Context, method string, params any) error {
	return s.write(ctx, mcpRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (s *mcpSession) write(ctx context.Context, req mcpRequest) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.stdin, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}

func (s *mcpSession) read(ctx context.Context) (mcpResponse, error) {
	select {
	case <-ctx.Done():
		return mcpResponse{}, ctx.Err()
	default:
	}
	headers := map[string]string{}
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			return mcpResponse{}, s.wrapIOError(err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			headers[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		}
	}
	length, err := strconv.Atoi(headers["content-length"])
	if err != nil || length <= 0 {
		return mcpResponse{}, fmt.Errorf("invalid MCP content-length")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(s.reader, data); err != nil {
		return mcpResponse{}, s.wrapIOError(err)
	}
	var resp mcpResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return mcpResponse{}, err
	}
	return resp, nil
}

func (s *mcpSession) wrapIOError(err error) error {
	if s.stderr.Len() > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(s.stderr.String()))
	}
	return err
}

func (s *mcpSession) close() {
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
}

func sanitizeToolName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = regexp.MustCompile(`[^a-z0-9_]+`).ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if name == "" {
		return "tool"
	}
	return name
}
