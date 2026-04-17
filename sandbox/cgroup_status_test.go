//go:build linux

package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper: build a fake cgroup v2 tree under tmpDir with a delegated subdir
// named "leaf" containing cgroup.controllers listing "memory cpu". Returns
// the root path. The procfile is written separately by the caller so tests
// can vary mode.
func setupFakeCgroupRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte("memory cpu"), 0o644))
	leaf := filepath.Join(root, "leaf")
	require.NoError(t, os.MkdirAll(leaf, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(leaf, "cgroup.controllers"), []byte("memory cpu"), 0o644))
	return root
}

// writeProcFile writes a v2-format /proc/self/cgroup membership file pointing
// at "/leaf" relative to the fake root.
func writeProcFile(t *testing.T, dir string) string {
	t.Helper()
	procPath := filepath.Join(dir, "proc.cgroup")
	require.NoError(t, os.WriteFile(procPath, []byte("0::/leaf\n"), 0o644))
	return procPath
}

// State #1 baseline: writable leaf, memory controller present → (true, nil).
func TestCheckCgroupAvailable_State1_Healthy(t *testing.T) {
	root := setupFakeCgroupRoot(t)
	procFile := writeProcFile(t, root)
	ok, reason := checkCgroupAvailableWith(root, procFile)
	assert.True(t, ok)
	assert.NoError(t, reason)
}

// State #2: leaf exists but is not writable (chmod 0o500 simulates pod
// without runtimeClassName: cgroup-writable, or older containerd).
func TestCheckCgroupAvailable_State2_NotWritable(t *testing.T) {
	root := setupFakeCgroupRoot(t)
	procFile := writeProcFile(t, root)
	leaf := filepath.Join(root, "leaf")
	require.NoError(t, os.Chmod(leaf, 0o500))
	t.Cleanup(func() { _ = os.Chmod(leaf, 0o755) })

	// Skip when running as root — root bypasses W_OK.
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses W_OK; cannot exercise state #2 deterministically")
	}

	ok, reason := checkCgroupAvailableWith(root, procFile)
	assert.False(t, ok)
	assert.True(t, errors.Is(reason, ErrCgroupNotWritable),
		"expected ErrCgroupNotWritable, got %v", reason)
}

// State #3: cgroup.controllers is missing → cgroup v2 not mounted.
func TestCheckCgroupAvailable_State3_NotMounted(t *testing.T) {
	root := t.TempDir() // no cgroup.controllers written
	procFile := writeProcFile(t, root)
	ok, reason := checkCgroupAvailableWith(root, procFile)
	assert.False(t, ok)
	assert.True(t, errors.Is(reason, ErrCgroupNotMounted),
		"expected ErrCgroupNotMounted, got %v", reason)
}

// Memory controller missing: cgroup.controllers exists but doesn't list memory.
func TestCheckCgroupAvailable_MemoryControllerMissing(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte("cpu io"), 0o644))
	leaf := filepath.Join(root, "leaf")
	require.NoError(t, os.MkdirAll(leaf, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(leaf, "cgroup.controllers"), []byte("cpu io"), 0o644))
	procFile := writeProcFile(t, root)

	ok, reason := checkCgroupAvailableWith(root, procFile)
	assert.False(t, ok)
	assert.True(t, errors.Is(reason, ErrCgroupMemoryControllerOff),
		"expected ErrCgroupMemoryControllerOff, got %v", reason)
}

// Delegated path discovery failure: empty/malformed procfile.
func TestCheckCgroupAvailable_NoDelegatedPath(t *testing.T) {
	root := setupFakeCgroupRoot(t)
	procFile := filepath.Join(t.TempDir(), "empty.cgroup")
	require.NoError(t, os.WriteFile(procFile, []byte(""), 0o644))

	ok, reason := checkCgroupAvailableWith(root, procFile)
	assert.False(t, ok)
	assert.True(t, errors.Is(reason, ErrCgroupNoDelegatedPath),
		"expected ErrCgroupNoDelegatedPath, got %v", reason)
}
