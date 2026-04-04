package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/temenos/internal/config"
	"github.com/tta-lab/temenos/internal/session"
	"github.com/tta-lab/temenos/sandbox"
)

// makeStore creates a temporary session store for testing.
func makeStore(t *testing.T) *session.Store {
	return session.NewStore(filepath.Join(t.TempDir(), "sessions.json"))
}

// makeToken registers a session and returns its token.
func makeToken(t *testing.T, store *session.Store) string {
	sess, err := store.Register(session.RegisterRequest{Agent: "test", WritePaths: []string{"/tmp/write"}})
	require.NoError(t, err)
	return sess.Token
}

// TestTokenMiddleware_MissingToken verifies that requests without an
// X-Session-Token header receive a 401 Unauthorized response.
func TestTokenMiddleware_MissingToken(t *testing.T) {
	store := makeStore(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := tokenMiddleware(next, store)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var resp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "unauthorized", resp["error"])
}

// TestTokenMiddleware_MalformedToken_TooShort verifies that a token
// shorter than 64 characters is rejected with 401.
func TestTokenMiddleware_MalformedToken_TooShort(t *testing.T) {
	store := makeStore(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := tokenMiddleware(next, store)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Session-Token", strings.Repeat("a", 63))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestTokenMiddleware_MalformedToken_NotHex verifies that a 64-character
// token containing uppercase letters or non-hex characters is rejected with 401.
func TestTokenMiddleware_MalformedToken_NotHex(t *testing.T) {
	store := makeStore(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := tokenMiddleware(next, store)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Session-Token", strings.Repeat("A", 64)) // uppercase, not hex
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Also test with non-hex character (g)
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-Session-Token", strings.Repeat("ab", 31)+"g1")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusUnauthorized, rec2.Code)
}

// TestTokenMiddleware_ValidToken_PassesThrough verifies that a valid
// 64-character lowercase hex token registered in the store passes through
// without a 401 response.
func TestTokenMiddleware_ValidToken_PassesThrough(t *testing.T) {
	store := makeStore(t)
	token := makeToken(t, store)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := tokenMiddleware(next, store)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Session-Token", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should NOT be 401; MCP handler may return other codes
	assert.NotEqual(t, http.StatusUnauthorized, rec.Code)
}

// TestTokenMiddleware_UnknownToken verifies that a valid-format 64-char hex
// token that is not registered in the store is rejected with 401.
func TestTokenMiddleware_UnknownToken(t *testing.T) {
	store := makeStore(t)

	// Generate a fake token: 64 hex chars, not in store
	fakeToken := strings.Repeat("ab", 32) // 64 lowercase hex chars

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := tokenMiddleware(next, store)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Session-Token", fakeToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestTruncateToken_Long verifies that a 64-character token is truncated
// to the first 8 characters.
func TestTruncateToken_Long(t *testing.T) {
	token := strings.Repeat("a", 64)
	truncated := truncateToken(token)
	assert.Equal(t, 8, len(truncated))
	assert.Equal(t, "aaaaaaaa", truncated)
}

// TestTruncateToken_Short verifies that a token shorter than 8 characters
// is returned unchanged.
func TestTruncateToken_Short(t *testing.T) {
	token := "hello"
	truncated := truncateToken(token)
	assert.Equal(t, "hello", truncated)
}

// TestTruncateToken_Empty verifies that an empty token returns an empty string.
func TestTruncateToken_Empty(t *testing.T) {
	assert.Equal(t, "", truncateToken(""))
}

// TestBaselineMountsInHandler verifies the mount-building logic:
// AllowRead → ro, AllowWrite → rw, session.WritePaths → rw appended on top.
func TestBaselineMountsInHandler(t *testing.T) {
	cfg := &config.Config{
		AllowRead:  []string{"/read-only-dir"},
		AllowWrite: []string{"/read-write-dir"},
	}

	// Simulate what bashHandler builds: baseline mounts + session write paths
	mounts := cfg.BaselineMounts()

	// Add session write paths (simulating a session with an extra write path)
	sessionWritePaths := []string{"/extra-write-path"}
	for _, p := range sessionWritePaths {
		mounts = append(mounts, sandbox.Mount{Source: p, Target: p, ReadOnly: false})
	}

	require.Equal(t, 3, len(mounts))

	// AllowRead → ro
	assert.Equal(t, "/read-only-dir", mounts[0].Source)
	assert.Equal(t, "/read-only-dir", mounts[0].Target)
	assert.True(t, mounts[0].ReadOnly)

	// AllowWrite → rw
	assert.Equal(t, "/read-write-dir", mounts[1].Source)
	assert.Equal(t, "/read-write-dir", mounts[1].Target)
	assert.False(t, mounts[1].ReadOnly)

	// session.WritePaths → rw appended on top
	assert.Equal(t, "/extra-write-path", mounts[2].Source)
	assert.Equal(t, "/extra-write-path", mounts[2].Target)
	assert.False(t, mounts[2].ReadOnly)
}

// TestBaselineMountsInHandler_EmptyConfig verifies that an empty config
// returns no mounts.
func TestBaselineMountsInHandler_EmptyConfig(t *testing.T) {
	cfg := &config.Config{
		AllowRead:  nil,
		AllowWrite: nil,
	}
	mounts := cfg.BaselineMounts()
	assert.Empty(t, mounts)
}

// TestBaselineMountsInHandler_ReadOnlyOnly verifies that a config with
// only AllowRead produces only read-only mounts.
func TestBaselineMountsInHandler_ReadOnlyOnly(t *testing.T) {
	cfg := &config.Config{
		AllowRead: []string{"/only-read"},
	}
	mounts := cfg.BaselineMounts()
	require.Len(t, mounts, 1)
	assert.Equal(t, "/only-read", mounts[0].Source)
	assert.True(t, mounts[0].ReadOnly)
}

// TestBaselineMountsInHandler_WriteOnly verifies that a config with
// only AllowWrite produces only read-write mounts.
func TestBaselineMountsInHandler_WriteOnly(t *testing.T) {
	cfg := &config.Config{
		AllowWrite: []string{"/only-write"},
	}
	mounts := cfg.BaselineMounts()
	require.Len(t, mounts, 1)
	assert.Equal(t, "/only-write", mounts[0].Source)
	assert.False(t, mounts[0].ReadOnly)
}

// TestTruncateToken_EightChars verifies that an exactly 8-character token
// is returned unchanged.
func TestTruncateToken_EightChars(t *testing.T) {
	token := "12345678"
	truncated := truncateToken(token)
	assert.Equal(t, token, truncated)
}

// TestTruncateToken_NineChars verifies that a 9-character token is
// truncated to 8 characters.
func TestTruncateToken_NineChars(t *testing.T) {
	token := "123456789"
	truncated := truncateToken(token)
	assert.Equal(t, 8, len(truncated))
	assert.Equal(t, "12345678", truncated)
}

// TestNewMCPHandler_ReturnsHandler verifies that NewMCPHandler returns
// a non-nil http.Handler without panicking.
func TestNewMCPHandler_ReturnsHandler(t *testing.T) {
	cfg := &config.Config{}
	store := makeStore(t)
	sbx := sandbox.New(sandbox.Options{AllowUnsandboxed: true})

	handler := NewMCPHandler(cfg, store, sbx)
	assert.NotNil(t, handler)
}

// TestBuildExecConfig_WritePaths_MountedReadWrite verifies that session WritePaths
// are mounted as writable.
func TestBuildExecConfig_WritePaths_MountedReadWrite(t *testing.T) {
	cfg := &config.Config{}
	sess := &session.Session{
		WritePaths: []string{"/session-write"},
	}

	execCfg := buildExecConfig(cfg, sess)

	var found bool
	for _, m := range execCfg.MountDirs {
		if m.Source == "/session-write" {
			found = true
			assert.False(t, m.ReadOnly, "WritePath must be a writable mount")
		}
	}
	assert.True(t, found, "WritePath must appear in mounts")
}

// TestBuildExecConfig_ReadPaths_MountedReadOnly verifies that session ReadPaths
// are mounted as read-only.
func TestBuildExecConfig_ReadPaths_MountedReadOnly(t *testing.T) {
	cfg := &config.Config{}
	sess := &session.Session{
		ReadPaths: []string{"/session-read"},
	}

	execCfg := buildExecConfig(cfg, sess)

	var found bool
	for _, m := range execCfg.MountDirs {
		if m.Source == "/session-read" {
			found = true
			assert.True(t, m.ReadOnly, "ReadPath must be a read-only mount")
		}
	}
	assert.True(t, found, "ReadPath must appear in mounts")
}

// TestBuildExecConfig_WriteAndReadPaths verifies that a session with both
// WritePaths and ReadPaths gets the correct mount modes for each.
func TestBuildExecConfig_WriteAndReadPaths(t *testing.T) {
	cfg := &config.Config{}
	sess := &session.Session{
		WritePaths: []string{"/session-write"},
		ReadPaths:  []string{"/session-read"},
	}

	execCfg := buildExecConfig(cfg, sess)

	foundWrite, foundRead := false, false
	for _, m := range execCfg.MountDirs {
		switch m.Source {
		case "/session-write":
			foundWrite = true
			assert.False(t, m.ReadOnly, "WritePath must be a writable mount")
		case "/session-read":
			foundRead = true
			assert.True(t, m.ReadOnly, "ReadPath must be a read-only mount")
		}
	}
	assert.True(t, foundWrite, "WritePath must appear in mounts")
	assert.True(t, foundRead, "ReadPath must appear in mounts")
}

func TestBuildExecConfig_NoSessionNoWritePaths_FallsBackToTempDir(t *testing.T) {
	cfg := &config.Config{} // no AllowWrite
	execCfg := buildExecConfig(cfg, nil)
	assert.Equal(t, os.TempDir(), execCfg.WorkingDir)
}

func TestBuildExecConfig_NoSessionWritePaths_UsesConfigAllowWrite(t *testing.T) {
	cfg := &config.Config{AllowWrite: []string{"/config-write"}}
	sess := &session.Session{} // no WritePaths
	execCfg := buildExecConfig(cfg, sess)
	assert.Equal(t, "/config-write", execCfg.WorkingDir)
}

func TestBuildExecConfig_WithWritePaths_UsesFirstWritePath(t *testing.T) {
	cfg := &config.Config{AllowWrite: []string{"/config-write"}}
	sess := &session.Session{WritePaths: []string{"/session-write"}}
	execCfg := buildExecConfig(cfg, sess)
	assert.Equal(t, "/session-write", execCfg.WorkingDir)
}
