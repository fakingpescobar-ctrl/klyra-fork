package agentcli

import (
	"strings"
	"testing"

	appconfig "agentcli/pkg/config"
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

func TestServiceMarkdownAvoidsUnsupportedHeaders(t *testing.T) {
	settings := formatTUISettings(appconfig.Default(), nil)
	cart := formatContextCart(nil)
	attachments := formatAttachments(nil)
	for name, text := range map[string]string{
		"settings":    settings,
		"cart":        cart,
		"attachments": attachments,
	} {
		if strings.Contains(text, "##") {
			t.Fatalf("%s output should not contain markdown headers:\n%s", name, text)
		}
	}
}

func TestTUILinesFromMessagesRestoresStats(t *testing.T) {
	lines := tuiLinesFromMessages([]llm.Message{
		{Role: llm.RoleUser, Content: "hello"},
		{
			Role:            llm.RoleAssistant,
			Content:         "done",
			DurationSeconds: 2.5,
			Usage: &llm.Usage{
				InputTokens:     1000,
				CachedTokens:    200,
				OutputTokens:    150,
				ReasoningTokens: 50,
				TotalTokens:     1150,
			},
		},
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "stats: duration=2.5s input=1000 cached=200 output=150 reasoning=50 total=1150") {
		t.Fatalf("stored stats were not restored properly: %#v", lines)
	}
}
