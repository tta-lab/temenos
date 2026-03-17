package sandbox

import (
	"testing"

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

func TestAppendBwrapToolBinds_SkipsStaticRoots(t *testing.T) {
	// /usr/local is a subdir of /usr (static root) — should be skipped.
	args := appendBwrapToolBinds(nil)
	for i, a := range args {
		if a == roBind && i+1 < len(args) {
			path := args[i+1]
			assert.False(t, coveredByStaticRoot(path),
				"appendBwrapToolBinds should not bind %s (covered by static root)", path)
		}
	}
}

func TestAppendBwrapToolBinds_NoDuplicates(t *testing.T) {
	args := appendBwrapToolBinds(nil)
	seen := make(map[string]bool)
	for i, a := range args {
		if a == roBind && i+1 < len(args) {
			path := args[i+1]
			assert.False(t, seen[path], "duplicate --ro-bind for %s", path)
			seen[path] = true
		}
	}
}
