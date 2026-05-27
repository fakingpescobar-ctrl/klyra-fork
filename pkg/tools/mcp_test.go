package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"klyra/pkg/llm"
)

func TestRegisterMCPServersAddsAndCallsTools(t *testing.T) {
	registry := NewRegistry()
	err := RegisterMCPServers(context.Background(), registry, []MCPServerConfig{{
		Name:    "demo",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestMCPHelperProcess", "--"},
		Env:     map[string]string{"KLYRA_MCP_HELPER": "1"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	specs := registry.Specs()
	if !hasMCPSpec(specs, "mcp_demo_echo") {
		t.Fatalf("expected mcp tool spec, got %+v", specs)
	}
	result, err := registry.RunWithSandbox(context.Background(), t.TempDir(), "workspace-write", llm.ToolCall{
		Name:      "mcp_demo_echo",
		Arguments: map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "echo hello" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}

func TestMCPToolsRequireApprovalAndReadOnlyBlocks(t *testing.T) {
	if !RequiresApproval("mcp_demo_echo") {
		t.Fatal("expected mcp tools to require approval")
	}
	_, err := NewRegistry(MCPTool{
		server:   MCPServerConfig{Name: "demo", Command: os.Args[0]},
		toolName: "echo",
		spec:     llm.ToolSpec{Name: "mcp_demo_echo", Parameters: objectSchema(map[string]any{})},
	}).RunWithSandbox(context.Background(), t.TempDir(), "read-only", llm.ToolCall{Name: "mcp_demo_echo"})
	if err == nil || !strings.Contains(err.Error(), "blocks external MCP tool") {
		t.Fatalf("expected read-only block, got %v", err)
	}
}

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("KLYRA_MCP_HELPER") != "1" {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		req, err := readMCPTestFrame(reader)
		if err != nil {
			os.Exit(0)
		}
		method, _ := req["method"].(string)
		id, hasID := req["id"].(float64)
		if !hasID {
			continue
		}
		switch method {
		case "initialize":
			writeMCPTestFrame(map[string]any{"jsonrpc": "2.0", "id": int(id), "result": map[string]any{"capabilities": map[string]any{}}})
		case "tools/list":
			writeMCPTestFrame(map[string]any{"jsonrpc": "2.0", "id": int(id), "result": map[string]any{
				"tools": []map[string]any{{
					"name":        "echo",
					"description": "Echo text.",
					"inputSchema": objectSchema(map[string]any{"text": stringProperty("Text.")}, "text"),
				}},
			}})
		case "tools/call":
			params, _ := req["params"].(map[string]any)
			args, _ := params["arguments"].(map[string]any)
			text, _ := args["text"].(string)
			writeMCPTestFrame(map[string]any{"jsonrpc": "2.0", "id": int(id), "result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "echo " + text}},
			}})
		default:
			writeMCPTestFrame(map[string]any{"jsonrpc": "2.0", "id": int(id), "error": map[string]any{"code": -32601, "message": "unknown method"}})
		}
	}
}

func hasMCPSpec(specs []llm.ToolSpec, name string) bool {
	for _, spec := range specs {
		if spec.Name == name {
			return true
		}
	}
	return false
}

func readMCPTestFrame(reader *bufio.Reader) (map[string]any, error) {
	headers := map[string]string{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
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
	if err != nil {
		return nil, err
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}
	var req map[string]any
	return req, json.Unmarshal(data, &req)
}

func writeMCPTestFrame(value any) {
	data, _ := json.Marshal(value)
	_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(data), data)
}
