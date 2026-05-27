package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"klyra/pkg/llm"
	"klyra/pkg/policy"
)

type Invocation struct {
	CWD          string
	Sandbox      string
	Mode         string
	ContextFiles []string
	Args         map[string]any
}

type Result struct {
	Output string
}

type Tool interface {
	Spec() llm.ToolSpec
	Run(ctx context.Context, inv Invocation) (Result, error)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry(toolList ...Tool) *Registry {
	registry := &Registry{tools: make(map[string]Tool, len(toolList))}
	for _, tool := range toolList {
		registry.Register(tool)
	}
	return registry
}

func NewDefaultRegistry() *Registry {
	return NewRegistry(
		ProjectMap{},
		GitStatus{},
		GitDiff{},
		WorkspaceCheckpoint{},
		WorkspaceCheckpointList{},
		WorkspaceRestore{},
		PolicyCheck{},
		BashRunner{},
		ListFiles{},
		FileReader{},
		GoSymbolReader{},
		FileWriter{},
		Search{},
		DiffPreview{},
		DiffPatcher{},
	)
}

func (r *Registry) Register(tool Tool) {
	r.tools[tool.Spec().Name] = tool
}

func (r *Registry) Specs() []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, len(r.tools))
	for _, tool := range r.tools {
		specs = append(specs, tool.Spec())
	}
	sortSpecs(specs)
	return specs
}

func (r *Registry) SpecsForTask(task string) []llm.ToolSpec {
	return r.SpecsForTaskMode(task, "", nil)
}

func (r *Registry) SpecsForTaskMode(task, mode string, contextFiles []string) []llm.ToolSpec {
	task = strings.ToLower(task)
	mode = strings.ToLower(strings.TrimSpace(mode))
	names := map[string]bool{
		"project_map":  true,
		"search":       true,
		"read_file":    true,
		"git_status":   true,
		"policy_check": true,
	}
	if mentionsGo(task) {
		names["read_go_symbol"] = true
	}
	if mentionsShell(task) || mentionsTest(task) {
		names["bash"] = true
	}
	if mentionsEdit(task) {
		names["diff_patch"] = true
		names["diff_preview"] = true
		names["write_file"] = true
		names["read_go_symbol"] = true
		names["bash"] = true
		names["git_diff"] = true
		names["workspace_checkpoint"] = true
	}
	switch mode {
	case "inspect":
		delete(names, "bash")
		delete(names, "diff_patch")
		delete(names, "diff_preview")
		delete(names, "git_diff")
		delete(names, "write_file")
		delete(names, "workspace_checkpoint")
		delete(names, "workspace_restore")
	case "repair":
		names["git_diff"] = true
		names["bash"] = true
		delete(names, "workspace_restore")
	case "refactor":
		names["search"] = true
		names["git_diff"] = true
		names["diff_preview"] = true
		names["bash"] = true
		if len(contextFiles) > 0 {
			names["diff_patch"] = true
			names["workspace_checkpoint"] = true
		}
	case "edit":
		if len(contextFiles) == 0 {
			delete(names, "diff_patch")
			delete(names, "write_file")
		}
	}
	if len(task) < 80 && !mentionsEdit(task) && !mentionsShell(task) {
		names["list_files"] = true
	}

	specs := make([]llm.ToolSpec, 0, len(names))
	for name := range names {
		if tool, ok := r.tools[name]; ok {
			specs = append(specs, tool.Spec())
		}
	}
	sortSpecs(specs)
	return specs
}

func (r *Registry) Run(ctx context.Context, cwd string, call llm.ToolCall) (Result, error) {
	return r.RunWithSandbox(ctx, cwd, "", call)
}

func (r *Registry) RunWithSandbox(ctx context.Context, cwd string, sandbox string, call llm.ToolCall) (Result, error) {
	return r.RunWithPolicy(ctx, cwd, sandbox, "", nil, call)
}

