package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGeminiProviderParsesFunctionCalls(t *testing.T) {
	var captured geminiRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-test:generateContent" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Fatalf("missing api key")
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {
					"role": "model",
					"parts": [
						{"text": "checking"},
						{"functionCall": {"id": "fc_123", "name": "project_map", "args": {"max_files": 20}}, "thoughtSignature": "sig-a"}
					]
				}
			}],
			"usageMetadata": {
				"promptTokenCount": 100,
				"cachedContentTokenCount": 25,
				"candidatesTokenCount": 12,
				"thoughtsTokenCount": 3,
				"totalTokenCount": 115
			}
		}`))
	}))
	defer server.Close()

	provider, err := NewGeminiProvider("test-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := provider.Complete(context.Background(), Request{
		Model:           "models/gemini-test",
		MaxOutputTokens: 512,
		Messages: []Message{
			{Role: RoleSystem, Content: "system prompt"},
			{Role: RoleUser, Content: "inspect"},
		},
		Tools: []ToolSpec{{
			Name:        "project_map",
			Description: "Map project",
			Parameters:  map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured.SystemInstruction.Parts[0].Text != "system prompt" {
		t.Fatalf("system prompt was not sent: %+v", captured.SystemInstruction)
	}
	if captured.GenerationConfig.MaxOutputTokens != 512 {
		t.Fatalf("max output tokens were not sent: %+v", captured.GenerationConfig)
	}
	if len(captured.Tools) != 1 || captured.Tools[0].FunctionDeclarations[0].Name != "project_map" {
		t.Fatalf("tool schema was not sent: %+v", captured.Tools)
	}
	if resp.Content != "checking" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "fc_123" || resp.ToolCalls[0].Name != "project_map" {
		t.Fatalf("unexpected tool call: %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].ProviderMetadata["thoughtSignature"] != "sig-a" {
		t.Fatalf("thought signature was not preserved: %+v", resp.ToolCalls[0])
	}
	if resp.Usage.CachedTokens != 25 || resp.Usage.ReasoningTokens != 3 || resp.Usage.TotalTokens != 115 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestGeminiContentsIncludeFunctionResponses(t *testing.T) {
	messages := geminiContents([]Message{
		{Role: RoleAssistant, Content: "checking", ToolCalls: []ToolCall{{
			ID:        "call_123",
			Name:      "read_file",
			Arguments: map[string]any{"path": "main.go"},
			ProviderMetadata: map[string]any{
				"thoughtSignature": "sig-a",
			},
		}}},
		{Role: RoleTool, ToolCallID: "call_123", Content: "file contents"},
	})
	if len(messages) != 2 {
		t.Fatalf("unexpected messages: %+v", messages)
	}
	if messages[0].Role != "model" || messages[0].Parts[1].FunctionCall == nil {
		t.Fatalf("function call was not mapped: %+v", messages[0])
	}
	if messages[0].Parts[1].ThoughtSignature != "sig-a" {
		t.Fatalf("thought signature was not mapped: %+v", messages[0])
	}
	response := messages[1].Parts[0].FunctionResponse
	if response == nil || response.ID != "call_123" || response.Name != "read_file" || response.Response["output"] != "file contents" {
		t.Fatalf("function response was not mapped: %+v", messages[1])
	}
}

func TestGeminiContentsIncludeImages(t *testing.T) {
	messages := geminiContents([]Message{{
		Role:    RoleUser,
		Content: "describe",
		Attachments: []Attachment{{
			Type:     "image",
			MIMEType: "image/jpeg",
			Name:     "photo.jpg",
			Data:     "aW1hZ2U=",
		}},
	}})
	if len(messages) != 1 || len(messages[0].Parts) != 2 {
		t.Fatalf("expected text and image parts, got %+v", messages)
	}
	image := messages[0].Parts[1].InlineData
	if image == nil || image.MIMEType != "image/jpeg" || image.Data != "aW1hZ2U=" {
		t.Fatalf("unexpected image part: %+v", messages[0].Parts[1])
	}
}
