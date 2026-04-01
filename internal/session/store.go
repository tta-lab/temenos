package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultTTL = 8 * time.Hour

// Session represents a sandbox session token.
type Session struct {
	Token      string    `json:"token"`
	Agent      string    `json:"agent"`
	Access     string    `json:"access"`      // "rw" or "ro"
	WritePaths []string  `json:"write_paths"` // additional write paths beyond config baseline
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"` // default: CreatedAt + 8h
}

// RegisterRequest is the request to create a new session.
type RegisterRequest struct {
	Agent      string   `json:"agent"`
	Access     string   `json:"access"` // "rw" or "ro"
	WritePaths []string `json:"write_paths,omitempty"`
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

// Register creates a new session with the given request.
func (s *Store) Register(req RegisterRequest) (*Session, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	session := &Session{
		Token:      token,
		Agent:      req.Agent,
		Access:     req.Access,
		WritePaths: req.WritePaths,
		CreatedAt:  now,
		ExpiresAt:  now.Add(s.defaultTTL),
	}

	s.mu.Lock()
	s.sessions[token] = session
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return nil, err
	}

	return session, nil
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
	return session
}

// List returns all non-expired sessions.
func (s *Store) List() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	var result []Session
	for _, session := range s.sessions {
		if now.Before(session.ExpiresAt) {
			result = append(result, *session)
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

// PruneStale removes sessions where WritePaths dirs don't exist or ExpiresAt has passed.
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
		for _, path := range session.WritePaths {
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() {
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
