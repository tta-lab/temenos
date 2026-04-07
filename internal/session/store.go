package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrValidation is returned by Register when request fields fail validation.
// Callers (e.g. HTTP handlers) can check errors.Is(err, ErrValidation) to
// distinguish client input errors from internal failures.
var ErrValidation = errors.New("session validation error")

const defaultTTL = 8 * time.Hour

// Session represents a sandbox session token.
type Session struct {
	Token      string   `json:"token"`
	Agent      string   `json:"agent"`
	WritePaths []string `json:"write_paths"` // paths mounted read-write in the sandbox
	ReadPaths  []string `json:"read_paths"`  // paths mounted read-only in the sandbox
	// Env: session-scoped environment variables. Returned by List() — admin socket
	// is trusted; do not expose List() over untrusted transports.
	Env       map[string]string `json:"env,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	ExpiresAt time.Time         `json:"expires_at"` // default: CreatedAt + 8h
}

// RegisterRequest is the request to create a new session.
// Env is used by MCP bash for session-scoped env injection.
type RegisterRequest struct {
	Agent      string            `json:"agent"`
	WritePaths []string          `json:"write_paths,omitempty"` // paths to mount read-write
	ReadPaths  []string          `json:"read_paths,omitempty"`  // paths to mount read-only
	Env        map[string]string `json:"env,omitempty"`
}

// Store manages session tokens in memory with disk persistence.
type Store struct {
	mu         sync.RWMutex
	sessions   map[string]*Session
	filePath   string
	defaultTTL time.Duration
}

// NewStore creates a new session store with the given file path for persistence.
func NewStore(filePath string) *Store {
	return &Store{
		sessions:   make(map[string]*Session),
		filePath:   filePath,
		defaultTTL: defaultTTL,
	}
}

// generateToken creates a 64-character hex token from 32 random bytes.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// persistLocked writes the session map to disk atomically.
// Caller must hold s.mu (read or write lock).
func (s *Store) persistLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmpPath := s.filePath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(tmpPath) //nolint:errcheck

	if err := json.NewEncoder(f).Encode(s.sessions); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	return os.Rename(tmpPath, s.filePath)
}

// persist acquires a read lock and persists the session map atomically.
func (s *Store) persist() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.persistLocked()
}

// validateEnv returns an ErrValidation-wrapped error if any env key or value
// is invalid. Key rules: non-empty, no NUL, no whitespace, no '=', must match
// POSIX name pattern [a-zA-Z_][a-zA-Z0-9_]*. Value rules: no NUL, no LF, no CR.
// Empty values are valid. Shell metacharacters ($ ` ${}) are allowed — env vars
// are passed via execve KEY=VALUE pairs, not shell-interpreted.
func validateEnv(env map[string]string) error {
	for k, v := range env {
		if k == "" {
			return fmt.Errorf("%w: env: key must not be empty", ErrValidation)
		}
		if strings.ContainsRune(k, 0) {
			return fmt.Errorf("%w: env: key %q contains NUL byte", ErrValidation, k)
		}
		if strings.ContainsAny(k, " \t\n\r=") {
			return fmt.Errorf("%w: env: key %q contains whitespace or '='", ErrValidation, k)
		}
		if len(k) > 0 && !isValidEnvName(k) {
			return fmt.Errorf("%w: env: key %q must match [a-zA-Z_][a-zA-Z0-9_]*", ErrValidation, k)
		}
		if strings.ContainsAny(v, "\x00\n\r") {
			return fmt.Errorf("%w: env: value for key %q contains NUL, LF, or CR", ErrValidation, k)
		}
	}
	return nil
}

// isValidEnvName returns true if s is a valid POSIX env var name:
// [a-zA-Z_][a-zA-Z0-9_]*.
func isValidEnvName(s string) bool {
	if len(s) == 0 {
		return false
	}
	if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_", rune(s[0])) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_", rune(s[i])) {
			return false
		}
	}
	return true
}

// clone returns a deep copy of the session.
func (s *Session) clone() *Session {
	c := *s
	if s.Env != nil {
		c.Env = make(map[string]string, len(s.Env))
		for k, v := range s.Env {
			c.Env[k] = v
		}
	}
	return &c
}

