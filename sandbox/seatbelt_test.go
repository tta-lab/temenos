package sandbox

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireSandboxExec skips the test if sandbox-exec is not available.
func requireSandboxExec(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available, skipping seatbelt integration test")
	}
}

func TestSeatbeltSandbox_IsAvailable(t *testing.T) {
	s := &SeatbeltSandbox{}
	// IsAvailable() just checks whether the binary exists — no skip needed.
	_, err := os.Stat("/usr/bin/sandbox-exec")
	expected := err == nil
	assert.Equal(t, expected, s.IsAvailable())
}

func TestSeatbeltSandbox_EchoHello(t *testing.T) {
	requireSandboxExec(t)

	s := &SeatbeltSandbox{Timeout: 10 * time.Second}
	stdout, stderr, code, err := s.Exec(t.Context(), "echo hello", nil)

	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "hello\n", stdout)
	assert.Empty(t, stderr)
}

func TestSeatbeltSandbox_DeniesFileRead(t *testing.T) {
	requireSandboxExec(t)

	// Create a sentinel file in the user's home dir — not in sandbox allowed paths.
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	sentinel := filepath.Join(homeDir, ".ttal_sandbox_test_sentinel")
	require.NoError(t, os.WriteFile(sentinel, []byte("secret"), 0600))
	t.Cleanup(func() { _ = os.Remove(sentinel) })

	s := &SeatbeltSandbox{Timeout: 10 * time.Second}
	_, _, code, err := s.Exec(t.Context(), "cat "+sentinel, nil)

	require.NoError(t, err) // no exec infrastructure error
	assert.NotEqual(t, 0, code, "sandbox should deny access to user home dir")
}

func TestSeatbeltSandbox_AllowsMountRead(t *testing.T) {
	requireSandboxExec(t)

	// Create a test directory in user home (not in default allowed paths).
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	testDir := filepath.Join(homeDir, ".ttal_sandbox_test_mount")
	require.NoError(t, os.MkdirAll(testDir, 0755))
	t.Cleanup(func() { _ = os.RemoveAll(testDir) })

	testFile := filepath.Join(testDir, "hello.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("hello"), 0644))

	s := &SeatbeltSandbox{Timeout: 10 * time.Second}
	cfg := &ExecConfig{
		MountDirs: []Mount{{Source: testDir, Target: testDir, ReadOnly: true}},
	}
	stdout, _, code, err := s.Exec(t.Context(), "cat "+testFile, cfg)

	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "hello", stdout)
}

func TestSeatbeltSandbox_AllowsMountWrite(t *testing.T) {
	requireSandboxExec(t)

	// Create a test directory in user home (not in default allowed paths).
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	testDir := filepath.Join(homeDir, ".ttal_sandbox_test_mount_write")
	require.NoError(t, os.MkdirAll(testDir, 0755))
	t.Cleanup(func() { _ = os.RemoveAll(testDir) })

	outFile := filepath.Join(testDir, "output.txt")

	s := &SeatbeltSandbox{Timeout: 10 * time.Second}
	cfg := &ExecConfig{
		MountDirs: []Mount{{Source: testDir, Target: testDir, ReadOnly: false}},
	}
	_, _, code, err := s.Exec(t.Context(), "echo written > "+outFile, cfg)

	require.NoError(t, err)
	assert.Equal(t, 0, code)

	content, readErr := os.ReadFile(outFile)
	require.NoError(t, readErr, "output file should exist after sandbox write")
	assert.Equal(t, "written\n", string(content))
}

func TestSeatbeltSandbox_NetworkWorks(t *testing.T) {
	requireSandboxExec(t)
	if os.Getenv("CI") != "" {
		t.Skip("skipping network test in CI (may be airgapped)")
	}

	s := &SeatbeltSandbox{Timeout: 15 * time.Second}
	// Use curl to check network reachability (DNS + TCP).
	curlCmd := "curl -s -o /dev/null -w '%{http_code}' --max-time 5 https://example.com"
	stdout, stderr, code, err := s.Exec(t.Context(), curlCmd, nil)

	require.NoError(t, err)
	// Accept any HTTP response — we just want network to work.
	t.Logf("curl exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	assert.Equal(t, 0, code, "curl should succeed with network access")
}

func TestSeatbeltSandbox_TempHomeCleanup(t *testing.T) {
	requireSandboxExec(t)

	// Verify that after Exec, no /tmp/ttal-agent-* dirs remain.
	// List before, run, list after.
	entries, err := filepath.Glob("/tmp/ttal-agent-*")
	require.NoError(t, err)
	before := len(entries)

	s := &SeatbeltSandbox{Timeout: 10 * time.Second}
	_, _, _, err = s.Exec(t.Context(), "echo done", nil)
	require.NoError(t, err)

	entries, err = filepath.Glob("/tmp/ttal-agent-*")
	require.NoError(t, err)
	assert.Equal(t, before, len(entries), "temp HOME dirs should be cleaned up after Exec")
}
