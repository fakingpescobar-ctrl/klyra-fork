package cockpit

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	contextmgr "klyra/pkg/context"
)

const (
	maxChunkBytes = 2400
	minChunkBytes = 320
)

type retrievalConfig struct {
	MaxTokens     int
	MaxChunks     int
	MaxFiles      int
	UseEmbeddings bool
	UseReranker   bool
	RepoMap       string
}

type retrievalChunk struct {
	Path      string
	StartLine int
	EndLine   int
	Text      string
	Terms     map[string]int
	Tokens    int
	Score     float64
	Reason    string
}

func buildRetrievalCart(ctx context.Context, cfg retrievalConfig, cwd, query string) (string, []string) {
	queryTerms := retrievalTerms(query)
	if len(queryTerms) == 0 {
		return "", nil
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 1000
	}
	if cfg.MaxChunks <= 0 {
		cfg.MaxChunks = 10
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = DefaultMaxFiles
	}

	chunks, warnings := collectRetrievalChunks(ctx, cwd, cfg.MaxFiles)
	if len(chunks) == 0 {
		return "", warnings
	}
	astHints := repoMapHints(cfg.RepoMap)
	idf := inverseDocumentFrequency(chunks)
	avgLen := averageChunkTerms(chunks)
	for i := range chunks {
		chunks[i].Score, chunks[i].Reason = scoreChunk(chunks[i], queryTerms, idf, avgLen, astHints)
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		if chunks[i].Score == chunks[j].Score {
			if chunks[i].Path == chunks[j].Path {
				return chunks[i].StartLine < chunks[j].StartLine
			}
			return chunks[i].Path < chunks[j].Path
		}
		return chunks[i].Score > chunks[j].Score
	})

	selected := selectRetrievalChunks(chunks, cfg.MaxChunks, cfg.MaxTokens)
	if len(selected) == 0 {
		return "", warnings
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("query: %s", strings.TrimSpace(query)))
	lines = append(lines, fmt.Sprintf("budget: %d tokens / %d chunks", cfg.MaxTokens, cfg.MaxChunks))
	lines = append(lines, fmt.Sprintf("embeddings: %s", onOff(cfg.UseEmbeddings)))
	lines = append(lines, fmt.Sprintf("reranker: %s", onOff(cfg.UseReranker)))
	if cfg.UseEmbeddings {
		lines = append(lines, "note: embeddings are configured on, but MVP retrieval currently uses deterministic BM25+AST only")
	}
	if cfg.UseReranker {
		lines = append(lines, "note: reranker is configured on, but no external reranker is wired in this MVP")
	}
	for i, chunk := range selected {
		lines = append(lines, fmt.Sprintf("%d. %s:%d-%d score=%.2f tokens=%d", i+1, chunk.Path, chunk.StartLine, chunk.EndLine, chunk.Score, chunk.Tokens))
		lines = append(lines, "   why: "+chunk.Reason)
		lines = append(lines, indentSnippet(chunk.Text, "   "))
	}
	return strings.Join(lines, "\n"), warnings
}

func collectRetrievalChunks(ctx context.Context, cwd string, maxFiles int) ([]retrievalChunk, []string) {
	deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	type candidate struct {
		path string
		size int64
	}
	var candidates []candidate
	var warnings []string
	err := filepath.WalkDir(cwd, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			warnings = append(warnings, "retrieval walk: "+walkErr.Error())
			return nil
		}
		if err := deadline.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if path != cwd && retrievalSkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			warnings = append(warnings, "retrieval stat: "+err.Error())
			return nil
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			warnings = append(warnings, "retrieval relpath: "+err.Error())
			return nil
		}
		rel = filepath.ToSlash(rel)
		if retrievalSkipPath(rel, info) {
			return nil
		}
		candidates = append(candidates, candidate{path: rel, size: info.Size()})
		return nil
	})
	if err != nil && err != context.DeadlineExceeded {
		warnings = append(warnings, "retrieval walk: "+err.Error())
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := retrievalFileScore(candidates[i].path), retrievalFileScore(candidates[j].path)
		if left == right {
			if candidates[i].size == candidates[j].size {
				return candidates[i].path < candidates[j].path
			}
			return candidates[i].size < candidates[j].size
		}
		return left > right
	})
	if len(candidates) > maxFiles {
		candidates = candidates[:maxFiles]
	}

	var chunks []retrievalChunk
	for _, file := range candidates {
		if err := deadline.Err(); err != nil {
			warnings = append(warnings, "retrieval timed out")
			break
		}
		data, err := os.ReadFile(filepath.Join(cwd, filepath.FromSlash(file.path)))
		if err != nil {
			warnings = append(warnings, "retrieval read: "+err.Error())
			continue
		}
		if looksBinary(data) {
			continue
		}
		chunks = append(chunks, chunkText(file.path, string(data))...)
	}
	return chunks, warnings
}

