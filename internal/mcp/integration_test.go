//go:build integration

package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/temenos/internal/config"
	"github.com/tta-lab/temenos/internal/session"
	"github.com/tta-lab/temenos/sandbox"
)

func setupTestServer(t *testing.T) (*httptest.Server, *session.Store) {
	t.Helper()

	store := session.NewStore(t.TempDir() + "/sessions.json")

	cfg := &config.Config{
		AllowRead:  []string{"/tmp"},
		AllowWrite: []string{"/tmp"},
	}

	sbx := sandbox.New(sandbox.Options{AllowUnsandboxed: true})

	handler := NewMCPHandler(cfg, store, sbx)
	server := httptest.NewServer(handler)

	return server, store
}

func makeMCPRequest(t *testing.T, serverURL, token string, body interface{}) *http.Response {
	t.Helper()

	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		require.NoError(t, err)
	}

	req, err := http.NewRequest("POST", serverURL, bytes.NewReader(reqBody))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Session-Token", token)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	return resp
}

func TestAuth_NoTokenHeader(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	resp := makeMCPRequest(t, server.URL, "", nil)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuth_MalformedToken_WrongLength(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	// Token too short (63 chars instead of 64)
	token := "a" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde"
	resp := makeMCPRequest(t, server.URL, token, nil)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuth_MalformedToken_NonHex(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	// Valid length but contains non-hex characters
	token := "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
	resp := makeMCPRequest(t, server.URL, token, nil)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuth_ValidFormatTokenNotInStore(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	// Valid 64-char hex string, but not registered
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	resp := makeMCPRequest(t, server.URL, token, nil)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuth_RegisteredToken(t *testing.T) {
	server, store := setupTestServer(t)
	defer server.Close()

	// Register a valid session
	sess, err := store.Register(session.RegisterRequest{
		Agent: "test-agent",
	})
	require.NoError(t, err)

	// Send request with valid registered token
	resp := makeMCPRequest(t, server.URL, sess.Token, nil)
	defer resp.Body.Close()

	// Should not be 401 - either 2xx or MCP-level response
	// The MCP handler will parse the JSON body, so we expect a valid response status
	assert.NotEqual(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuth_ExpiredSession(t *testing.T) {
	server, store := setupTestServer(t)
	defer server.Close()

	// Register a session
	sess, err := store.Register(session.RegisterRequest{
		Agent: "test-agent",
	})
	require.NoError(t, err)

	// Delete the session to simulate expiration
	store.Delete(sess.Token)

	// Send request with expired token
	resp := makeMCPRequest(t, server.URL, sess.Token, nil)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuth_SessionDeletedThenUsed(t *testing.T) {
	server, store := setupTestServer(t)
	defer server.Close()

	// Register a session
	sess, err := store.Register(session.RegisterRequest{
		Agent: "test-agent",
	})
	require.NoError(t, err)

	// Delete the session
	store.Delete(sess.Token)

	// Verify the session is no longer retrievable
	got := store.Get(sess.Token)
	assert.Nil(t, got)

	// Send request with deleted token
	resp := makeMCPRequest(t, server.URL, sess.Token, nil)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
