//go:build linux

package sandbox

import (
	"context"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rwBind  = "--bind"
	tmpPath = "/tmp"
)

func TestBwrapSandbox_BuildArgs(t *testing.T) {
	s := &BwrapSandbox{BwrapPath: "bwrap"}

	args := s.buildArgs("echo hello", nil)

	// Verify core bwrap flags are present
	assert.Contains(t, args, roBind)
	assert.Contains(t, args, "--unshare-all")
	assert.Contains(t, args, "--share-net")
	assert.Contains(t, args, "--die-with-parent")

	// Fresh procfs is needed for /proc/self/exe without exposing host /proc.
	foundProc := false
	for i, a := range args {
		if a == procArg && i+1 < len(args) && args[i+1] == staticProc {
			foundProc = true
		}
		if (a == roBind || a == rwBind) && i+2 < len(args) && args[i+1] == staticProc {
			t.Fatalf("host /proc must not be bind-mounted: %v", args[i:i+3])
		}
	}
	assert.True(t, foundProc, "expected bwrap --proc /proc")

	// Verify command is last
	require.GreaterOrEqual(t, len(args), 3)
	assert.Equal(t, "bash", args[len(args)-3])
	assert.Equal(t, "-c", args[len(args)-2])
	assert.Equal(t, "echo hello", args[len(args)-1])
}

func TestBwrapSandbox_BuildArgs_WithMounts(t *testing.T) {
	s := &BwrapSandbox{BwrapPath: "bwrap"}
	cfg := &ExecConfig{
		MountDirs: []Mount{
			{Source: "/usr", Target: "/usr", ReadOnly: true},
			{Source: tmpPath, Target: tmpPath, ReadOnly: false},
		},
	}

	args := s.buildArgs("ls", cfg)

	// Verify read-only mount uses --ro-bind
	foundROBind := false
	for i, a := range args {
		if a == roBind && i+2 < len(args) && args[i+1] == "/usr" && args[i+2] == "/usr" {
			foundROBind = true
		}
	}
	assert.True(t, foundROBind, "expected --ro-bind for /usr")

	// Verify writable mount uses --bind
	foundBind := false
	for i, a := range args {
		if a == rwBind && i+2 < len(args) && args[i+1] == tmpPath && args[i+2] == tmpPath {
			foundBind = true
		}
	}
	assert.True(t, foundBind, "expected --bind for /tmp")
}

func TestAppendNixStoreBind_AddsWhenStoreExists(t *testing.T) {
	prev := bwrapNixStoreStat
	bwrapNixStoreStat = func(path string) (fs.FileInfo, error) {
		require.Equal(t, bwrapNixStorePath, path)
		return nil, nil
	}
	t.Cleanup(func() { bwrapNixStoreStat = prev })

	args := appendNixStoreBind(nil)
	require.Len(t, args, 3)
	assert.Equal(t, "--ro-bind", args[0])
	assert.Equal(t, bwrapNixStorePath, args[1])
	assert.Equal(t, bwrapNixStorePath, args[2])
}

func TestAppendNixStoreBind_SkipsWhenStoreMissing(t *testing.T) {
	prev := bwrapNixStoreStat
	bwrapNixStoreStat = func(path string) (fs.FileInfo, error) {
		require.Equal(t, bwrapNixStorePath, path)
		return nil, fs.ErrNotExist
	}
	t.Cleanup(func() { bwrapNixStoreStat = prev })

	args := appendNixStoreBind(nil)
	for _, a := range args {
		assert.NotEqual(t, bwrapNixStorePath, a, "nix store path should be skipped when missing")
	}
}

func TestBuildArgs_IncludesNixStoreWhenAvailable(t *testing.T) {
	prev := bwrapNixStoreStat
	bwrapNixStoreStat = func(path string) (fs.FileInfo, error) {
		require.Equal(t, bwrapNixStorePath, path)
		return nil, nil
	}
	t.Cleanup(func() { bwrapNixStoreStat = prev })

	s := &BwrapSandbox{BwrapPath: "bwrap"}
	args := s.buildArgs("echo hello", nil)

	foundNixStore := false
	for i, a := range args {
		if a == "--ro-bind" && i+2 < len(args) && args[i+1] == bwrapNixStorePath {
			foundNixStore = true
			break
		}
	}
	assert.True(t, foundNixStore, "expected /nix/store ro-bind in buildArgs")
}

