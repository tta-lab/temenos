package sandbox

import (
	"context"
	"errors"
	"os/exec"
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
	case darwinOS:
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
	cfg := &ExecConfig{
		Env: []string{"FOO=bar", "BAZ=qux"},
	}

	env := buildEnv(cfg, "")

	pathEntry := findPathEntry(env)
	assert.Contains(t, pathEntry, "/usr/bin")
	assert.Contains(t, pathEntry, "/bin")
	assert.Contains(t, env, "HOME=/home/agent")
	assert.Contains(t, env, "FOO=bar")
	assert.Contains(t, env, "BAZ=qux")
}

func TestBuildEnv_Nil(t *testing.T) {
	env := buildEnv(nil, "")

	pathEntry := findPathEntry(env)
	assert.Contains(t, pathEntry, "/usr/bin")
	assert.Contains(t, pathEntry, "/bin")
	assert.Len(t, env, 3) // PATH, HOME, TERM
}

func TestBuildEnv_WithHomeDir(t *testing.T) {
	env := buildEnv(nil, "/tmp/ttal-agent-12345")
	assert.Contains(t, env, "HOME=/tmp/ttal-agent-12345")
}

func TestRunCmdWithHook_HookReceivesValidPID(t *testing.T) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "echo", "hello")

	var capturedPID int
	stdout, _, exitCode, err := runCmdWithHook(ctx, cmd, func(pid int) error {
		capturedPID = pid
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "hello")
	assert.Greater(t, capturedPID, 0, "hook should receive a positive PID")
}

func TestRunCmdWithHook_HookErrorAbortsExecution(t *testing.T) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "echo", "hello")

	hookErr := errors.New("hook injection failed")
	_, _, exitCode, err := runCmdWithHook(ctx, cmd, func(pid int) error {
		return hookErr
	})

	require.ErrorIs(t, err, hookErr)
	assert.Equal(t, -1, exitCode)
}

func TestTruncate(t *testing.T) {
	s := "hello"
	assert.Equal(t, "hello", truncate(s, 10))

	long := "12345678901234567890"
	result := truncate(long, 10)
	assert.Equal(t, "1234567890\n[output truncated]", result)
}
