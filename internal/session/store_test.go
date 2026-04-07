package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testEnvMutated = "mutated"
	testEnvAdded   = "added"
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

// TestPruneStaleRemovesMissingReadPaths verifies sessions with non-existent ReadPaths are removed.
func TestPruneStaleRemovesMissingReadPaths(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")
	store := NewStore(filePath)

	// Register session with non-existent ReadPaths (no WritePaths)
	session, err := store.Register(RegisterRequest{
		Agent:     "test-agent",
		ReadPaths: []string{"/nonexistent/read/path/12345"},
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

// TestPruneStaleRemovesMixedStalePaths verifies that a session with valid WritePaths
// but missing ReadPaths is still pruned.
func TestPruneStaleRemovesMixedStalePaths(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")
	store := NewStore(filePath)

	validDir := t.TempDir() // exists

	session, err := store.Register(RegisterRequest{
		Agent:      "test-agent",
		WritePaths: []string{validDir},
		ReadPaths:  []string{"/nonexistent/read/path/12345"},
	})
	require.NoError(t, err)

	_, inMap := store.sessions[session.Token]
	require.True(t, inMap, "session should be in map before pruning")

	store.PruneStale()

	assert.Nil(t, store.Get(session.Token))
}

// TestRegisterValidation verifies that Register rejects invalid paths.
func TestRegisterValidation(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))

	tests := []struct {
		name string
		req  RegisterRequest
	}{
		{"empty write path", RegisterRequest{Agent: "a", WritePaths: []string{""}}},
		{"relative write path", RegisterRequest{Agent: "a", WritePaths: []string{"relative/path"}}},
		{"empty read path", RegisterRequest{Agent: "a", ReadPaths: []string{""}}},
		{"relative read path", RegisterRequest{Agent: "a", ReadPaths: []string{"relative/path"}}},
		{"overlapping paths", RegisterRequest{Agent: "a", WritePaths: []string{"/shared"}, ReadPaths: []string{"/shared"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.Register(tt.req)
			require.Error(t, err, "Register should reject: %s", tt.name)
		})
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

// TestRegister_WithEnv_PersistsAndRoundTrips verifies Env is stored and retrieved correctly.
func TestRegister_WithEnv_PersistsAndRoundTrips(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))

	req := RegisterRequest{
		Agent:      "test-agent",
		WritePaths: []string{"/tmp/write"},
		ReadPaths:  []string{"/tmp/read"},
		Env:        map[string]string{"FOO": "bar", "TTAL_AGENT_NAME": "astra"},
	}

	session, err := store.Register(req)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"FOO": "bar", "TTAL_AGENT_NAME": "astra"}, session.Env)

	// Retrieve and verify
	retrieved := store.Get(session.Token)
	require.NotNil(t, retrieved)
	assert.Equal(t, map[string]string{"FOO": "bar", "TTAL_AGENT_NAME": "astra"}, retrieved.Env)
}

// TestRegister_EnvKeyValidation verifies invalid env keys are rejected.
func TestRegister_EnvKeyValidation(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))

	tests := []struct {
		name string
		env  map[string]string
	}{
		{"empty key", map[string]string{"": "value"}},
		{"key starting with digit", map[string]string{"1FOO": "bar"}},
		{"key with equals", map[string]string{"FOO=BAR": "baz"}},
		{"key with space", map[string]string{"FOO BAR": "baz"}},
		{"key with tab", map[string]string{"FOO\tBAR": "baz"}},
		{"key with newline", map[string]string{"FOO\nBAR": "baz"}},
		{"key with CR", map[string]string{"FOO\rBAR": "baz"}},
		{"whitespace-only key", map[string]string{"   ": "val"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.Register(RegisterRequest{Agent: "a", Env: tt.env})
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrValidation), "expected ErrValidation, got: %v", err)
		})
	}
}

// TestRegister_EnvValueValidation verifies invalid env values are rejected.
func TestRegister_EnvValueValidation(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))

	tests := []struct {
		name  string
		env   map[string]string
		valid bool
	}{
		{"value with NUL", map[string]string{"FOO": "bar\x00baz"}, false},
		{"value with LF", map[string]string{"FOO": "bar\nbaz"}, false},
		{"value with CR", map[string]string{"FOO": "bar\rbaz"}, false},
		{"value with shell metachar $", map[string]string{"FOO": "bar$HOME"}, true},
		{"value with backtick", map[string]string{"FOO": "bar`cmd`"}, true},
		{"value with ${}", map[string]string{"FOO": "bar${VAR}"}, true},
		{"value with =", map[string]string{"FOO": "a=b=c"}, true},
		{"empty value", map[string]string{"FOO": ""}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.Register(RegisterRequest{Agent: "a", Env: tt.env})
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.True(t, errors.Is(err, ErrValidation), "expected ErrValidation, got: %v", err)
			}
		})
	}
}