func TestBuildArgs_ExcludesNixStoreWhenMissing(t *testing.T) {
	prev := bwrapNixStoreStat
	bwrapNixStoreStat = func(path string) (fs.FileInfo, error) {
		require.Equal(t, bwrapNixStorePath, path)
		return nil, fs.ErrNotExist
	}
	t.Cleanup(func() { bwrapNixStoreStat = prev })

	s := &BwrapSandbox{BwrapPath: "bwrap"}
	args := s.buildArgs("echo hello", nil)

	for _, a := range args {
		assert.NotEqual(t, bwrapNixStorePath, a, "nix store path should be excluded when missing")
	}
}

func TestBwrapSandbox_BuildArgs_WithWorkingDir(t *testing.T) {
	s := &BwrapSandbox{BwrapPath: "bwrap"}
	cfg := &ExecConfig{
		MountDirs:  []Mount{{Source: "/data", Target: "/data", ReadOnly: true}},
		WorkingDir: "/data",
	}

	args := s.buildArgs("ls", cfg)

	// Find --chdir and -- positions
	chdirIdx := -1
	separatorIdx := -1
	for i, a := range args {
		if a == "--chdir" {
			chdirIdx = i
		}
		if a == "--" {
			separatorIdx = i
		}
	}
	require.NotEqual(t, -1, chdirIdx, "expected --chdir flag")
	require.NotEqual(t, -1, separatorIdx, "expected -- separator")
	assert.Less(t, chdirIdx, separatorIdx, "--chdir must appear before -- separator")
	assert.Equal(t, "/data", args[chdirIdx+1])
}

func TestBwrapSandbox_BuildArgs_NoWorkingDir(t *testing.T) {
	s := &BwrapSandbox{BwrapPath: "bwrap"}
	args := s.buildArgs("ls", nil)

	for _, a := range args {
		assert.NotEqual(t, "--chdir", a, "--chdir should not appear when WorkingDir is empty")
	}
}

func TestCoveredByStaticRoot(t *testing.T) {
	assert.True(t, coveredByStaticRoot("/usr"))
	assert.True(t, coveredByStaticRoot("/usr/local"))
	assert.True(t, coveredByStaticRoot("/usr/local/bin"))
	assert.True(t, coveredByStaticRoot("/bin"))
	assert.True(t, coveredByStaticRoot("/lib"))
	assert.False(t, coveredByStaticRoot("/opt/homebrew"))
	assert.False(t, coveredByStaticRoot("/snap"))
	assert.False(t, coveredByStaticRoot("/home/linuxbrew"))
}

// setupTempToolDir creates a temp directory, points HOME at it so
// dynamicToolDirs discovers a .cargo/bin entry, and resets the cache.
// Returns a cleanup function. NOT parallel-safe (resets sync.Once cache).
func setupTempToolDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	cargoBin := tmp + "/.cargo/bin"
	require.NoError(t, os.MkdirAll(cargoBin, 0o755))
	t.Setenv("HOME", tmp)
	t.Setenv("GOPATH", "/nonexistent/gopath")
	resetToolDirsCache()
	t.Cleanup(resetToolDirsCache)
}

func TestAppendBwrapToolBinds_SkipsStaticRoots(t *testing.T) {
	setupTempToolDir(t)
	args := appendBwrapToolBinds(nil)
	require.NotEmpty(t, args, "expected at least one --ro-bind from temp tool dir")
	for i, a := range args {
		if a == roBind && i+1 < len(args) {
			path := args[i+1]
			assert.False(t, coveredByStaticRoot(path),
				"appendBwrapToolBinds should not bind %s (covered by static root)", path)
		}
	}
}

func TestBwrapSandbox_MemoryLimit_Degradation(t *testing.T) {
	sbx := &BwrapSandbox{
		BwrapPath:     "bwrap",
		Timeout:       5 * time.Second,
		MemoryLimitMB: 128,
	}
	if !sbx.IsAvailable() {
		t.Skip("bwrap not available")
	}
	if !cgroupAvailable() {
		t.Skip("cgroup v2 not available")
	}
	// Requires execCgroupBase to be set (full daemon init-leaf setup).
	// In CI the cgroup path can't be discovered, so skip.
	if execCgroupBase == "" {
		t.Skip("execCgroupBase not set (requires full cgroup v2 init-leaf setup)")
	}
	// Verify bwrap is actually installed; IsAvailable checks PATH only.
	if _, err := os.Stat(sbx.BwrapPath); err != nil {
		t.Skip("bwrap not installed: " + sbx.BwrapPath)
	}

	stdout, stderr, exitCode, err := sbx.Exec(context.Background(), "echo hello", nil)
	if err != nil {
		t.Fatalf("Exec failed: %v (stderr: %s)", err, stderr)
	}
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 (stderr: %s)", exitCode, stderr)
	}
	if !strings.Contains(stdout, "hello") {
		t.Errorf("stdout = %q, want 'hello'", stdout)
	}
}

