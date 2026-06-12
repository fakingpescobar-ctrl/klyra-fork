package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"klyra/pkg/llm"
)

type Search struct{}

func (Search) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "search",
		Description: "Search workspace text with ripgrep; skips secrets, sessions, generated, and dependency dirs.",
		Parameters: objectSchema(map[string]any{
			"pattern":   stringProperty("Search pattern."),
			"glob":      stringProperty("Optional file glob."),
			"max_lines": integerProperty("Maximum output lines.", 1),
		}, "pattern"),
	}
}

func (Search) Run(ctx context.Context, inv Invocation) (Result, error) {
	pattern, err := stringArg(inv.Args, "pattern")
	if err != nil {
		return Result{}, err
	}
	glob, err := optionalStringArg(inv.Args, "glob", "")
	if err != nil {
		return Result{}, err
	}
	maxLines, err := optionalIntArg(inv.Args, "max_lines", 120)
	if err != nil {
		return Result{}, err
	}

	ignorePatterns := loadIgnorePatterns(inv.CWD)
	args := defaultSearchArgs(pattern, glob, ignorePatterns)
	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = inv.CWD
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n" + stderr.String()
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return Result{Output: "no matches"}, nil
		}
		return Result{Output: CompressOutput(output, maxLines)}, fmt.Errorf("search failed: %w", err)
	}
	return Result{Output: CompressOutput(output, maxLines)}, nil
}

func defaultSearchArgs(pattern, userGlob string, ignorePatterns []string) []string {
	args := []string{"--line-number", "--hidden"}
	for _, glob := range []string{
		"!.git",
		"!.agentcli",
		"!node_modules",
		"!dist",
		"!build",
		"!.cache",
		"!.next",
		"!vendor",
		"!.env",
		"!.env.*",
		"!*.pem",
		"!*.key",
		"!*.p12",
		"!*.pfx",
	} {
		args = append(args, "--glob", glob)
	}
	for _, pat := range ignorePatterns {
		if !strings.HasPrefix(pat, "!") {
			pat = "!" + pat
		}
		args = append(args, "--glob", pat)
	}
	if userGlob != "" {
		args = append(args, "--glob", userGlob)
	}
	return append(args, pattern)
}