func (r *Registry) RunWithPolicy(ctx context.Context, cwd string, sandbox string, mode string, contextFiles []string, call llm.ToolCall) (Result, error) {
	tool, ok := r.tools[call.Name]
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", call.Name)
	}
	if err := enforceMode(mode, contextFiles, call); err != nil {
		return Result{Output: err.Error()}, err
	}
	if err := enforceSandbox(sandbox, call); err != nil {
		return Result{Output: err.Error()}, err
	}
	return tool.Run(ctx, Invocation{CWD: cwd, Sandbox: sandbox, Mode: mode, ContextFiles: contextFiles, Args: call.Arguments})
}

func enforceMode(mode string, contextFiles []string, call llm.ToolCall) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "inspect":
		if isWriteTool(call.Name) {
			return fmt.Errorf("mode inspect blocks %s", call.Name)
		}
	case "edit":
		if isFileWriteTool(call.Name) {
			if len(contextFiles) == 0 {
				return fmt.Errorf("mode edit requires files in context cart before %s", call.Name)
			}
			if path := primaryWritePath(call); path != "" && !pathAllowed(path, contextFiles) {
				return fmt.Errorf("mode edit blocks %s outside context cart: %s", call.Name, path)
			}
		}
	case "refactor":
		if call.Name == "diff_patch" && len(contextFiles) == 0 {
			return fmt.Errorf("mode refactor requires context cart and dry-run evidence before diff_patch")
		}
	}
	return nil
}

func isWriteTool(name string) bool {
	return name == "write_file" || name == "diff_patch" || name == "workspace_restore" || name == "bash"
}

func isFileWriteTool(name string) bool {
	return name == "write_file" || name == "diff_patch"
}

func primaryWritePath(call llm.ToolCall) string {
	if call.Name == "write_file" {
		path, _ := call.Arguments["path"].(string)
		return path
	}
	if call.Name == "diff_patch" {
		patch, _ := call.Arguments["patch"].(string)
		return firstPatchPath(patch)
	}
	return ""
}

func firstPatchPath(patch string) string {
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			return strings.TrimPrefix(strings.TrimSpace(line), "+++ b/")
		}
	}
	return ""
}

func pathAllowed(path string, contextFiles []string) bool {
	path = strings.Trim(strings.ReplaceAll(path, "\\", "/"), "./")
	for _, allowed := range contextFiles {
		allowed = strings.Trim(strings.ReplaceAll(allowed, "\\", "/"), "./")
		if path == allowed {
			return true
		}
	}
	return false
}

func enforceSandbox(sandbox string, call llm.ToolCall) error {
	profile := policy.NormalizeSandbox(sandbox)
	switch call.Name {
	case "write_file", "diff_patch", "workspace_restore":
		if profile == policy.SandboxReadOnly {
			return fmt.Errorf("sandbox %s blocks %s", profile, call.Name)
		}
	case "bash":
		command, _ := call.Arguments["command"].(string)
		assessment := policy.AssessShellCommand(command)
		if ok, reason := policy.IsAllowedInSandbox(assessment, profile); !ok {
			return fmt.Errorf("sandbox %s blocks bash command: %s", profile, reason)
		}
	}
	return nil
}

func sortSpecs(specs []llm.ToolSpec) {
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
}

func mentionsGo(task string) bool {
	return strings.Contains(task, ".go") || strings.Contains(task, " go ") || strings.Contains(task, "golang")
}

func mentionsShell(task string) bool {
	return containsAny(task, []string{"run ", "запусти", "команд", "bash", "shell", "terminal", "build", "сбор", "lint", "test", "тест"})
}

func mentionsTest(task string) bool {
	return containsAny(task, []string{"test", "тест", "verify", "проверь", "validation", "smoke"})
}

func mentionsEdit(task string) bool {
	return containsAny(task, []string{
		"implement", "add ", "fix", "change", "edit", "write", "refactor", "delete",
		"реализ", "добав", "исправ", "измени", "поправ", "напиши", "удали", "рефактор",
	})
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func RequiresApproval(name string) bool {
	switch name {
	case "bash", "write_file", "diff_patch", "workspace_restore":
		return true
	default:
		return false
	}
}