func TestBwrapSandbox_ExecProvidesProcSelfExe(t *testing.T) {
	sbx := &BwrapSandbox{BwrapPath: "bwrap", Timeout: 5 * time.Second}
	if !sbx.IsAvailable() {
		t.Skip("bwrap not available")
	}

	stdout, stderr, exitCode, err := sbx.Exec(context.Background(), "readlink /proc/self/exe", nil)

	require.NoError(t, err, "stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.NotEmpty(t, strings.TrimSpace(stdout))
}

func TestAppendBwrapToolBinds_NoDuplicates(t *testing.T) {
	setupTempToolDir(t)
	args := appendBwrapToolBinds(nil)
	require.NotEmpty(t, args, "expected at least one --ro-bind from temp tool dir")
	seen := make(map[string]bool)
	for i, a := range args {
		if a == roBind && i+1 < len(args) {
			path := args[i+1]
			assert.False(t, seen[path], "duplicate --ro-bind for %s", path)
			seen[path] = true
		}
	}
}

func TestBwrapSandbox_BuildArgs_MetadataOnlySkipped(t *testing.T) {
	s := &BwrapSandbox{BwrapPath: "bwrap"}
	cfg := &ExecConfig{
		MountDirs: []Mount{
			{Source: "/tmp", Target: "/tmp", ReadOnly: true},
			{Source: "/opt", Target: "/opt", MetadataOnly: true},
		},
	}

	args := s.buildArgs("ls", cfg)

	// /tmp should be bound read-only.
	foundBind := false
	for i, a := range args {
		if a == roBind && i+2 < len(args) && args[i+1] == "/tmp" {
			foundBind = true
		}
	}
	assert.True(t, foundBind, "expected --ro-bind for /tmp")

	// MetadataOnly mount /opt must NOT appear in args — seatbelt concept only.
	for _, a := range args {
		assert.NotEqual(t, "/opt", a, "MetadataOnly mount /opt should not appear in bwrap args")
	}
}

func TestBwrapSandbox_BuildArgs_SkipsMissingSources(t *testing.T) {
	s := &BwrapSandbox{BwrapPath: "bwrap"}
	cfg := &ExecConfig{
		MountDirs: []Mount{
			{Source: "/nonexistent/path", Target: "/nonexistent/path", ReadOnly: true},
			{Source: tmpPath, Target: tmpPath, ReadOnly: true},
		},
	}
	args := s.buildArgs("echo hello", cfg)

	// /nonexistent/path should be skipped
	for _, a := range args {
		assert.NotEqual(t, "/nonexistent/path", a,
			"missing source path should be skipped in bwrap args")
	}

	// /tmp should still be present (as a bind, though also as tmpfs — check ro-bind)
	foundTmpBind := false
	for i, a := range args {
		if a == roBind && i+1 < len(args) && args[i+1] == tmpPath {
			foundTmpBind = true
		}
	}
	assert.True(t, foundTmpBind, "existing source path /tmp should be bound")
}

func TestBwrapSandbox_BuildArgs_KubernetesMode(t *testing.T) {
	s := &BwrapSandbox{BwrapPath: "bwrap", KubernetesMode: true}

	args := s.buildArgs("echo hello", nil)

	// In Kubernetes mode, --proc /proc and --unshare-pid must be absent.
	foundProc := false
	foundUnsharePID := false
	for i, a := range args {
		if a == "--proc" && i+1 < len(args) && args[i+1] == "/proc" {
			foundProc = true
		}
		if a == "--unshare-pid" {
			foundUnsharePID = true
		}
	}
	assert.False(t, foundProc, "expected no --proc /proc in k8s mode")
	assert.False(t, foundUnsharePID, "expected no --unshare-pid in k8s mode")

	// Still should have --unshare-all which, without --unshare-pid, will skip PID namespace.
	assert.Contains(t, args, "--unshare-all")
}
