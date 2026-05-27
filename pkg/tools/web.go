package tools

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"klyra/pkg/llm"
)

const webToolTimeout = 8 * time.Second

type WebSearch struct {
	Endpoint string
	Client   *http.Client
}

func (w WebSearch) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "web_search",
		Description: "Search the public web for current information. Use only when the user asks for internet/current/latest/external facts.",
		Parameters: objectSchema(map[string]any{
			"query":       stringProperty("Search query."),
			"max_results": integerProperty("Maximum result count.", 1),
		}, "query"),
	}
}

func (w WebSearch) Run(ctx context.Context, inv Invocation) (Result, error) {
	query, err := stringArg(inv.Args, "query")
	if err != nil {
		return Result{}, err
	}
	maxResults, err := optionalIntArg(inv.Args, "max_results", 5)
	if err != nil {
		return Result{}, err
	}
	if maxResults <= 0 || maxResults > 10 {
		maxResults = 5
	}
	endpoints := []string{w.Endpoint}
	if strings.TrimSpace(w.Endpoint) == "" {
		endpoints = []string{
			"https://lite.duckduckgo.com/lite/?q=%s",
			"https://html.duckduckgo.com/html/?q=%s",
		}
	}
	var errs []string
	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint) == "" {
			continue
		}
		searchURL := searchEndpointURL(endpoint, query)
		data, err := httpGetText(ctx, w.Client, searchURL, 512_000)
		if err != nil {
			errs = append(errs, err.Error())
			if errors.Is(err, context.Canceled) {
				return Result{}, err
			}
			continue
		}
		results := parseSearchResults(data, maxResults)
		if len(results) == 0 {
			errs = append(errs, "no web results from "+searchURL)
			continue
		}
		return Result{Output: strings.Join(results, "\n")}, nil
	}
	if len(errs) == 0 {
		return Result{Output: "no web results"}, nil
	}
	return Result{Output: strings.Join(errs, "\n")}, fmt.Errorf("web search failed")
}

type FetchURL struct {
	Client *http.Client
}

func (FetchURL) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "fetch_url",
		Description: "Fetch and summarize a public http(s) URL. Use after web_search when page details are needed.",
		Parameters: objectSchema(map[string]any{
			"url":       stringProperty("HTTP or HTTPS URL."),
			"max_bytes": integerProperty("Maximum response bytes to read.", 1),
		}, "url"),
	}
}

func (f FetchURL) Run(ctx context.Context, inv Invocation) (Result, error) {
	rawURL, err := stringArg(inv.Args, "url")
	if err != nil {
		return Result{}, err
	}
	maxBytes, err := optionalIntArg(inv.Args, "max_bytes", 12000)
	if err != nil {
		return Result{}, err
	}
	if maxBytes <= 0 || maxBytes > 100_000 {
		maxBytes = 12000
	}
	data, err := httpGetText(ctx, f.Client, rawURL, int64(maxBytes))
	if err != nil {
		return Result{}, err
	}
	text := htmlToText(data)
	return Result{Output: CompressOutput(text, 180)}, nil
}

func searchEndpointURL(endpoint, query string) string {
	escaped := url.QueryEscape(query)
	if strings.Contains(endpoint, "%s") {
		return fmt.Sprintf(endpoint, escaped)
	}
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	return endpoint + separator + "q=" + escaped
}

func httpGetText(ctx context.Context, client *http.Client, rawURL string, maxBytes int64) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("only http(s) URLs are allowed")
	}
	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok {
		ctx, cancel = context.WithTimeout(ctx, webToolTimeout)
		defer cancel()
	}
	if client == nil {
		client = &http.Client{Timeout: webToolTimeout}
	} else if client.Timeout == 0 {
		cloned := *client
		cloned.Timeout = webToolTimeout
		client = &cloned
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Klyra/1.0 (+https://github.com/tg-prplx/klyra)")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("GET %s returned %s", parsed.String(), resp.Status)
	}
	limit := maxBytes
	if limit <= 0 {
		limit = 12000
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > limit {
		data = data[:limit]
	}
	return string(data), nil
}

func parseSearchResults(page string, maxResults int) []string {
	linkPattern := regexp.MustCompile(`(?is)<a[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	tagPattern := regexp.MustCompile(`(?is)<[^>]+>`)
	seen := map[string]bool{}
	var results []string
	for _, match := range linkPattern.FindAllStringSubmatch(page, -1) {
		href := html.UnescapeString(match[1])
		title := strings.TrimSpace(tagPattern.ReplaceAllString(match[2], " "))
		title = strings.Join(strings.Fields(html.UnescapeString(title)), " ")
		if title == "" || strings.Contains(strings.ToLower(href), "duckduckgo.com/y.js") {
			continue
		}
		if decoded := decodeDuckDuckGoURL(href); decoded != "" {
			href = decoded
		}
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
			continue
		}
		if seen[href] {
			continue
		}
		seen[href] = true
		results = append(results, strconv.Itoa(len(results)+1)+". "+title+" - "+href)
		if len(results) >= maxResults {
			return results
		}
	}
	return results
}

func decodeDuckDuckGoURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.Query().Get("uddg") != "" {
		return parsed.Query().Get("uddg")
	}
	return ""
}

func htmlToText(raw string) string {
	text := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`).ReplaceAllString(raw, " ")
	text = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`).ReplaceAllString(text, " ")
	text = regexp.MustCompile(`(?is)<br\s*/?>|</p>|</div>|</h[1-6]>|</li>`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	lines := splitNonEmptyLines(text)
	return strings.Join(lines, "\n")
}
