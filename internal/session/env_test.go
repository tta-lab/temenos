package session

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvMapToSlice_Empty(t *testing.T) {
	result := EnvMapToSlice(nil)
	assert.Nil(t, result)
}

func TestEnvMapToSlice_SingleEntry(t *testing.T) {
	result := EnvMapToSlice(map[string]string{"FOO": "bar"})
	assert.Len(t, result, 1)
	assert.Equal(t, "FOO=bar", result[0])
}

func TestEnvMapToSlice_MultipleEntries(t *testing.T) {
	env := map[string]string{"FOO": "bar", "BAZ": "qux", "AAA": "zzz"}
	result := EnvMapToSlice(env)
	sort.Strings(result)
	assert.Len(t, result, 3)
	assert.Equal(t, []string{"AAA=zzz", "BAZ=qux", "FOO=bar"}, result)
}

func TestIsValidEnvName(t *testing.T) {
	valid := []string{"A", "_", "FOO", "FOO_BAR", "FOO_BAR_123", "a", "a1", "_1"}
	for _, s := range valid {
		assert.True(t, isValidEnvName(s), "expected %q to be valid", s)
	}

	invalid := []string{"", "1", "1FOO", "FOO=BAR", "FOO BAR", "FOO\tBAR", "FOO\nBAR"}
	for _, s := range invalid {
		assert.False(t, isValidEnvName(s), "expected %q to be invalid", s)
	}
}
