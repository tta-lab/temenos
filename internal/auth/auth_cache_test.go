package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTokenCache_CachedHitReturnsSameUser(t *testing.T) {
	cache := newTokenCache(5 * time.Minute)

	cache.set("token-abc", "user:ns:sa")
	username, ok := cache.get("token-abc")

	assert.True(t, ok)
	assert.Equal(t, "user:ns:sa", username)
}

func TestTokenCache_MissReturnsFalse(t *testing.T) {
	cache := newTokenCache(5 * time.Minute)

	_, ok := cache.get("nonexistent")

	assert.False(t, ok)
}

func TestTokenCache_EntryExpiresAfterTTL(t *testing.T) {
	cache := newTokenCache(1 * time.Millisecond)

	cache.set("token-abc", "user:ns:sa")
	time.Sleep(5 * time.Millisecond)

	_, ok := cache.get("token-abc")
	assert.False(t, ok, "expired entry should not be returned")
}

func TestTokenCache_NonExpiredEntryStillValid(t *testing.T) {
	cache := newTokenCache(5 * time.Second)

	cache.set("token-abc", "user:ns:sa")
	time.Sleep(1 * time.Millisecond)

	username, ok := cache.get("token-abc")
	assert.True(t, ok, "still-valid entry should be returned")
	assert.Equal(t, "user:ns:sa", username)
}

func TestTokenCache_ValidateTokenUsesCache(t *testing.T) {
	callCount := 0
	created := make(chan struct{})
	close(created)

	prev := validateFunc
	validateFunc = func(ctx context.Context, token, baseURL string) (string, error) {
		callCount++
		<-created
		return "user-1", nil
	}
	defer func() { validateFunc = prev }()

	ctx := context.Background()
	cached := NewCachedTokenValidator(5 * time.Minute)
	u1, _ := cached.ValidateToken(ctx, "same-token", "https://k8s")
	u2, _ := cached.ValidateToken(ctx, "same-token", "https://k8s")

	assert.Equal(t, "user-1", u1)
	assert.Equal(t, "user-1", u2)
	assert.Equal(t, 1, callCount, "second call should use cache, not underlier")
}

func TestTokenCache_ConcurrentAccess(t *testing.T) {
	cache := newTokenCache(5 * time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.set("token", "user-1")
			cache.get("token")
		}()
	}
	wg.Wait()
}

func TestTokenCache_SweepRemovesExpiredEntries(t *testing.T) {
	cache := newTokenCache(5 * time.Millisecond)
	defer cache.close()

	cache.set("token-abc", "user-1")
	cache.set("token-xyz", "user-2")

	// Wait long enough for all entries to expire and the sweep to run.
	time.Sleep(20 * time.Millisecond)

	cache.mu.RLock()
	count := len(cache.entries)
	cache.mu.RUnlock()

	assert.Equal(t, 0, count, "sweep should remove all expired entries")
}

func TestTokenCache_SweepKeepsValidEntries(t *testing.T) {
	cache := newTokenCache(5 * time.Second)
	defer cache.close()

	cache.set("token-abc", "user-1")
	time.Sleep(10 * time.Millisecond)

	cache.mu.RLock()
	count := len(cache.entries)
	cache.mu.RUnlock()

	assert.Equal(t, 1, count, "valid entry should not be swept")
}

func TestTokenCache_CloseStopsSweep(t *testing.T) {
	cache := newTokenCache(1 * time.Millisecond)
	cache.close()

	// Insert after close — sweep is no longer running.
	cache.set("token-abc", "user-1")
	time.Sleep(10 * time.Millisecond)

	cache.mu.RLock()
	count := len(cache.entries)
	cache.mu.RUnlock()

	assert.Equal(t, 1, count, "entry inserted after close should remain")
}
