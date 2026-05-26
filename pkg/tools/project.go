package tools

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agentcli/pkg/llm"
)

type ProjectMap struct{}

func (ProjectMap) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "project_map",
		Description: "Return a token-budgeted repo map with important files, Go symbols, imports, and likely relevant slices. Use this before broad exploration.",
		Parameters: objectSchema(map[string]any{
			"max_files":  integerProperty("Maximum important files to include.", 1),
			"max_tokens": integerProperty("Approximate token budget for the map.", 1),
			"focus":      stringProperty("Optional task/query to rank relevant files and symbols."),
		}),
	}
}

func (ProjectMap) Run(_ context.Context, inv Invocation) (Result, error) {
	maxFiles, err := optionalIntArg(inv.Args, "max_files", 80)
	if err != nil {
		return Result{}, err
	}
	maxTokens, err := optionalIntArg(inv.Args, "max_tokens", 1000)
	if err != nil {
		return Result{}, err
	}
	focus, _ := inv.Args["focus"].(string)

	var files []string
	byExt := map[string]int{}
	totalBytes := int64(0)
	err = filepath.WalkDir(inv.CWD, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && shouldSkipDir(entry.Name()) && path != inv.CWD {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		if shouldSkipFile(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err == nil {
			totalBytes += info.Size()
		}
		rel, err := filepath.Rel(inv.CWD, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		ext := filepath.Ext(rel)
		if ext == "" {
			ext = "[no extension]"
		}
		byExt[ext]++
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	sort.Strings(files)
	important := importantFiles(files, maxFiles, focus)
	goSymbols := goSymbolSummaries(inv.CWD, important, focus)
	var out []string
	out = append(out, fmt.Sprintf("root: %s", inv.CWD))
	out = append(out, fmt.Sprintf("files: %d", len(files)))
	out = append(out, fmt.Sprintf("bytes: %d", totalBytes))
	if strings.TrimSpace(focus) != "" {
		out = append(out, fmt.Sprintf("focus: %s", focus))
	}
	out = append(out, "languages/extensions:")
	for _, pair := range sortedCounts(byExt) {
		out = append(out, fmt.Sprintf("- %s: %d", pair.name, pair.count))
	}
	out = append(out, "important_files:")
	for _, file := range important {
		out = append(out, "- "+file)
	}
	if len(goSymbols) > 0 {
		out = append(out, "go_symbols:")
		for _, summary := range goSymbols {
			out = append(out, summary.lines()...)
		}
	}
	return Result{Output: trimLinesToTokenBudget(out, maxTokens)}, nil
}

type countPair struct {
	name  string
	count int
}

func sortedCounts(counts map[string]int) []countPair {
	out := make([]countPair, 0, len(counts))
	for name, count := range counts {
		out = append(out, countPair{name: name, count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count == out[j].count {
			return out[i].name < out[j].name
		}
		return out[i].count > out[j].count
	})
	return out
}

func importantFiles(files []string, limit int, focus string) []string {
	if limit <= 0 || len(files) == 0 {
		return nil
	}
	focusTerms := queryTerms(focus)
	score := func(path string) int {
		name := strings.ToLower(filepath.Base(path))
		dir := strings.ToLower(filepath.Dir(path))
		lowerPath := strings.ToLower(filepath.ToSlash(path))
		score := 0
		switch name {
		case "readme.md", "go.mod", "package.json", "cargo.toml", "pyproject.toml", "implementation_plan.md", "makefile":
			score += 100
		}
		if strings.HasPrefix(dir, "cmd") || strings.HasPrefix(dir, "pkg") || strings.HasPrefix(dir, "src") || strings.HasPrefix(dir, "internal") {
			score += 20
		}
		if strings.Contains(name, "test") {
			score += 8
		}
		if filepath.Ext(name) == ".go" || filepath.Ext(name) == ".rs" || filepath.Ext(name) == ".ts" || filepath.Ext(name) == ".py" {
			score += 5
		}
		for _, term := range focusTerms {
			if strings.Contains(lowerPath, term) {
				score += 40
			}
		}
		return score
	}

	sorted := append([]string(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := score(sorted[i]), score(sorted[j])
		if left == right {
			return sorted[i] < sorted[j]
		}
		return left > right
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

type goFileSummary struct {
	Path    string
	Package string
	Imports []string
	Symbols []string
	Score   int
}

func goSymbolSummaries(root string, files []string, focus string) []goFileSummary {
	focusTerms := queryTerms(focus)
	var summaries []goFileSummary
	for _, rel := range files {
		if filepath.Ext(rel) != ".go" {
			continue
		}
		summary, err := parseGoFileSummary(filepath.Join(root, rel), rel, focusTerms)
		if err != nil || len(summary.Symbols) == 0 {
			continue
		}
		summaries = append(summaries, summary)
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].Score == summaries[j].Score {
			return summaries[i].Path < summaries[j].Path
		}
		return summaries[i].Score > summaries[j].Score
	})
	if len(summaries) > 24 {
		summaries = summaries[:24]
	}
	return summaries
}

func parseGoFileSummary(path, rel string, focusTerms []string) (goFileSummary, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return goFileSummary{}, err
	}
	summary := goFileSummary{Path: filepath.ToSlash(rel), Package: file.Name.Name}
	for _, imp := range file.Imports {
		summary.Imports = append(summary.Imports, strings.Trim(imp.Path.Value, `"`))
	}
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			name := decl.Name.Name
			if decl.Recv != nil && len(decl.Recv.List) > 0 {
				name = receiverName(decl.Recv.List[0].Type) + "." + name
			}
			summary.Symbols = append(summary.Symbols, "func "+name+signatureSummary(decl.Type))
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					summary.Symbols = append(summary.Symbols, "type "+spec.Name.Name+" "+exprKind(spec.Type))
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						summary.Symbols = append(summary.Symbols, strings.ToLower(decl.Tok.String())+" "+name.Name)
					}
				}
			}
		}
	}
	if len(summary.Imports) > 6 {
		summary.Imports = summary.Imports[:6]
	}
	if len(summary.Symbols) > 10 {
		summary.Symbols = summary.Symbols[:10]
	}
	lower := strings.ToLower(summary.Path + " " + strings.Join(summary.Symbols, " ") + " " + strings.Join(summary.Imports, " "))
	for _, term := range focusTerms {
		if strings.Contains(lower, term) {
			summary.Score += 10
		}
	}
	return summary, nil
}

