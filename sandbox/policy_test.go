package sandbox

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPolicy_NoMounts(t *testing.T) {
	policy, params, err := buildPolicy(nil)
	require.NoError(t, err)
	assert.NotEmpty(t, policy)
	assert.Contains(t, policy, "(version 1)")
	assert.Contains(t, policy, "(deny default)")
	assert.Contains(t, policy, "(allow network-outbound)")
	// DARWIN_USER_CACHE_DIR param should always be present
	found := false
	for _, p := range params {
		if strings.HasPrefix(p, "DARWIN_USER_CACHE_DIR=") {
			found = true
		}
	}
	assert.True(t, found, "DARWIN_USER_CACHE_DIR should be in params")
}

func TestBuildPolicy_ReadOnlyMount(t *testing.T) {
	cfg := &ExecConfig{
		MountDirs: []Mount{
			{Source: "/some/path", Target: "/some/path", ReadOnly: true},
		},
	}
	policy, params, err := buildPolicy(cfg)
	require.NoError(t, err)
	assert.Contains(t, policy, `(allow file-read* (subpath (param "READABLE_ROOT_0")))`)
	assert.Contains(t, params, "READABLE_ROOT_0=/some/path")
}

func TestBuildPolicy_WritableMount(t *testing.T) {
	cfg := &ExecConfig{
		MountDirs: []Mount{
			{Source: "/rw/path", Target: "/rw/path", ReadOnly: false},
		},
	}
	policy, params, err := buildPolicy(cfg)
	require.NoError(t, err)
	assert.Contains(t, policy, `(allow file-read* file-write* (subpath (param "WRITABLE_ROOT_0")))`)
	assert.Contains(t, params, "WRITABLE_ROOT_0=/rw/path")
}

func TestBuildPolicy_MountParams(t *testing.T) {
	cfg := &ExecConfig{
		MountDirs: []Mount{
			{Source: "/ro1", Target: "/ro1", ReadOnly: true},
			{Source: "/ro2", Target: "/ro2", ReadOnly: true},
			{Source: "/rw1", Target: "/rw1", ReadOnly: false},
		},
	}
	policy, params, err := buildPolicy(cfg)
	require.NoError(t, err)
	assert.Contains(t, policy, `"READABLE_ROOT_0"`)
	assert.Contains(t, policy, `"READABLE_ROOT_1"`)
	assert.Contains(t, policy, `"WRITABLE_ROOT_0"`)
	assert.Contains(t, params, "READABLE_ROOT_0=/ro1")
	assert.Contains(t, params, "READABLE_ROOT_1=/ro2")
	assert.Contains(t, params, "WRITABLE_ROOT_0=/rw1")
}

func TestBuildPolicy_SourceTargetMismatch(t *testing.T) {
	cfg := &ExecConfig{
		MountDirs: []Mount{
			{Source: "/source", Target: "/target", ReadOnly: true},
		},
	}
	_, _, err := buildPolicy(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot remap paths")
	assert.Contains(t, err.Error(), "/source")
	assert.Contains(t, err.Error(), "/target")
}
