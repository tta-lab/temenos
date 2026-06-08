package auth

import (
	"context"
	"sync"
	"time"
)

// validateFunc is the underlying token validation function, used for tests.
var validateFunc = ValidateToken

type cacheEntry struct {
	username  string
	expiresAt time.Time
}

type tokenCache struct {
	entries map[string]cacheEntry
	ttl     time.Duration
	mu      sync.RWMutex
	done    chan struct{}
}

func newTokenCache(ttl time.Duration) *tokenCache {
	c := &tokenCache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
		done:    make(chan struct{}),
	}
	go c.sweep()
	return c
}

func (c *tokenCache) sweep() {
	ticker := time.NewTicker(c.ttl / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.deleteExpired()
		case <-c.done:
			return
		}
	}
}

func (c *tokenCache) deleteExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for token, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, token)
		}
	}
}

func (c *tokenCache) close() {
	close(c.done)
}

func (c *tokenCache) get(token string) (string, bool) {
	c.mu.RLock()
	entry, ok := c.entries[token]
	c.mu.RUnlock()

	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.entries, token)
		c.mu.Unlock()
		return "", false
	}
	return entry.username, true
}

func (c *tokenCache) set(token, username string) {
	c.mu.Lock()
	c.entries[token] = cacheEntry{
		username:  username,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// CachedTokenValidator wraps ValidateToken with an in-memory cache.
// Same token string returns the same username within the TTL window.
type CachedTokenValidator struct {
	cache *tokenCache
}

// NewCachedTokenValidator creates a CachedTokenValidator with the given cache TTL.
func NewCachedTokenValidator(ttl time.Duration) *CachedTokenValidator {
	return &CachedTokenValidator{cache: newTokenCache(ttl)}
}

// ValidateToken checks the cache first, then falls back to the underlying
// validateFunc (ValidateToken by default).
func (c *CachedTokenValidator) ValidateToken(ctx context.Context, token, baseURL string) (string, error) {
	if username, ok := c.cache.get(token); ok {
		return username, nil
	}

	username, err := validateFunc(ctx, token, baseURL)
	if err != nil {
		return "", err
	}

	c.cache.set(token, username)
	return username, nil
}
