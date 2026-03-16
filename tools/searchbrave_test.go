package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBraveSearcher_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("X-Subscription-Token"))
		assert.Equal(t, "/res/v1/web/search", r.URL.Path)
		assert.Equal(t, "golang tutorials", r.URL.Query().Get("q"))

		json.NewEncoder(w).Encode(braveSearchResponse{ //nolint:errcheck
			Web: struct {
				Results []braveWebResult `json:"results"`
			}{
				Results: []braveWebResult{
					{Title: "Go Tutorial", URL: "https://go.dev", Description: "Learn Go"},
				},
			},
		})
	}))
	defer srv.Close()

	s := &BraveSearcher{
		apiKey:  "test-key",
		baseURL: srv.URL + "/res/v1",
		client:  srv.Client(),
	}

	results, err := s.Search(context.Background(), "golang tutorials", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Go Tutorial", results[0].Title)
	assert.Equal(t, "https://go.dev", results[0].Link)
	assert.Equal(t, "Learn Go", results[0].Snippet)
	assert.Equal(t, 1, results[0].Position)
}

func TestBraveSearcher_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid key"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	s := &BraveSearcher{
		apiKey:  "bad-key",
		baseURL: srv.URL + "/res/v1",
		client:  srv.Client(),
	}

	_, err := s.Search(context.Background(), "test", 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 401")
}

func TestBraveSearcher_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(braveSearchResponse{}) //nolint:errcheck
	}))
	defer srv.Close()

	s := &BraveSearcher{
		apiKey:  "test-key",
		baseURL: srv.URL + "/res/v1",
		client:  srv.Client(),
	}

	results, err := s.Search(context.Background(), "obscure query", 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}
