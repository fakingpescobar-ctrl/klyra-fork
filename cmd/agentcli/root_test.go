package agentcli

import (
	"strings"
	"testing"

	"agentcli/pkg/llm"
)

func TestTUILinesFromMessagesRestoresAssistantReasoning(t *testing.T) {
	lines := tuiLinesFromMessages([]llm.Message{
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "done", Reasoning: "## Plan\n\n- inspect"},
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "thoughts:0:## Plan\n\n- inspect") {
		t.Fatalf("stored reasoning was not restored as thoughts: %#v", lines)
	}
	if !strings.Contains(joined, "agent: done") {
		t.Fatalf("assistant output missing: %#v", lines)
	}
}