// TestRegister_EnvDefensiveCopy verifies Register copies the Env map defensively.
func TestRegister_EnvDefensiveCopy(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))

	original := map[string]string{"FOO": "bar"}
	req := RegisterRequest{Agent: "a", Env: original}

	session, err := store.Register(req)
	require.NoError(t, err)

	// Mutate the original map
	original["FOO"] = testEnvMutated
	original["NEW"] = testEnvAdded

	// Retrieve and verify original values intact
	retrieved := store.Get(session.Token)
	require.NotNil(t, retrieved)
	assert.Equal(t, "bar", retrieved.Env["FOO"])
	_, hasNew := retrieved.Env["NEW"]
	assert.False(t, hasNew)
}

// TestSession_Clone_DeepCopiesEnv verifies Session.clone() deep-copies the Env map.
func TestSession_Clone_DeepCopiesEnv(t *testing.T) {
	orig := &Session{Agent: "a", Env: map[string]string{"FOO": "bar"}}
	clone := orig.clone()

	clone.Env["FOO"] = testEnvMutated
	clone.Env["NEW"] = testEnvAdded

	assert.Equal(t, "bar", orig.Env["FOO"])
	_, hasNew := orig.Env["NEW"]
	assert.False(t, hasNew)
}

// TestLoadFromDisk_BackwardCompat verifies old JSON without env field loads with nil Env.
func TestLoadFromDisk_BackwardCompat(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")

	oldJSON := `{"token1234567890123456789012345678901234567890123456789012345678901234":` +
		`{"token":"token1234567890123456789012345678901234567890123456789012345678901234"` +
		`,"agent":"old-agent","write_paths":[],"read_paths":[],` +
		`"created_at":"2024-01-01T00:00:00Z","expires_at":"2030-01-01T00:00:00Z"}}`
	err := os.WriteFile(filePath, []byte(oldJSON), 0o600)
	require.NoError(t, err)

	store := NewStore(filePath)
	err = store.LoadFromDisk()
	require.NoError(t, err)

	list := store.List()
	require.Len(t, list, 1)
	assert.Equal(t, "old-agent", list[0].Agent)
	assert.Nil(t, list[0].Env)

	sess := store.Get(list[0].Token)
	require.NotNil(t, sess)
	assert.Nil(t, sess.Env)
}

// TestGet_EnvDefensiveCopy verifies Get returns a cloned session.
func TestGet_EnvDefensiveCopy(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))

	req := RegisterRequest{Agent: "a", Env: map[string]string{"FOO": "bar"}}
	session, err := store.Register(req)
	require.NoError(t, err)

	retrieved := store.Get(session.Token)
	require.NotNil(t, retrieved)

	// Mutate returned Env
	retrieved.Env["FOO"] = testEnvMutated
	retrieved.Env["NEW"] = testEnvAdded

	// Get again — original must be intact
	retrieved2 := store.Get(session.Token)
	require.NotNil(t, retrieved2)
	assert.Equal(t, "bar", retrieved2.Env["FOO"])
	_, hasNew2 := retrieved2.Env["NEW"]
	assert.False(t, hasNew2)
}

// TestList_EnvDefensiveCopy verifies List returns cloned sessions.
func TestList_EnvDefensiveCopy(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "sessions.json"))

	req := RegisterRequest{Agent: "a", Env: map[string]string{"FOO": "bar"}}
	session, err := store.Register(req)
	require.NoError(t, err)

	list := store.List()
	require.Len(t, list, 1)
	assert.Equal(t, session.Token, list[0].Token)

	// Mutate returned Env
	list[0].Env["FOO"] = testEnvMutated
	list[0].Env["NEW"] = testEnvAdded

	// Get again — original must be intact
	sess := store.Get(session.Token)
	require.NotNil(t, sess)
	assert.Equal(t, "bar", sess.Env["FOO"])
	_, hasNewSess := sess.Env["NEW"]
	assert.False(t, hasNewSess)
}
