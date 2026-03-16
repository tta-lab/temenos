package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const braveBaseURL = "https://api.search.brave.com/res/v1"

// BraveSearcher searches via the Brave Search API.
type BraveSearcher struct {
	apiKey  string
	baseURL string // defaults to braveBaseURL; tests override
	client  *http.Client
}

func NewBraveSearcher(apiKey string) *BraveSearcher {
	if apiKey == "" {
		slog.Warn("NewBraveSearcher called with empty API key — searches will return HTTP 401")
	}
	return &BraveSearcher{
		apiKey:  apiKey,
		baseURL: braveBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *BraveSearcher) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	u := fmt.Sprintf("%s/web/search?q=%s&count=%d",
		s.baseURL, url.QueryEscape(query), maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Subscription-Token", s.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return nil, fmt.Errorf("brave search: HTTP %d (body read error: %w)", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("brave search: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result braveSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("brave search: decode: %w", err)
	}

	results := toBraveResults(result)
	if len(results) == 0 {
		slog.Warn("brave_search returned zero results — possible quota exhaustion or no matches")
	}
	return results, nil
}

// braveWebResult is a single result entry in the Brave API response.
type braveWebResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// braveSearchResponse is the minimal Brave API response we need.
type braveSearchResponse struct {
	Web struct {
		Results []braveWebResult `json:"results"`
	} `json:"web"`
}

func toBraveResults(resp braveSearchResponse) []SearchResult {
	results := make([]SearchResult, 0, len(resp.Web.Results))
	for i, r := range resp.Web.Results {
		results = append(results, SearchResult{
			Title:    r.Title,
			Link:     r.URL,
			Snippet:  r.Description,
			Position: i + 1,
		})
	}
	return results
}
