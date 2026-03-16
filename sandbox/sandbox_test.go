package sandbox

import (
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pathPrefix = "PATH="

func findPathEntry(env []string) string {
	for _, e := range env {
		if len(e) >= len(pathPrefix) && e[:len(pathPrefix)] == pathPrefix {
			return e
		}
	}
	return ""
}

func TestNew_AllowUnsandboxed_IsAvailable(t *testing.T) {
	sbx := New(Options{AllowUnsandboxed: true})
	require.NotNil(t, sbx)
	assert.True(t, sbx.IsAvailable())
}

func TestNew_ReturnsCorrectType(t *testing.T) {
	sbx := New(Options{AllowUnsandboxed: true})
	switch runtime.GOOS {
	case "darwin":
		// sandbox-exec is always present on macOS — should be SeatbeltSandbox
		assert.IsType(t, &SeatbeltSandbox{}, sbx)
	default:
		// bwrap may or may not be installed; with AllowUnsandboxed either type is fine
		_, isBwrap := sbx.(*BwrapSandbox)
		_, isNoop := sbx.(*NoopSandbox)
		assert.True(t, isBwrap || isNoop, "expected BwrapSandbox or NoopSandbox on Linux")
	}
}

func TestNoopSandbox_Exec(t *testing.T) {
	n := &NoopSandbox{}
	stdout, stderr, code, err := n.Exec(t.Context(), "echo hello", nil)
	require.NoError(t, err)
	assert.Equal(t, "hello\n", stdout)
	assert.Empty(t, stderr)
	assert.Equal(t, 0, code)
}

func TestUnavailableSandbox_AlwaysErrors(t *testing.T) {
	u := &UnavailableSandbox{Platform: "testplatform"}
	_, _, _, err := u.Exec(t.Context(), "echo hello", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "testplatform")
	assert.Contains(t, err.Error(), "no sandbox available")
}

func TestUnavailableSandbox_IsAvailable(t *testing.T) {
	u := &UnavailableSandbox{Platform: "linux"}
	assert.False(t, u.IsAvailable())
}

func TestBuildEnv(t *testing.T) {
	t.Setenv("GOPATH", "/test/gopath")

	cfg := &ExecConfig{
		Env: []string{"FOO=bar", "BAZ=qux"},
	}

	env := buildEnv(cfg, "")

	// PATH should include GOPATH/bin
	pathEntry := findPathEntry(env)
	assert.Contains(t, pathEntry, "/usr/bin:/usr/local/bin:/bin:")
	assert.Contains(t, pathEntry, "/test/gopath/bin") // pinned GOPATH/bin
	assert.Contains(t, env, "HOME=/home/agent")
	assert.Contains(t, env, "FOO=bar")
	assert.Contains(t, env, "BAZ=qux")
}

func TestBuildEnv_Nil(t *testing.T) {
	env := buildEnv(nil, "")

	pathEntry := findPathEntry(env)
	assert.Contains(t, pathEntry, "/usr/bin:/usr/local/bin:/bin")
	assert.Len(t, env, 3) // PATH, HOME, TERM
}

func TestBuildEnv_WithHomeDir(t *testing.T) {
	env := buildEnv(nil, "/tmp/ttal-agent-12345")
	assert.Contains(t, env, "HOME=/tmp/ttal-agent-12345")
}

func TestBuildEnv_GOPATHAndHOMEUnset_UsesUserHomeDir(t *testing.T) {
	t.Setenv("GOPATH", "")
	t.Setenv("HOME", "")

	// os.UserHomeDir() falls back to a syscall (getpwuid_r) when HOME is unset.
	// Skip if that syscall is unavailable (e.g. CGO disabled in this environment).
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("os.UserHomeDir() unavailable in this environment (%v) — skipping fallback test", err)
	}

	env := buildEnv(nil, "")

	pathEntry := findPathEntry(env)
	assert.Contains(t, pathEntry, userHome+"/go/bin")
}

func TestTruncate(t *testing.T) {
	s := "hello"
	assert.Equal(t, "hello", truncate(s, 10))

	long := "12345678901234567890"
	result := truncate(long, 10)
	assert.Equal(t, "1234567890\n[output truncated]", result)
}
