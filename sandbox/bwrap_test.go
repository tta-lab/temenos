package sandbox

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const roBind = "--ro-bind"

func TestBwrapSandbox_BuildArgs(t *testing.T) {
	s := &BwrapSandbox{BwrapPath: "bwrap"}

	args := s.buildArgs("echo hello", nil)

	// Verify core bwrap flags are present
	assert.Contains(t, args, roBind)
	assert.Contains(t, args, "--unshare-all")
	assert.Contains(t, args, "--share-net")
	assert.Contains(t, args, "--die-with-parent")

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
			{Source: "/data", Target: "/data", ReadOnly: true},
			{Source: "/writable", Target: "/writable", ReadOnly: false},
		},
	}

	args := s.buildArgs("ls", cfg)

	// Verify read-only mount uses --ro-bind
	foundROBind := false
	for i, a := range args {
		if a == roBind && i+2 < len(args) && args[i+1] == "/data" && args[i+2] == "/data" {
			foundROBind = true
		}
	}
	assert.True(t, foundROBind, "expected --ro-bind for /data")

	// Verify writable mount uses --bind
	foundBind := false
	for i, a := range args {
		if a == "--bind" && i+2 < len(args) && args[i+1] == "/writable" && args[i+2] == "/writable" {
			foundBind = true
		}
	}
	assert.True(t, foundBind, "expected --bind for /writable")
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
			{Source: "/project/code", Target: "/project/code", ReadOnly: true},
			{Source: "/project", Target: "/project", MetadataOnly: true},
		},
	}

	args := s.buildArgs("ls", cfg)

	// /project/code should be bound read-only.
	foundBind := false
	for i, a := range args {
		if a == roBind && i+2 < len(args) && args[i+1] == "/project/code" {
			foundBind = true
		}
	}
	assert.True(t, foundBind, "expected --ro-bind for /project/code")

	// MetadataOnly mount /project must NOT appear in args — seatbelt concept only.
	for _, a := range args {
		assert.NotEqual(t, "/project", a, "MetadataOnly mount /project should not appear in bwrap args")
	}
}
