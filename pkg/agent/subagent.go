package agent

import (
	"context"
	"fmt"
	"strings"

	"klyra/pkg/llm"
	"klyra/pkg/tools"
)

// SubAgentFactory is a function that creates and runs a child agent for a given task.
type SubAgentFactory func(ctx context.Context, task, mode string, contextFiles []string) (string, error)

// subAgentTool implements tools.Tool and spawns child agents via a SubAgentFactory.
type subAgentTool struct {
	factory SubAgentFactory
}

func (t subAgentTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "sub_agent",
		Description: "Spawn a focused child agent to handle a specific subtask independently. " +
			"Use when the task can be split into parallel independent workstreams, or when isolating " +
			"a subtask from the current context reduces noise. The child agent has its own tool calls " +
			"and context; its final answer is returned as this tool's output. " +
			"Sub-agents cannot spawn further sub-agents.",
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"task"},
			"properties": map[string]any{
				"task": map[string]any{
					"type":        "string",
					"description": "The self-contained task for the child agent. Be precise — it has no access to the parent conversation.",
				},
				"mode": map[string]any{
					"type":        "string",
					"description": "Agent mode: edit, inspect, plan, repair, or refactor. Defaults to the parent's mode.",
					"enum":        []string{"edit", "inspect", "plan", "repair", "refactor"},
				},
				"context_files": map[string]any{
					"type":        "array",
					"description": "Files the child agent is allowed to modify (required for edit/refactor modes).",
					"items":       map[string]any{"type": "string"},
				},
			},
		},
	}
}

func (t subAgentTool) Run(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	task, _ := inv.Args["task"].(string)
	if strings.TrimSpace(task) == "" {
		return tools.Result{}, fmt.Errorf("sub_agent: task is required")
	}
	mode, _ := inv.Args["mode"].(string)
	if mode == "" {
		mode = inv.Mode
	}
	var contextFiles []string
	if raw, ok := inv.Args["context_files"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				contextFiles = append(contextFiles, strings.TrimSpace(s))
			}
		}
	}
	result, err := t.factory(ctx, task, mode, contextFiles)
	return tools.Result{Output: result}, err
}

// DefaultSubAgentFactory returns a SubAgentFactory that spawns real child agents
// sharing the parent's provider and tools. Child agents are capped at 10 steps
// and cannot spawn further sub-agents (SubAgentFactory is nil in child config).
// Child output is discarded — the result is returned as the tool output string.
func DefaultSubAgentFactory(parentCfg Config) SubAgentFactory {
	return func(ctx context.Context, task, mode string, contextFiles []string) (string, error) {
		childCfg := parentCfg
		childCfg.Mode = mode
		childCfg.ContextFiles = contextFiles
		childCfg.SubAgentFactory = nil  // prevent infinite recursion
		childCfg.Output = nil           // result returns as string, not written to parent output
		childCfg.Input = nil            // child needs no interactive input
		childCfg.StreamHandler = nil    // no streaming to parent
		childCfg.ReasoningHandler = nil // no reasoning forwarding
		childCfg.ToolProgress = nil     // no tool progress forwarding
		childCfg.Approver = nil         // child auto-approves (parent already decided to delegate)
		if childCfg.MaxSteps > 10 {
			childCfg.MaxSteps = 10
		}
		child, err := New(childCfg)
		if err != nil {
			return "", fmt.Errorf("sub_agent setup failed: %w", err)
		}
		return child.Run(ctx, task)
	}
}
