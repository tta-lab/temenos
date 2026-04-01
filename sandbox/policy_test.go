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
	// Control environment so dynamic tool dirs don't shift indices.
	t.Setenv("GOPATH", "/nonexistent/gopath")
	t.Setenv("HOME", "/nonexistent/home")
	resetToolDirsCache()
	t.Cleanup(resetToolDirsCache)

	cfg := &ExecConfig{
		MountDirs: []Mount{
			{Source: "/ro1", Target: "/ro1", ReadOnly: true},
			{Source: "/ro2", Target: "/ro2", ReadOnly: true},
			{Source: "/rw1", Target: "/rw1", ReadOnly: false},
		},
	}
	policy, params, err := buildPolicy(cfg)
	require.NoError(t, err)

	// Assert by value rather than numbered key — static tool dirs
	// may inject READABLE_ROOT entries before per-request mounts,
	// shifting indices depending on the machine.
	foundRO1, foundRO2, foundRW1 := false, false, false
	for _, p := range params {
		switch {
		case strings.HasSuffix(p, "=/ro1") && strings.HasPrefix(p, "READABLE_ROOT_"):
			foundRO1 = true
			// Verify the policy references this param.
			key := strings.SplitN(p, "=", 2)[0]
			assert.Contains(t, policy, `"`+key+`"`)
		case strings.HasSuffix(p, "=/ro2") && strings.HasPrefix(p, "READABLE_ROOT_"):
			foundRO2 = true
			key := strings.SplitN(p, "=", 2)[0]
			assert.Contains(t, policy, `"`+key+`"`)
		case strings.HasSuffix(p, "=/rw1") && strings.HasPrefix(p, "WRITABLE_ROOT_"):
			foundRW1 = true
			key := strings.SplitN(p, "=", 2)[0]
			assert.Contains(t, policy, `"`+key+`"`)
		}
	}
	assert.True(t, foundRO1, "expected a READABLE_ROOT param for /ro1")
	assert.True(t, foundRO2, "expected a READABLE_ROOT param for /ro2")
	assert.True(t, foundRW1, "expected a WRITABLE_ROOT param for /rw1")
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

func TestBuildPolicy_MetadataOnlyMount(t *testing.T) {
	cfg := &ExecConfig{
		MountDirs: []Mount{
			{Source: "/some/ancestor", Target: "/some/ancestor", MetadataOnly: true},
		},
	}
	policy, params, err := buildPolicy(cfg)
	require.NoError(t, err)

	// Should emit file-read-metadata literal for the MetadataOnly mount.
	assert.Contains(t, policy, `(allow file-read-metadata (literal (param "METADATA_ROOT_0")))`)
	// The MetadataOnly mount must NOT get a READABLE_ROOT or WRITABLE_ROOT param.
	for _, p := range params {
		assert.False(t,
			strings.Contains(p, "READABLE_ROOT") && strings.HasSuffix(p, "=/some/ancestor"),
			"MetadataOnly mount should not emit READABLE_ROOT param, got: %s", p)
		assert.False(t,
			strings.Contains(p, "WRITABLE_ROOT") && strings.HasSuffix(p, "=/some/ancestor"),
			"MetadataOnly mount should not emit WRITABLE_ROOT param, got: %s", p)
	}
	assert.Contains(t, params, "METADATA_ROOT_0=/some/ancestor")
}

func TestBuildPolicy_MetadataOnly_SourceTargetMismatchSkipped(t *testing.T) {
	// MetadataOnly mounts skip the Source \!= Target check.
	cfg := &ExecConfig{
		MountDirs: []Mount{
			{Source: "/ancestor", Target: "/different", MetadataOnly: true},
		},
	}
	_, _, err := buildPolicy(cfg)
	require.NoError(t, err, "MetadataOnly mounts should not trigger source/target mismatch error")
}

func TestBuildPolicy_MixedMounts(t *testing.T) {
	cfg := &ExecConfig{
		MountDirs: []Mount{
			{Source: "/rw/path", Target: "/rw/path", ReadOnly: false},
			{Source: "/ancestor", Target: "/ancestor", MetadataOnly: true},
			{Source: "/ro/path", Target: "/ro/path", ReadOnly: true},
		},
	}
	policy, params, err := buildPolicy(cfg)
	require.NoError(t, err)

	// Verify all three rule types are present.
	hasWritable := false
	hasReadable := false
	hasMetadata := false
	for _, p := range params {
		switch {
		case strings.HasSuffix(p, "=/rw/path") && strings.HasPrefix(p, "WRITABLE_ROOT_"):
			hasWritable = true
		case strings.HasSuffix(p, "=/ro/path") && strings.HasPrefix(p, "READABLE_ROOT_"):
			hasReadable = true
		case strings.HasSuffix(p, "=/ancestor") && strings.HasPrefix(p, "METADATA_ROOT_"):
			hasMetadata = true
		}
	}
	assert.True(t, hasWritable, "expected WRITABLE_ROOT param for /rw/path")
	assert.True(t, hasReadable, "expected READABLE_ROOT param for /ro/path")
	assert.True(t, hasMetadata, "expected METADATA_ROOT param for /ancestor")

	assert.Contains(t, policy, `file-read-metadata (literal`)
}
