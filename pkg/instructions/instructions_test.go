package instructions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectInstructionsInStableOrder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "AGENTS.md", "follow agent rules")
	writeFile(t, root, "CLAUDE.md", "follow claude rules")
	writeFile(t, root, ".cursor/rules/go.md", "go rules")

	result, err := Load(root, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 3 {
		t.Fatalf("expected three files, got %+v", result.Files)
	}
	if result.Files[0].Path != "AGENTS.md" || result.Files[1].Path != "CLAUDE.md" || result.Files[2].Path != ".cursor/rules/go.md" {
		t.Fatalf("unexpected order: %+v", result.Files)
	}
	if !strings.Contains(result.Content, "Source: AGENTS.md") || !strings.Contains(result.Content, "go rules") {
		t.Fatalf("unexpected content: %q", result.Content)
	}
}

func TestLoadProjectInstructionsRespectsMaxBytes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "AGENTS.md", strings.Repeat("x", 200))

	result, err := Load(root, 40)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || !result.Files[0].Truncated {
		t.Fatalf("expected truncated result: %+v", result)
	}
	if len(result.Content) > 80 {
		t.Fatalf("expected compact content, got %d bytes", len(result.Content))
	}
}

func writeFile(t *testing.T, root, path, content string) {
	t.Helper()
	target := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
