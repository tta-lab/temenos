package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"cmp"
)

const maxOutputBytes = 64 * 1024 // 64KB output truncation

// Sandbox executes commands in an isolated environment.
type Sandbox interface {
	Exec(ctx context.Context, command string, cfg *ExecConfig) (stdout, stderr string, exitCode int, err error)
	IsAvailable() bool
}

// Seconds returns a duration from a seconds count.
func Seconds(s int) time.Duration {
	return time.Duration(s) * time.Second
}

// ExecConfig holds per-execution sandbox settings.
type ExecConfig struct {
	Env        []string // Extra env vars passed to the sandboxed process
	MountDirs  []Mount  // Additional read-only bind mounts
	WorkingDir string   // If set, commands run in this directory; empty = sandbox default
}

// Mount represents a filesystem mount inside the sandbox.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
	// MetadataOnly indicates the mount should only grant file-read-metadata access.
	// Seatbelt emits (allow file-read-metadata (literal ...)) for these mounts.
	// Bwrap skips MetadataOnly mounts entirely — bwrap namespace isolation provides
	// implicit parent-directory visibility for bind-mounted paths.
	MetadataOnly bool
}

// runCmd executes a prepared command and returns output, exit code, and errors.
// It distinguishes between context cancellation (timeout) and other exec errors.
func runCmd(ctx context.Context, cmd *exec.Cmd) (stdout, stderr string, exitCode int, err error) {
	return runCmdWithHook(ctx, cmd, nil)
}

// runCmdWithHook executes a prepared command, calling postStart (if non-nil)
// after the process starts but before waiting for it to finish. This enables
// cgroup PID assignment between Start and Wait.
// If postStart returns a non-nil error the process is killed and that error is returned.
func runCmdWithHook(
	ctx context.Context, cmd *exec.Cmd, postStart func(pid int) error,
) (stdout, stderr string, exitCode int, err error) {
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return "", "", -1, fmt.Errorf("exec failed: %w", err)
	}

	if postStart != nil {
		if hookErr := postStart(cmd.Process.Pid); hookErr != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return "", "", -1, hookErr
		}
	}

	waitErr := cmd.Wait()

	stdoutStr := truncate(stdoutBuf.String(), maxOutputBytes)
	stderrStr := truncate(stderrBuf.String(), maxOutputBytes)

	if ctx.Err() != nil {
		return stdoutStr, stderrStr, -1, ctx.Err()
	}

	// Distinguish successful exit (including non-zero) from exec infrastructure failure.
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return stdoutStr, stderrStr, exitErr.ExitCode(), nil
	}
	if waitErr != nil {
		return stdoutStr, stderrStr, -1, fmt.Errorf("exec failed: %w", waitErr)
	}

	return stdoutStr, stderrStr, 0, nil
}

// buildEnv constructs the environment for a sandboxed process.
// fallbackHome sets HOME when cfg.Env does not provide one; if empty, defaults to "/home/agent".
// If cfg.Env contains a HOME= entry, it takes precedence — allowing the caller (e.g. MCP server)
// to forward the real HOME so tools can find their config files naturally. The sandbox's
// filesystem policy (seatbelt/bwrap) is the security boundary, not HOME.
// PATH is built from buildSandboxPATH() which includes all discovered
// tool directories (see paths.go).
//
// Security note: when the real HOME is forwarded, tools like git/curl/ssh will resolve
// ~/.ssh/config, ~/.netrc, ~/.gitconfig — but only if $HOME is in AllowedPaths. The
// seatbelt/bwrap policy denies access otherwise. Callers should not add $HOME itself
// to AllowedPaths; mount specific subdirs (e.g. ~/.config/ttal) instead.
func buildEnv(cfg *ExecConfig, fallbackHome string) []string {
	base := []string{
		"PATH=" + buildSandboxPATH(),
		"TERM=dumb",
	}
	if cfg != nil {
		base = append(base, cfg.Env...)
	}
	// Only inject HOME if the caller's env doesn't already set it.
	if !envContainsKey(base, "HOME") {
		home := cmp.Or(fallbackHome, "/home/agent")
		// Build a new slice to avoid aliasing — append(base[:1], ...) would
		// overwrite base[1] (TERM) when len==cap (cfg==nil path).
		result := make([]string, 0, len(base)+1)
		result = append(result, base[0])
		result = append(result, "HOME="+home)
		result = append(result, base[1:]...)
		base = result
	}
	return base
}

// envContainsKey returns true if the env slice contains a KEY= entry for the given key.
func envContainsKey(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n[output truncated]"
}

// effectiveTimeout returns d if positive, otherwise the default 30s.
func effectiveTimeout(d time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return 30 * time.Second
}
