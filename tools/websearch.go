package tools

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Search performs a web search via DuckDuckGo Lite and returns formatted results.
// maxResults defaults to 10 if ≤ 0, capped at 20.
// Uses a simple one-shot HTTP client — the fantasy tool (NewSearchWebTool) keeps a
// separate long-lived client with connection pooling for repeated searches.
func Search(ctx context.Context, query string, maxResults int) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	if err := maybeDelaySearch(ctx); err != nil {
		return "", fmt.Errorf("search cancelled: %w", err)
	}

	results, err := searchDuckDuckGo(ctx, client, query, normalizeMaxResults(maxResults))
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}

	return formatSearchResults(results), nil
}