// EnvMapToSlice converts a map of env vars to a KEY=VALUE string slice.
// The result is deterministic if the input is nil or empty.
func EnvMapToSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

// validatePaths returns an ErrValidation-wrapped error if any path is empty,
// non-absolute, or if WritePaths and ReadPaths overlap (conflicting mounts).
func validatePaths(write, read []string) error {
	seen := make(map[string]struct{}, len(write))
	for _, p := range write {
		if p == "" {
			return fmt.Errorf("%w: write_paths: empty string is not a valid path", ErrValidation)
		}
		if !filepath.IsAbs(p) {
			return fmt.Errorf("%w: write_paths: path must be absolute: %q", ErrValidation, p)
		}
		seen[filepath.Clean(p)] = struct{}{}
	}
	for _, p := range read {
		if p == "" {
			return fmt.Errorf("%w: read_paths: empty string is not a valid path", ErrValidation)
		}
		if !filepath.IsAbs(p) {
			return fmt.Errorf("%w: read_paths: path must be absolute: %q", ErrValidation, p)
		}
		if _, dup := seen[filepath.Clean(p)]; dup {
			return fmt.Errorf("%w: read_paths: path %q also appears in write_paths (conflicting mounts)", ErrValidation, p)
		}
	}
	return nil
}

// Register creates a new session with the given request.
func (s *Store) Register(req RegisterRequest) (*Session, error) {
	if err := validatePaths(req.WritePaths, req.ReadPaths); err != nil {
		return nil, err
	}
	if err := validateEnv(req.Env); err != nil {
		return nil, err
	}

	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	sess := &Session{
		Token:      token,
		Agent:      req.Agent,
		WritePaths: req.WritePaths,
		ReadPaths:  req.ReadPaths,
		CreatedAt:  now,
		ExpiresAt:  now.Add(s.defaultTTL),
	}
	if req.Env != nil {
		sess.Env = make(map[string]string, len(req.Env))
		for k, v := range req.Env {
			sess.Env[k] = v
		}
	}

	s.mu.Lock()
	s.sessions[token] = sess
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return nil, err
	}

	return sess, nil
}

// Delete removes a session by token. Returns nil if not found (idempotent).
func (s *Store) Delete(token string) error {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
	return s.persist()
}

// Get returns the session for the given token, or nil if not found or expired.
func (s *Store) Get(token string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[token]
	if !ok {
		return nil
	}
	if time.Now().After(session.ExpiresAt) {
		return nil
	}
	return session.clone()
}

// List returns all non-expired sessions.
func (s *Store) List() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	var result []Session
	for _, session := range s.sessions {
		if now.Before(session.ExpiresAt) {
			result = append(result, *session.clone())
		}
	}
	return result
}

// LoadFromDisk loads sessions from the JSON file, replacing the in-memory map.
// Called on daemon startup. Skips expired sessions.
func (s *Store) LoadFromDisk() error {
	f, err := os.Open(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	var sessions map[string]*Session
	if err := json.NewDecoder(f).Decode(&sessions); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions = make(map[string]*Session)
	now := time.Now()
	for token, session := range sessions {
		if now.Before(session.ExpiresAt) {
			s.sessions[token] = session
		}
	}
	return nil
}

// PruneStale removes sessions where any WritePaths or ReadPaths directory no longer
// exists, or where ExpiresAt has passed.
func (s *Store) PruneStale() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	changed := false

	for token, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, token)
			changed = true
			continue
		}

		stale := false
		for _, path := range append(session.WritePaths, session.ReadPaths...) {
			info, err := os.Stat(path)
			if err != nil {
				slog.Warn("session store: pruning session — path stat failed",
					"agent", session.Agent, "path", path, "err", err)
				stale = true
				break
			}
			if !info.IsDir() {
				slog.Warn("session store: pruning session — path is not a directory",
					"agent", session.Agent, "path", path)
				stale = true
				break
			}
		}
		if stale {
			delete(s.sessions, token)
			changed = true
		}
	}

	if changed {
		if err := s.persistLocked(); err != nil {
			slog.Warn("session store: failed to persist after prune", "err", err)
		}
	}
}
