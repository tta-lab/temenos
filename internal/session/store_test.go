package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegister verifies token format, session fields, and TTL.
func TestRegister(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))

	req := RegisterRequest{
		Agent:      "test-agent",
		WritePaths: []string{"/tmp/write1", "/tmp/write2"},
		ReadPaths:  []string{"/tmp/read1"},
	}

	session, err := store.Register(req)
	require.NoError(t, err)
	require.NotNil(t, session)

	// Token is 64 hex chars
	require.Len(t, session.Token, 64)
	assert.Regexp(t, regexp.MustCompile(`^[a-f0-9]{64}$`), session.Token)

	// Session fields match request
	assert.Equal(t, req.Agent, session.Agent)
	assert.Equal(t, req.WritePaths, session.WritePaths)
	assert.Equal(t, req.ReadPaths, session.ReadPaths)

	// CreatedAt and ExpiresAt are set
	assert.False(t, session.CreatedAt.IsZero())
	assert.False(t, session.ExpiresAt.IsZero())

	// ExpiresAt is approximately 8h from CreatedAt
	expectedExpiry := session.CreatedAt.Add(defaultTTL)
	assert.WithinDuration(t, expectedExpiry, session.ExpiresAt, time.Second)
}

// TestGet verifies session retrieval after registration and unknown token.
func TestGet(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))

	// Get unknown token returns nil
	result := store.Get("unknown-token")
	assert.Nil(t, result)

	// Register and retrieve
	req := RegisterRequest{Agent: "test-agent", ReadPaths: []string{"/tmp/read"}}
	session, err := store.Register(req)
	require.NoError(t, err)

	result = store.Get(session.Token)
	require.NotNil(t, result)
	assert.Equal(t, session.Token, result.Token)
	assert.Equal(t, session.Agent, result.Agent)
}

// TestGetExpired verifies Get returns nil for expired sessions.
func TestGetExpired(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))

	req := RegisterRequest{Agent: "test-agent"}
	session, err := store.Register(req)
	require.NoError(t, err)

	// Manually set ExpiresAt to the past
	store.sessions[session.Token].ExpiresAt = time.Now().Add(-1 * time.Hour)

	// Get should return nil for expired session
	result := store.Get(session.Token)
	assert.Nil(t, result)
}

// TestDelete verifies deletion and idempotency.
func TestDelete(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))

	// Register a session
	req := RegisterRequest{Agent: "test-agent", WritePaths: []string{"/tmp/write"}}
	session, err := store.Register(req)
	require.NoError(t, err)

	// Verify it exists
	result := store.Get(session.Token)
	require.NotNil(t, result)

	// Delete it
	err = store.Delete(session.Token)
	require.NoError(t, err)

	// Verify it's gone
	result = store.Get(session.Token)
	assert.Nil(t, result)

	// Delete of unknown token returns nil (idempotent)
	err = store.Delete("unknown-token")
	require.NoError(t, err)
}

// TestList verifies only non-expired sessions are returned.
func TestList(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))

	// Register two sessions
	session1, err := store.Register(RegisterRequest{Agent: "agent-1", ReadPaths: []string{"/tmp/read"}})
	require.NoError(t, err)
	session2, err := store.Register(RegisterRequest{Agent: "agent-2", WritePaths: []string{"/tmp/write"}})
	require.NoError(t, err)

	// List should return both
	list := store.List()
	assert.Len(t, list, 2)

	// Expire session1
	store.sessions[session1.Token].ExpiresAt = time.Now().Add(-1 * time.Hour)

	// List should return only session2
	list = store.List()
	assert.Len(t, list, 1)
	assert.Equal(t, session2.Token, list[0].Token)
}

// TestPersistence verifies sessions survive store recreation via LoadFromDisk.
func TestPersistence(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")

	// Create first store and register sessions
	store1 := NewStore(filePath)
	_, err := store1.Register(RegisterRequest{Agent: "agent-1", ReadPaths: []string{"/tmp/read"}})
	require.NoError(t, err)
	_, err = store1.Register(RegisterRequest{Agent: "agent-2", WritePaths: []string{"/tmp/write"}})
	require.NoError(t, err)

	// Create new store from same file and load
	store2 := NewStore(filePath)
	err = store2.LoadFromDisk()
	require.NoError(t, err)

	// Verify sessions are present
	list := store2.List()
	assert.Len(t, list, 2)
}

// TestAtomicWrite verifies file permissions are 0o600.
func TestAtomicWrite(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")
	store := NewStore(filePath)

	_, err := store.Register(RegisterRequest{Agent: "test-agent"})
	require.NoError(t, err)

	// File should exist with 0o600 permissions
	info, err := os.Stat(filePath)
	require.NoError(t, err)

	perm := info.Mode().Perm()
	assert.Equal(t, os.FileMode(0o600), perm)
}

