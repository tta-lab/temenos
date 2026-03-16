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

	searcher, err := resolveSearcher()
	if err != nil {
		return "", err
	}
	results, err := searcher.Search(ctx, query, normalizeMaxResults(maxResults))
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}
	return formatSearchResults(results), nil
}

// resolveSearcher returns the best available search backend.
// Returns an error if BRAVE_API_KEY is set but empty — this prevents silently
// falling back to DuckDuckGo when a user has misconfigured their Brave key.
func resolveSearcher() (WebSearcher, error) {
	key, set := os.LookupEnv("BRAVE_API_KEY")
	if set && key == "" {
		return nil, fmt.Errorf("BRAVE_API_KEY is set but empty; provide a valid key or unset it to use DuckDuckGo")
	}
	if key != "" {
		return NewBraveSearcher(key), nil
	}
	return NewDDGSearcher(), nil
}
