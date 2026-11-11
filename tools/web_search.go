package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	webSearchTimeout = 10 * time.Second
	searchCacheTTL   = 5 * time.Minute
)

// WebSearchTool provides web search functionality.
type WebSearchTool struct {
	client *http.Client
	mu     sync.RWMutex
	cache  map[string]searchCacheEntry
}

type searchCacheEntry struct {
	results   []SearchResult
	expiresAt time.Time
}

// NewWebSearchTool creates a new web search tool.
func NewWebSearchTool() *WebSearchTool {
	return &WebSearchTool{
		client: &http.Client{Timeout: webSearchTimeout},
		cache:  make(map[string]searchCacheEntry),
	}
}

// Name returns the tool name.
func (t *WebSearchTool) Name() string {
	return "web_search"
}

// Description returns the tool description.
func (t *WebSearchTool) Description() string {
	return "Searches the web for information about a query."
}

// Parameters returns the tool parameters.
func (t *WebSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query.",
			},
			"max_results": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 5).",
			},
		},
		"required": []string{"query"},
	}
}

// Execute executes the web search.
func (t *WebSearchTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return "", err
	}

	if args.MaxResults <= 0 {
		args.MaxResults = 5
	}

	cacheKey := args.Query
	t.mu.RLock()
	if entry, ok := t.cache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		t.mu.RUnlock()
		results := make([]SearchResult, len(entry.results))
		copy(results, entry.results)
		resultJSON, _ := json.Marshal(map[string]interface{}{
			"query":   args.Query,
			"results": results,
			"count":   len(results),
			"cached":  true,
			"source":  "cache",
		})
		return string(resultJSON), nil
	}
	t.mu.RUnlock()

	results, err := t.search(args.Query, args.MaxResults)
	if err != nil {
		// Graceful degradation: return partial results or a helpful error
		// that the agent can explain to the user.
		return "", fmt.Errorf("web search failed: %w (try a simpler query or check network connectivity)", err)
	}

	// Cache the results
	t.mu.Lock()
	t.cache[cacheKey] = searchCacheEntry{
		results:   results,
		expiresAt: time.Now().Add(searchCacheTTL),
	}
	// Evict old entries if cache grows too large
	if len(t.cache) > 100 {
		for k, v := range t.cache {
			if time.Now().After(v.expiresAt) {
				delete(t.cache, k)
			}
		}
	}
	t.mu.Unlock()

	resultJSON, _ := json.Marshal(map[string]interface{}{
		"query":   args.Query,
		"results": results,
		"count":   len(results),
	})

	return string(resultJSON), nil
}

// search performs a web search using DuckDuckGo HTML endpoint.
func (t *WebSearchTool) search(query string, maxResults int) ([]SearchResult, error) {
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", strings.ReplaceAll(query, " ", "+"))

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,ru;q=0.8")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	results, err := parseDuckDuckGoResults(string(body), maxResults)
	if err != nil {
		return nil, err
	}

	// If no results found, try alternative parsing
	if len(results) == 0 {
		results = t.parseFallback(string(body), maxResults)
	}

	return results, nil
}

// parseFallback attempts alternative HTML parsing for DuckDuckGo results.
func (t *WebSearchTool) parseFallback(html string, maxResults int) []SearchResult {
	var results []SearchResult

	// Try extracting from <a> tags with result classes
	links := extractResultLinks(html)
	for i := 0; i < len(links) && len(results) < maxResults; i++ {
		results = append(results, SearchResult{
			Title:   links[i].title,
			URL:     links[i].url,
			Snippet: "",
		})
	}
	return results
}

type resultLink struct {
	title string
	url   string
}

func extractResultLinks(html string) []resultLink {
	var links []resultLink

	// Extract title from result__a tags
	titleBlocks := splitBy(html, `<div class="result__a"`)
	for _, block := range titleBlocks {
		titleStart := strings.Index(block, ">")
		if titleStart == -1 {
			continue
		}
		titleEnd := strings.Index(block[titleStart:], "</a>")
		if titleEnd == -1 {
			continue
		}
		title := strings.TrimSpace(block[titleStart+1 : titleStart+titleEnd])

		urlStart := strings.Index(block, `href="`)
		if urlStart == -1 {
			continue
		}
		urlStart += 7
		urlEnd := strings.Index(block[urlStart:], `"`)
		if urlEnd == -1 {
			continue
		}
		url := block[urlStart : urlStart+urlEnd]

		if title != "" && url != "" {
			links = append(links, resultLink{title, url})
		}
	}
	return links
}

// splitBy splits s by separator and returns non-empty parts.
func splitBy(s, sep string) []string {
	parts := strings.Split(s, sep)
	var result []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			result = append(result, p)
		}
	}
	return result
}

// WebSearch is a convenience function for web search.
func WebSearch(query string) (string, error) {
	tool := NewWebSearchTool()
	result, err := tool.search(query, 5)
	if err != nil {
		return "", err
	}

	resultJSON, _ := json.Marshal(map[string]interface{}{
		"query":   query,
		"results": result,
		"count":   len(result),
	})

	return string(resultJSON), nil
}

// SearchResult represents a search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// parseDuckDuckGoResults parses HTML results from DuckDuckGo.
func parseDuckDuckGoResults(html string, maxResults int) ([]SearchResult, error) {
	var results []SearchResult

	// Simple HTML parsing for DuckDuckGo results
	// Extract result blocks
	blocks := strings.Split(html, `<div class="result__a"`)

	for i := 1; i < len(blocks) && len(results) < maxResults; i++ {
		block := blocks[i]

		// Extract title
		titleStart := strings.Index(block, ">")
		if titleStart == -1 {
			continue
		}
		titleEnd := strings.Index(block[titleStart:], "</a>")
		if titleEnd == -1 {
			continue
		}
		title := strings.TrimSpace(block[titleStart+1 : titleStart+titleEnd])

		// Extract URL
		urlStart := strings.Index(block, `href="`)
		if urlStart == -1 {
			continue
		}
		urlStart += 7
		urlEnd := strings.Index(block[urlStart:], `"`)
		if urlEnd == -1 {
			continue
		}
		url := block[urlStart : urlStart+urlEnd]

		// Extract snippet
		snippetStart := strings.Index(block, `<a class="result__snippet"`)
		if snippetStart != -1 {
			snippetBlock := block[snippetStart:]
			snipStart := strings.Index(snippetBlock, ">")
			if snipStart != -1 {
				snipEnd := strings.Index(snippetBlock[snipStart:], "</a>")
				if snipEnd != -1 {
					snippet := strings.TrimSpace(snippetBlock[snipStart+1 : snipStart+snipEnd])
					results = append(results, SearchResult{
						Title:   title,
						URL:     url,
						Snippet: snippet,
					})
					continue
				}
			}
		}

		// Fallback: try to get snippet from result__snippet div
		snipIdx := strings.Index(block, `class="result__snippet"`)
		if snipIdx != -1 {
			snipBlock := block[snipIdx:]
			snipStart := strings.Index(snipBlock, ">")
			if snipStart != -1 {
				snipEnd := strings.Index(snipBlock[snipStart:], "</a>")
				if snipEnd != -1 {
					snippet := strings.TrimSpace(snipBlock[snipStart+1 : snipStart+snipEnd])
					results = append(results, SearchResult{
						Title:   title,
						URL:     url,
						Snippet: snippet,
					})
				}
			}
		} else {
			results = append(results, SearchResult{
				Title:   title,
				URL:     url,
				Snippet: "",
			})
		}
	}

	return results, nil
}
