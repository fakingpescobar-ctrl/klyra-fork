package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"klyra/pkg/llm"
)

type ReplaceLines struct{}

func (ReplaceLines) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "replace_lines",
		Description: "Replace a small inclusive line range. Prefer over write_file for focused edits.",
		Parameters: objectSchema(map[string]any{
			"path":       stringProperty("Relative file path."),
			"start_line": integerProperty("1-based first line to replace.", 1),
			"end_line":   integerProperty("1-based last line to replace, inclusive.", 1),
			"content":    stringProperty("Replacement text for the range."),
		}, "path", "start_line", "end_line", "content"),
	}
}

func (ReplaceLines) Run(_ context.Context, inv Invocation) (Result, error) {
	requestedPath, err := stringArg(inv.Args, "path")
	if err != nil {
		return Result{}, err
	}
	startLine, err := optionalIntArg(inv.Args, "start_line", 0)
	if err != nil {
		return Result{}, err
	}
	endLine, err := optionalIntArg(inv.Args, "end_line", 0)
	if err != nil {
		return Result{}, err
	}
	content, err := stringArg(inv.Args, "content")
	if err != nil {
		return Result{}, err
	}
	if startLine <= 0 || endLine < startLine {
		return Result{}, fmt.Errorf("invalid line range %d-%d", startLine, endLine)
	}
	target, err := safeWorkspacePath(inv.CWD, requestedPath)
	if err != nil {
		return Result{}, err
	}
	lines, finalNewline, err := readEditableLines(target)
	if err != nil {
		return Result{}, err
	}
	if endLine > len(lines) {
		return Result{}, fmt.Errorf("line range %d-%d exceeds file length %d", startLine, endLine, len(lines))
	}
	replacement := editContentLines(content)
	next := make([]string, 0, len(lines)-(endLine-startLine+1)+len(replacement))
	next = append(next, lines[:startLine-1]...)
	next = append(next, replacement...)
	next = append(next, lines[endLine:]...)
	if err := writeEditableLines(target, next, finalNewline || strings.HasSuffix(content, "\n")); err != nil {
		return Result{}, err
	}
	return Result{Output: fmt.Sprintf("replaced %s lines %d-%d with %d line(s)", requestedPath, startLine, endLine, len(replacement))}, nil
}

type InsertLines struct{}

func (InsertLines) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "insert_lines",
		Description: "Insert a small text block after a line. Use after_line=0 for file start.",
		Parameters: objectSchema(map[string]any{
			"path":       stringProperty("Relative file path."),
			"after_line": integerProperty("Insert after this 1-based line; use 0 for file start.", 0),
			"content":    stringProperty("Text to insert."),
		}, "path", "after_line", "content"),
	}
}

func (InsertLines) Run(_ context.Context, inv Invocation) (Result, error) {
	requestedPath, err := stringArg(inv.Args, "path")
	if err != nil {
		return Result{}, err
	}
	afterLine, err := optionalIntArg(inv.Args, "after_line", 0)
	if err != nil {
		return Result{}, err
	}
	content, err := stringArg(inv.Args, "content")
	if err != nil {
		return Result{}, err
	}
	target, err := safeWorkspacePath(inv.CWD, requestedPath)
	if err != nil {
		return Result{}, err
	}
	lines, finalNewline, err := readEditableLines(target)
	if err != nil {
		return Result{}, err
	}
	if afterLine < 0 || afterLine > len(lines) {
		return Result{}, fmt.Errorf("after_line %d outside file length %d", afterLine, len(lines))
	}
	inserted := editContentLines(content)
	next := make([]string, 0, len(lines)+len(inserted))
	next = append(next, lines[:afterLine]...)
	next = append(next, inserted...)
	next = append(next, lines[afterLine:]...)
	if err := writeEditableLines(target, next, finalNewline || strings.HasSuffix(content, "\n")); err != nil {
		return Result{}, err
	}
	return Result{Output: fmt.Sprintf("inserted %d line(s) into %s after line %d", len(inserted), requestedPath, afterLine)}, nil
}

type ReplaceSymbol struct{}

func (ReplaceSymbol) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "replace_symbol",
		Description: "Replace one function/class/type symbol by AST range. Best for function-level edits.",
		Parameters: objectSchema(map[string]any{
			"path":    stringProperty("Relative source file path."),
			"symbol":  stringProperty("Symbol name from project_map ast_symbols, e.g. UserCard or Server.Login."),
			"content": stringProperty("Replacement source for the whole symbol."),
		}, "path", "symbol", "content"),
	}
}

func (ReplaceSymbol) Run(ctx context.Context, inv Invocation) (Result, error) {
	requestedPath, err := stringArg(inv.Args, "path")
	if err != nil {
		return Result{}, err
	}
	symbol, err := stringArg(inv.Args, "symbol")
	if err != nil {
		return Result{}, err
	}
	content, err := stringArg(inv.Args, "content")
	if err != nil {
		return Result{}, err
	}
	target, err := safeWorkspacePath(inv.CWD, requestedPath)
	if err != nil {
		return Result{}, err
	}
	parseCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	start, end, ok, err := findASTSymbolRange(parseCtx, target, filepath.ToSlash(requestedPath), symbol)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{Output: "symbol not found"}, nil
	}
	result, err := ReplaceLines{}.Run(ctx, Invocation{
		CWD: inv.CWD,
		Args: map[string]any{
			"path":       requestedPath,
			"start_line": start,
			"end_line":   end,
			"content":    content,
		},
	})
	if err != nil {
		return result, err
	}
	return Result{Output: fmt.Sprintf("replaced symbol %s in %s lines %d-%d", symbol, requestedPath, start, end)}, nil
}

func readEditableLines(path string) ([]string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	finalNewline := strings.HasSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil, finalNewline, nil
	}
	return strings.Split(text, "\n"), finalNewline, nil
}

func writeEditableLines(path string, lines []string, finalNewline bool) error {
	text := strings.Join(lines, "\n")
	if finalNewline {
		text += "\n"
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func editContentLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}
