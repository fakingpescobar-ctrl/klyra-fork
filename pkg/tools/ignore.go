package tools

import (
	"os"
	"path/filepath"
	"strings"
)

// loadIgnorePatterns reads .klyra/ignore.md and .agentcli/ignore.md.
// Each non-empty, non-comment line is a glob pattern to exclude from file listings.
func loadIgnorePatterns(cwd string) []string {
	candidates := []string{
		filepath.Join(cwd, ".klyra", "ignore.md"),
		filepath.Join(cwd, ".agentcli", "ignore.md"),
	}
	var patterns []string
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
				continue
			}
			patterns = append(patterns, filepath.ToSlash(line))
		}
	}
	return patterns
}

// matchesIgnorePattern returns true if the slash-normalised relative path matches
// any of the given patterns. Supports glob wildcards and prefix/dir matching.
func matchesIgnorePattern(relPath string, patterns []string) bool {
	rel := filepath.ToSlash(strings.TrimPrefix(relPath, "./"))
	base := filepath.Base(rel)
	for _, pat := range patterns {
		// Full-path glob match
		if ok, _ := filepath.Match(pat, rel); ok {
			return true
		}
		// Base-name glob match (e.g. "*.log" matches "subdir/foo.log")
		if ok, _ := filepath.Match(pat, base); ok {
			return true
		}
		// Directory prefix match: "gen/" excludes everything under gen/
		if strings.HasSuffix(pat, "/") {
			if strings.HasPrefix(rel, pat) || rel == strings.TrimSuffix(pat, "/") {
				return true
			}
		}
		// Exact prefix match without trailing slash
		if !strings.ContainsAny(pat, "*?[") {
			if rel == pat || strings.HasPrefix(rel, pat+"/") {
				return true
			}
		}
	}
	return false
}