func (s goFileSummary) lines() []string {
	header := fmt.Sprintf("- %s package=%s", s.Path, s.Package)
	if len(s.Imports) > 0 {
		header += " imports=" + strings.Join(s.Imports, ",")
	}
	lines := []string{header}
	for _, symbol := range s.Symbols {
		lines = append(lines, "  - "+symbol)
	}
	return lines
}

func receiverName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return receiverName(expr.X)
	default:
		return "recv"
	}
}

func signatureSummary(fn *ast.FuncType) string {
	params := fieldCount(fn.Params)
	results := fieldCount(fn.Results)
	if results == 0 {
		return fmt.Sprintf("(%d params)", params)
	}
	return fmt.Sprintf("(%d params) -> %d", params, results)
}

func fieldCount(fields *ast.FieldList) int {
	if fields == nil {
		return 0
	}
	count := 0
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			count++
			continue
		}
		count += len(field.Names)
	}
	return count
}

func exprKind(expr ast.Expr) string {
	switch expr.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	case *ast.FuncType:
		return "func"
	default:
		return "alias"
	}
}

func queryTerms(query string) []string {
	raw := strings.Fields(strings.ToLower(query))
	var terms []string
	for _, term := range raw {
		term = strings.Trim(term, ".,:;!?()[]{}\"'")
		if len(term) >= 3 {
			terms = append(terms, term)
		}
	}
	return terms
}

func trimLinesToTokenBudget(lines []string, maxTokens int) string {
	if maxTokens <= 0 {
		maxTokens = 1000
	}
	var out []string
	tokens := 0
	for _, line := range lines {
		next := estimateTokens(line) + 1
		if tokens+next > maxTokens {
			out = append(out, fmt.Sprintf("... repo map truncated at ~%d tokens", maxTokens))
			break
		}
		out = append(out, line)
		tokens += next
	}
	return strings.Join(out, "\n")
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(text)/4 + 1
}
