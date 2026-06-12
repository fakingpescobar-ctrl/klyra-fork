package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"klyra/pkg/llm"
)

type DiffPatcher struct{}

func (DiffPatcher) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "diff_patch",
		Description: "Apply a unified diff in the workspace. Uses git apply in git repos and a direct patch fallback elsewhere.",
		Parameters: objectSchema(map[string]any{
			"patch": stringProperty("Unified diff patch."),
		}, "patch"),
	}
}

func (DiffPatcher) Run(ctx context.Context, inv Invocation) (Result, error) {
	patch, err := stringArg(inv.Args, "patch")
	if err != nil {
		return Result{}, err
	}

	if isGitRepository(ctx, inv.CWD) {
		result, err := runGitApply(ctx, inv.CWD, patch, 80, "--whitespace=nowarn", "-")
		if err != nil {
			if fallbackErr := applyUnifiedPatch(inv.CWD, patch, false); fallbackErr == nil {
				return Result{Output: "patch applied without git apply"}, nil
			} else {
				return patchFailureResult("patch failed", result, err, fallbackErr), fmt.Errorf("patch failed: git apply: %w; direct patch fallback: %v", err, fallbackErr)
			}
		}
		return Result{Output: "patch applied"}, nil
	}
	if err := applyUnifiedPatch(inv.CWD, patch, false); err != nil {
		return Result{}, fmt.Errorf("patch failed: %w", err)
	}
	return Result{Output: "patch applied without git"}, nil
}

type DiffPreview struct{}

func (DiffPreview) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "diff_preview",
		Description: "Validate a unified diff and return compact diffstat; do not apply.",
		Parameters: objectSchema(map[string]any{
			"patch":     stringProperty("Unified diff patch."),
			"max_lines": integerProperty("Maximum compressed output lines.", 1),
		}, "patch"),
	}
}

func (DiffPreview) Run(ctx context.Context, inv Invocation) (Result, error) {
	patch, err := stringArg(inv.Args, "patch")
	if err != nil {
		return Result{}, err
	}
	maxLines, err := optionalIntArg(inv.Args, "max_lines", 120)
	if err != nil {
		return Result{}, err
	}

	if isGitRepository(ctx, inv.CWD) {
		check, err := runGitApply(ctx, inv.CWD, patch, maxLines, "--check", "--whitespace=nowarn", "-")
		if err != nil {
			if files, fallbackErr := previewUnifiedPatch(inv.CWD, patch); fallbackErr == nil {
				output := "patch check passed without git apply"
				if len(files) > 0 {
					output += "\n" + CompressOutput(strings.Join(files, "\n"), maxLines)
				}
				return Result{Output: output}, nil
			} else {
				return patchFailureResult("patch check failed", check, err, fallbackErr), fmt.Errorf("patch check failed: git apply: %w; direct patch fallback: %v", err, fallbackErr)
			}
		}
		stat, err := runGitApply(ctx, inv.CWD, patch, maxLines, "--stat", "-")
		if err != nil {
			return stat, fmt.Errorf("patch stat failed: %w", err)
		}
		output := "patch check passed"
		if stat.Output != "" {
			output += "\n" + stat.Output
		}
		return Result{Output: output}, nil
	}
	files, err := previewUnifiedPatch(inv.CWD, patch)
	if err != nil {
		return Result{}, fmt.Errorf("patch check failed: %w", err)
	}
	output := "patch check passed"
	if len(files) > 0 {
		output += "\n" + CompressOutput(strings.Join(files, "\n"), maxLines)
	}
	return Result{Output: output}, nil
}

func runGitApply(ctx context.Context, cwd, patch string, maxLines int, args ...string) (Result, error) {
	// Force core.autocrlf=false so patch application never rewrites line endings,
	// regardless of the user's global git config (autocrlf=true is common on Windows).
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.autocrlf=false", "apply"}, args...)...)
	cmd.Dir = cwd
	cmd.Stdin = bytes.NewBufferString(patch)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n" + stderr.String()
	}
	if err != nil {
		return Result{Output: CompressOutput(output, maxLines)}, err
	}
	return Result{Output: CompressOutput(output, maxLines)}, nil
}

func patchFailureResult(prefix string, gitResult Result, gitErr, fallbackErr error) Result {
	lines := []string{prefix}
	if strings.TrimSpace(gitResult.Output) != "" {
		lines = append(lines, "git apply output:", strings.TrimSpace(gitResult.Output))
	}
	if gitErr != nil {
		lines = append(lines, "git apply error: "+gitErr.Error())
	}
	if fallbackErr != nil {
		lines = append(lines, "direct patch fallback error: "+fallbackErr.Error())
	}
	return Result{Output: strings.Join(lines, "\n")}
}
