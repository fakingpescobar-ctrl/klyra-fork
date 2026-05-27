package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileReaderLineSlice(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FileReader{}.Run(context.Background(), Invocation{
		CWD: dir,
		Args: map[string]any{
			"path":       "sample.txt",
			"start_line": 2,
			"max_lines":  1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Output) != "2: two" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}

func TestCreateFileStoresDescriptionForListFiles(t *testing.T) {
	dir := t.TempDir()
	result, err := FileCreator{}.Run(context.Background(), Invocation{
		CWD: dir,
		Args: map[string]any{
			"path":        "internal/plan.txt",
			"content":     "ship it\n",
			"description": "Implementation checklist for the agent",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "description: Implementation checklist") {
		t.Fatalf("expected description in create output: %s", result.Output)
	}

	list, err := ListFiles{}.Run(context.Background(), Invocation{
		CWD:  dir,
		Args: map[string]any{"max_files": 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list.Output, "internal/plan.txt\t# Implementation checklist for the agent") {
		t.Fatalf("expected file note next to file, got:\n%s", list.Output)
	}
	if strings.Contains(list.Output, ".agentcli") {
		t.Fatalf("internal notes should stay hidden from list_files:\n%s", list.Output)
	}
}
