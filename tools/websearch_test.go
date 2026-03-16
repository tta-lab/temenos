package tools

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSearcher_BraveWhenKeySet(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "test-key")
	s, err := resolveSearcher()
	require.NoError(t, err)
	_, ok := s.(*BraveSearcher)
	assert.True(t, ok, "expected BraveSearcher when BRAVE_API_KEY is set")
}

func TestResolveSearcher_DDGFallback(t *testing.T) {
	prev, hadPrev := os.LookupEnv("BRAVE_API_KEY")
	os.Unsetenv("BRAVE_API_KEY") //nolint:errcheck
	t.Cleanup(func() {
		if hadPrev {
			os.Setenv("BRAVE_API_KEY", prev) //nolint:errcheck
		} else {
			os.Unsetenv("BRAVE_API_KEY") //nolint:errcheck
		}
	})
	s, err := resolveSearcher()
	require.NoError(t, err)
	_, ok := s.(*DDGSearcher)
	assert.True(t, ok, "expected DDGSearcher when BRAVE_API_KEY is not set")
}

func TestResolveSearcher_EmptyKeyError(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	_, err := resolveSearcher()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BRAVE_API_KEY is set but empty")
}
