package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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
	Env       []string // Extra env vars passed to the sandboxed process
	MountDirs []Mount  // Additional read-only bind mounts
}

// Mount represents a filesystem mount inside the sandbox.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// runCmd executes a prepared command and returns output, exit code, and errors.
// It distinguishes between context cancellation (timeout) and other exec errors.
func runCmd(ctx context.Context, cmd *exec.Cmd) (stdout, stderr string, exitCode int, err error) {
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()

	stdoutStr := truncate(stdoutBuf.String(), maxOutputBytes)
	stderrStr := truncate(stderrBuf.String(), maxOutputBytes)

	if ctx.Err() != nil {
		return stdoutStr, stderrStr, -1, ctx.Err()
	}

	// Distinguish successful exit (including non-zero) from exec infrastructure failure.
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return stdoutStr, stderrStr, exitErr.ExitCode(), nil
	}
	if runErr != nil {
		return stdoutStr, stderrStr, -1, fmt.Errorf("exec failed: %w", runErr)
	}

	return stdoutStr, stderrStr, 0, nil
}

// buildEnv constructs the environment for a sandboxed process.
// homeDir sets HOME; if empty, defaults to "/home/agent".
func buildEnv(cfg *ExecConfig, homeDir string) []string {
	home := cmp.Or(homeDir, "/home/agent")
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		if h := os.Getenv("HOME"); h != "" {
			gopath = filepath.Join(h, "go")
		} else {
			slog.Warn("sandbox: GOPATH and HOME both unset, logos binary may not be on PATH")
		}
	}

	path := "/usr/bin:/usr/local/bin:/bin"
	if gopath != "" {
		path += ":" + gopath + "/bin"
	}
	base := []string{
		"PATH=" + path,
		"HOME=" + home,
		"TERM=dumb",
	}
	if cfg != nil {
		base = append(base, cfg.Env...)
	}
	return base
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
