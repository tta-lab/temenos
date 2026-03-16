package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveSearcher_BraveWhenKeySet(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "test-key")
	s := resolveSearcher()
	_, ok := s.(*BraveSearcher)
	assert.True(t, ok, "expected BraveSearcher when BRAVE_API_KEY is set")
}

func TestResolveSearcher_DDGFallback(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	s := resolveSearcher()
	_, ok := s.(*DDGSearcher)
	assert.True(t, ok, "expected DDGSearcher when no API key is set")
}
