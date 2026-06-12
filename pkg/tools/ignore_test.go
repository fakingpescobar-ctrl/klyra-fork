package tools

import (
	"strings"
	"testing"
)

func TestLoadIgnorePatternsSkipsCommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, ".klyra/ignore.md", "# comment\n\ndist/\n// also a comment\n*.generated.go\n  trimmed.txt  \n")

	patterns := loadIgnorePatterns(dir)
	want := map[string]bool{"dist/": true, "*.generated.go": true, "trimmed.txt": true}
	if len(patterns) != len(want) {
		t.Fatalf("expected %d patterns, got %d: %v", len(want), len(patterns), patterns)
	}
	for _, p := range patterns {
		if !want[p] {
			t.Fatalf("unexpected pattern %q in %v", p, patterns)
		}
	}
}

func TestLoadIgnorePatternsMergesBothLocations(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, ".klyra/ignore.md", "from-klyra/\n")
	writeTestFile(t, dir, ".agentcli/ignore.md", "from-agentcli/\n")

	patterns := loadIgnorePatterns(dir)
	seen := map[string]bool{}
	for _, p := range patterns {
		seen[p] = true
	}
	if !seen["from-klyra/"] || !seen["from-agentcli/"] {
		t.Fatalf("expected patterns from both files, got %v", patterns)
	}
}

func TestLoadIgnorePatternsMissingFilesIsEmpty(t *testing.T) {
	if patterns := loadIgnorePatterns(t.TempDir()); len(patterns) != 0 {
		t.Fatalf("expected no patterns for empty dir, got %v", patterns)
	}
}

func TestMatchesIgnorePattern(t *testing.T) {
	cases := []struct {
		name     string
		rel      string
		patterns []string
		want     bool
	}{
		{"glob on full path", "src/app.generated.go", []string{"*.generated.go"}, true},
		{"glob on basename", "deep/nested/x.min.js", []string{"*.min.js"}, true},
		{"dir suffix prefix match", "dist/bundle.js", []string{"dist/"}, true},
		{"dir suffix exact dir", "dist", []string{"dist/"}, true},
		{"literal path exact", "config.yaml", []string{"config.yaml"}, true},
		{"literal path as dir prefix", "vendor/lib/x.go", []string{"vendor"}, true},
		{"leading ./ is stripped", "./dist/x.js", []string{"dist/"}, true},
		{"no match", "src/main.go", []string{"dist/", "*.min.js"}, false},
		{"empty patterns", "src/main.go", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesIgnorePattern(tc.rel, tc.patterns); got != tc.want {
				t.Fatalf("matchesIgnorePattern(%q, %v) = %v, want %v", tc.rel, tc.patterns, got, tc.want)
			}
		})
	}
}

func TestListFilesRespectsIgnorePatterns(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, ".klyra/ignore.md", "ignored/\n*.tmp\n")
	writeTestFile(t, dir, "keep.go", "package main\n")
	writeTestFile(t, dir, "scratch.tmp", "junk\n")
	writeTestFile(t, dir, "ignored/secret.go", "package secret\n")

	result, err := ListFiles{}.Run(t.Context(), Invocation{
		CWD:  dir,
		Args: map[string]any{"max_files": 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "keep.go") {
		t.Fatalf("expected keep.go in output:\n%s", result.Output)
	}
	if strings.Contains(result.Output, "scratch.tmp") || strings.Contains(result.Output, "secret.go") {
		t.Fatalf("ignored files leaked into output:\n%s", result.Output)
	}
}
