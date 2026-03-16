package tools

import (
	"context"
	"fmt"
	"os"
)

// WebSearcher performs web searches and returns structured results.
type WebSearcher interface {
	Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error)
}

// Search performs a web search using the best available backend.
// Backend selection: BRAVE_API_KEY → Brave, otherwise → DuckDuckGo Lite.
func Search(ctx context.Context, query string, maxResults int) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	searcher := resolveSearcher()
	results, err := searcher.Search(ctx, query, normalizeMaxResults(maxResults))
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}
	return formatSearchResults(results), nil
}

// resolveSearcher returns the best available search backend.
func resolveSearcher() WebSearcher {
	if key := os.Getenv("BRAVE_API_KEY"); key != "" {
		return NewBraveSearcher(key)
	}
	return NewDDGSearcher()
}
