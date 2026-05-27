package tools

import (
	"context"
	"strings"
	"testing"
)

func TestFileOutlineReturnsCompactASTSummary(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "component.tsx", `import React from "react";

export interface Props {
  name: string
}

export function UserCard(props: Props) {
  return <section>{props.name}</section>
}
`)

	result, err := FileOutline{}.Run(context.Background(), Invocation{
		CWD:  dir,
		Args: map[string]any{"path": "component.tsx"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "language: tsx") || !strings.Contains(result.Output, "UserCard") {
		t.Fatalf("expected compact TSX outline:\n%s", result.Output)
	}
}

func TestReadSymbolReturnsOnlyRequestedSymbolWindow(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "worker.py", `import pathlib

class Runner:
    def run(self):
        return pathlib.Path(".")

def unrelated():
    return "skip"
`)

	result, err := SymbolReader{}.Run(context.Background(), Invocation{
		CWD: dir,
		Args: map[string]any{
			"path":          "worker.py",
			"symbol":        "Runner",
			"context_lines": 0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "class Runner") || strings.Contains(result.Output, "def unrelated") {
		t.Fatalf("expected only Runner symbol window:\n%s", result.Output)
	}
}
