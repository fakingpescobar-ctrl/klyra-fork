package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"klyra/pkg/llm"
)

func TestWebSearchParsesResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "klyra" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><a class="result__a" href="https://example.com/a">Example <b>A</b></a></html>`))
	}))
	defer server.Close()

	result, err := WebSearch{Endpoint: server.URL + "?q=%s", Client: server.Client()}.Run(context.Background(), Invocation{
		Args: map[string]any{"query": "klyra", "max_results": 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "Example A - https://example.com/a") {
		t.Fatalf("unexpected output: %s", result.Output)
	}
}

func TestWebSearchRespectsCallerCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := WebSearch{Endpoint: server.URL + "?q=%s", Client: server.Client()}.Run(ctx, Invocation{
		Args: map[string]any{"query": "klyra", "max_results": 3},
	})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("cancellation took too long: %s", elapsed)
	}
}

func TestFetchURLConvertsHTMLToText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><head><style>.x{}</style></head><body><h1>Title</h1><p>Hello &amp; world</p></body></html>`))
	}))
	defer server.Close()

	result, err := FetchURL{Client: server.Client()}.Run(context.Background(), Invocation{
		Args: map[string]any{"url": server.URL, "max_bytes": 1000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "Title") || !strings.Contains(result.Output, "Hello & world") || strings.Contains(result.Output, ".x") {
		t.Fatalf("unexpected output: %s", result.Output)
	}
}

func TestWebToolsExposedForInternetTasks(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTask("найди актуальную информацию в интернете")
	if !hasToolSpec(specs, "web_search") || !hasToolSpec(specs, "fetch_url") {
		t.Fatalf("expected web tools, got %+v", specs)
	}
}

func TestWebToolsAlwaysMatchSystemPrompt(t *testing.T) {
	specs := NewDefaultRegistry().SpecsForTask("что ты умеешь")
	if !hasToolSpec(specs, "web_search") || !hasToolSpec(specs, "fetch_url") {
		t.Fatalf("system prompt mentions web tools, so they must always be exposed: %+v", specs)
	}
}

func hasToolSpec(specs []llm.ToolSpec, name string) bool {
	for _, spec := range specs {
		if spec.Name == name {
			return true
		}
	}
	return false
}
