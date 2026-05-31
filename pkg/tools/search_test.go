package tools

import (
	"context"
	"strings"
	"testing"
)

func TestSearchSkipsSecretsAndSessionState(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "src/app.go", "package main\n// needle visible\n")
	writeTestFile(t, dir, ".env", "SECRET_TOKEN=needle\n")
	writeTestFile(t, dir, ".agentcli/sessions/run.json", `{"content":"needle"}`)

	result, err := Search{}.Run(context.Background(), Invocation{
		CWD: dir,
		Args: map[string]any{
			"pattern":   "needle",
			"max_lines": 20,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "src/app.go") {
		t.Fatalf("expected visible match, got:\n%s", result.Output)
	}
	if strings.Contains(result.Output, ".env") || strings.Contains(result.Output, ".agentcli") || strings.Contains(result.Output, "SECRET_TOKEN") {
		t.Fatalf("search leaked skipped files:\n%s", result.Output)
	}
}

func TestDiscoverySkipsSecretFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", "package main\n")
	writeTestFile(t, dir, ".env", "TOKEN=secret\n")
	writeTestFile(t, dir, "cert.pem", "secret\n")

	result, err := ListFiles{}.Run(context.Background(), Invocation{
		CWD:  dir,
		Args: map[string]any{"max_files": 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "main.go") || strings.Contains(result.Output, ".env") || strings.Contains(result.Output, "cert.pem") {
		t.Fatalf("unexpected file list:\n%s", result.Output)
	}
}