// TestLoadFromDiskSkipsExpired verifies expired sessions in file are skipped.
func TestLoadFromDiskSkipsExpired(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")

	// Create and populate a store
	store := NewStore(filePath)
	session, err := store.Register(RegisterRequest{Agent: "active-agent"})
	require.NoError(t, err)

	// Write an expired session directly to the file
	expiredSession := &Session{
		Token:     "expired-token-1234567890123456789012345678901234567890123456",
		Agent:     "expired-agent",
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Expired
	}
	store.sessions[expiredSession.Token] = expiredSession
	err = store.persist()
	require.NoError(t, err)

	// Create new store and load
	newStore := NewStore(filePath)
	err = newStore.LoadFromDisk()
	require.NoError(t, err)

	// Only active session should be loaded
	list := newStore.List()
	require.Len(t, list, 1)
	assert.Equal(t, session.Token, list[0].Token)

	// Expired session should not exist
	assert.Nil(t, newStore.Get(expiredSession.Token))
}

// TestPruneStaleRemovesMissingWritePaths verifies sessions with non-existent WritePaths are removed.
func TestPruneStaleRemovesMissingWritePaths(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")
	store := NewStore(filePath)

	// Register session with non-existent WritePaths
	session, err := store.Register(RegisterRequest{
		Agent:      "test-agent",
		WritePaths: []string{"/nonexistent/path/12345"},
	})
	require.NoError(t, err)

	// Verify session exists
	_, inMap := store.sessions[session.Token]
	require.True(t, inMap, "session should be in map before pruning")

	// Prune should remove it
	store.PruneStale()

	// Session should be gone
	assert.Nil(t, store.Get(session.Token))
}

// TestPruneStaleRemovesExpired verifies expired sessions are removed by PruneStale.
func TestPruneStaleRemovesExpired(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")
	store := NewStore(filePath)

	session, err := store.Register(RegisterRequest{Agent: "test-agent"})
	require.NoError(t, err)

	// Manually expire the session
	store.sessions[session.Token].ExpiresAt = time.Now().Add(-1 * time.Hour)
	err = store.persist()
	require.NoError(t, err)

	// Verify session is in map before pruning (Get filters expired, so check map directly)
	_, inMap := store.sessions[session.Token]
	require.True(t, inMap, "session should be in map before pruning")

	// Prune should remove it
	store.PruneStale()

	// Session should be gone
	assert.Nil(t, store.Get(session.Token))
}

// TestRegisterDeleteReloadRoundTrip verifies register, delete, reload cycle.
func TestRegisterDeleteReloadRoundTrip(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")

	// Create store and register session
	store1 := NewStore(filePath)
	session, err := store1.Register(RegisterRequest{Agent: "test-agent"})
	require.NoError(t, err)

	// Verify it exists
	require.NotNil(t, store1.Get(session.Token))

	// Delete it
	err = store1.Delete(session.Token)
	require.NoError(t, err)

	// Create new store and load
	store2 := NewStore(filePath)
	err = store2.LoadFromDisk()
	require.NoError(t, err)

	// Session should be gone
	assert.Nil(t, store2.Get(session.Token))
}

// TestSessionJSONFormat verifies the JSON structure matches expectations.
func TestSessionJSONFormat(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")
	store := NewStore(filePath)

	_, err := store.Register(RegisterRequest{
		Agent:      "my-agent",
		WritePaths: []string{"/custom/write"},
		ReadPaths:  []string{"/custom/read"},
	})
	require.NoError(t, err)

	// Read and parse the JSON file
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)

	var sessions map[string]*Session
	err = json.Unmarshal(data, &sessions)
	require.NoError(t, err)

	require.Len(t, sessions, 1)

	for token, session := range sessions {
		assert.Len(t, token, 64)
		assert.Equal(t, "my-agent", session.Agent)
		assert.Equal(t, []string{"/custom/write"}, session.WritePaths)
		assert.Equal(t, []string{"/custom/read"}, session.ReadPaths)
		assert.False(t, session.CreatedAt.IsZero())
		assert.False(t, session.ExpiresAt.IsZero())
	}
}

// TestLoadFromDiskFileNotExist verifies LoadFromDisk handles missing file gracefully.
func TestLoadFromDiskFileNotExist(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "nonexistent.json"))

	err := store.LoadFromDisk()
	require.NoError(t, err)

	// Should have empty sessions
	list := store.List()
	assert.Len(t, list, 0)
}