func chunkText(path, text string) []retrievalChunk {
	lines := strings.Split(text, "\n")
	var chunks []retrievalChunk
	start := 0
	bytes := 0
	for i, line := range lines {
		bytes += len(line) + 1
		blankBoundary := strings.TrimSpace(line) == "" && bytes >= minChunkBytes
		if bytes >= maxChunkBytes || blankBoundary {
			chunks = appendChunk(chunks, path, lines[start:i+1], start+1)
			start = i + 1
			bytes = 0
		}
	}
	if start < len(lines) {
		chunks = appendChunk(chunks, path, lines[start:], start+1)
	}
	return chunks
}

func appendChunk(chunks []retrievalChunk, path string, lines []string, startLine int) []retrievalChunk {
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if text == "" {
		return chunks
	}
	return append(chunks, retrievalChunk{
		Path:      path,
		StartLine: startLine,
		EndLine:   startLine + strings.Count(text, "\n"),
		Text:      text,
		Terms:     termCounts(text + " " + path),
		Tokens:    contextmgr.EstimateTokens(text),
	})
}

func scoreChunk(chunk retrievalChunk, queryTerms []string, idf map[string]float64, avgLen float64, astHints map[string]bool) (float64, string) {
	const k1 = 1.2
	const b = 0.75
	docLen := float64(sumTerms(chunk.Terms))
	if avgLen <= 0 {
		avgLen = 1
	}
	score := 0.0
	var matches []string
	for _, term := range queryTerms {
		tf := float64(chunk.Terms[term])
		if tf == 0 {
			continue
		}
		score += idf[term] * (tf * (k1 + 1)) / (tf + k1*(1-b+b*docLen/avgLen))
		matches = append(matches, term)
	}
	var boosts []string
	if astHints[chunk.Path] {
		score += 1.4
		boosts = append(boosts, "repo-map path")
	}
	for _, term := range queryTerms {
		if strings.Contains(strings.ToLower(chunk.Path), term) {
			score += 1.0
			boosts = append(boosts, "path:"+term)
		}
	}
	if strings.Contains(strings.ToLower(filepath.Base(chunk.Path)), "test") {
		score += 0.2
	}
	if len(matches) == 0 && len(boosts) == 0 {
		return 0, "no lexical or AST match"
	}
	reason := "bm25 terms: " + strings.Join(matches, ", ")
	if len(boosts) > 0 {
		reason += "; boosts: " + strings.Join(boosts, ", ")
	}
	return score, reason
}

func selectRetrievalChunks(chunks []retrievalChunk, maxChunks, maxTokens int) []retrievalChunk {
	selected := make([]retrievalChunk, 0, maxChunks)
	tokens := 0
	seenPath := map[string]int{}
	for _, chunk := range chunks {
		if chunk.Score <= 0 {
			continue
		}
		if len(selected) >= maxChunks {
			break
		}
		if seenPath[chunk.Path] >= 3 {
			continue
		}
		if tokens+chunk.Tokens > maxTokens {
			continue
		}
		selected = append(selected, chunk)
		tokens += chunk.Tokens
		seenPath[chunk.Path]++
	}
	return selected
}

func inverseDocumentFrequency(chunks []retrievalChunk) map[string]float64 {
	df := map[string]int{}
	for _, chunk := range chunks {
		for term := range chunk.Terms {
			df[term]++
		}
	}
	total := float64(len(chunks))
	idf := map[string]float64{}
	for term, count := range df {
		idf[term] = math.Log(1 + (total-float64(count)+0.5)/(float64(count)+0.5))
	}
	return idf
}

