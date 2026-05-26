package instructions

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const DefaultMaxBytes = 12_000

type File struct {
	Path      string
	Bytes     int
	Truncated bool
}

type Result struct {
	Content   string
	Files     []File
	Bytes     int
	Truncated bool
}

func Load(cwd string, maxBytes int) (Result, error) {
	root, err := filepath.Abs(cwd)
	if err != nil {
		return Result{}, err
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	paths, err := candidatePaths(root)
	if err != nil {
		return Result{}, err
	}

	var result Result
	var sections []string
	for _, path := range paths {
		if result.Bytes >= maxBytes {
			result.Truncated = true
			break
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Result{}, fmt.Errorf("read project instructions %s: %w", path, err)
		}
		content := strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
		if content == "" {
			continue
		}
		remaining := maxBytes - result.Bytes
		truncated := false
		if len(content) > remaining {
			content = trimToBytes(content, remaining)
			truncated = true
			result.Truncated = true
		}
		result.Files = append(result.Files, File{Path: path, Bytes: len(content), Truncated: truncated})
		result.Bytes += len(content)
		sections = append(sections, fmt.Sprintf("Source: %s\n%s", path, content))
	}
	if len(sections) > 0 {
		result.Content = strings.Join(sections, "\n\n")
	}
	return result, nil
}

func candidatePaths(root string) ([]string, error) {
	ordered := []string{
		"AGENTS.md",
		"CLAUDE.md",
		"GEMINI.md",
		".agentcli/instructions.md",
		".agentcli/rules.md",
		".cursorrules",
		".github/copilot-instructions.md",
	}
	seen := map[string]bool{}
	var out []string
	for _, path := range ordered {
		if fileExists(filepath.Join(root, path)) {
			out = append(out, filepath.ToSlash(path))
			seen[filepath.ToSlash(path)] = true
		}
	}

	cursorRules, err := filepath.Glob(filepath.Join(root, ".cursor", "rules", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(cursorRules)
	for _, path := range cursorRules {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			out = append(out, rel)
			seen[rel] = true
		}
	}
	return out, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func trimToBytes(content string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(content) <= maxBytes {
		return content
	}
	marker := "[truncated]"
	if maxBytes <= len(marker) {
		return marker[:maxBytes]
	}
	limit := maxBytes - len(marker) - 1
	content = content[:limit]
	if idx := strings.LastIndexByte(content, '\n'); idx > limit/2 {
		content = content[:idx]
	}
	out := strings.TrimSpace(content) + "\n" + marker
	if len(out) > maxBytes {
		return out[:maxBytes]
	}
	return out
}