func averageChunkTerms(chunks []retrievalChunk) float64 {
	if len(chunks) == 0 {
		return 0
	}
	total := 0
	for _, chunk := range chunks {
		total += sumTerms(chunk.Terms)
	}
	return float64(total) / float64(len(chunks))
}

func sumTerms(terms map[string]int) int {
	total := 0
	for _, count := range terms {
		total += count
	}
	return total
}

func repoMapHints(repoMap string) map[string]bool {
	hints := map[string]bool{}
	for _, line := range strings.Split(repoMap, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if line == "" || strings.Contains(line, ": ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		path := strings.TrimSpace(fields[0])
		if strings.Contains(path, "/") || strings.Contains(path, ".") {
			hints[path] = true
		}
	}
	return hints
}

func retrievalTerms(query string) []string {
	counts := termCounts(query)
	terms := make([]string, 0, len(counts))
	for term := range counts {
		if len(term) >= 3 && !stopTerms[term] {
			terms = append(terms, term)
		}
	}
	sort.Strings(terms)
	return terms
}

func termCounts(text string) map[string]int {
	terms := map[string]int{}
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		term := strings.ToLower(string(current))
		current = current[:0]
		if len(term) < 2 || stopTerms[term] {
			return
		}
		terms[term]++
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return terms
}

func indentSnippet(text, prefix string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 14 {
		lines = append(lines[:14], "...")
	}
	for i, line := range lines {
		if len(line) > 160 {
			line = line[:157] + "..."
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func retrievalFileScore(path string) int {
	base := strings.ToLower(filepath.Base(path))
	dir := strings.ToLower(filepath.Dir(path))
	score := 0
	if isCodeLikePath(path) {
		score += 10
	}
	switch base {
	case "readme.md", "go.mod", "package.json", "cargo.toml", "pyproject.toml", "implementation_plan.md", "makefile":
		score += 20
	}
	if strings.HasPrefix(dir, "cmd") || strings.HasPrefix(dir, "pkg") || strings.HasPrefix(dir, "src") || strings.HasPrefix(dir, "internal") {
		score += 8
	}
	if strings.Contains(base, "test") {
		score += 3
	}
	return score
}

func retrievalSkipDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".agentcli", "node_modules", "dist", "build", ".cache", ".next", "vendor", "coverage", "target", ".venv", "venv":
		return true
	default:
		return false
	}
}

func retrievalSkipPath(path string, info os.FileInfo) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	name := filepath.Base(lower)
	switch name {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lockb", "go.sum", "cargo.lock", "poetry.lock", "gemfile.lock":
		return true
	}
	if strings.HasSuffix(name, ".min.js") || strings.HasSuffix(name, ".min.css") || strings.HasSuffix(name, ".snap") {
		return true
	}
	if strings.Contains(lower, "__snapshots__/") || strings.Contains(name, ".generated.") || strings.Contains(name, ".gen.") {
		return true
	}
	if !isCodeLikePath(path) && !isDocLikePath(path) {
		return true
	}
	if info != nil && info.Size() > 256*1024 {
		return true
	}
	return false
}

func isCodeLikePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".py", ".rs", ".java", ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".hh", ".rb", ".php", ".cs", ".kt", ".kts", ".swift", ".lua", ".sh", ".bash", ".zsh", ".sql", ".html", ".htm", ".css", ".scss", ".sass", ".svelte", ".yaml", ".yml", ".toml", ".json", ".md":
		return true
	default:
		return false
	}
}

func isDocLikePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".md", ".rst":
		return true
	default:
		return false
	}
}

func looksBinary(data []byte) bool {
	sample := data
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	for _, b := range sample {
		if b == 0 {
			return true
		}
	}
	return false
}

var stopTerms = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
	"from": true, "into": true, "when": true, "where": true, "what": true, "why": true,
	"как": true, "что": true, "для": true, "или": true, "это": true, "если": true,
	"надо": true, "нужно": true, "сделай": true,
}
