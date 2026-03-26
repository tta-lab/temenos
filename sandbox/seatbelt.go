package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"
)

// SeatbeltSandbox executes commands using macOS seatbelt (sandbox-exec).
// Used on macOS for kernel-level sandboxing with deny-default filesystem policy.
type SeatbeltSandbox struct {
	Timeout time.Duration
}

// Exec runs a bash command inside the seatbelt sandbox.
func (s *SeatbeltSandbox) Exec(
	ctx context.Context, command string, cfg *ExecConfig,
) (stdout, stderr string, exitCode int, err error) {
	timeout := effectiveTimeout(s.Timeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create a temp dir for fallback working directory — always clean up.
	// This is NOT used as HOME; HOME comes from cfg.Env (forwarded by caller)
	// or falls back to a default. The sandbox's seatbelt policy controls
	// filesystem access, not HOME.
	tmpDir, err := os.MkdirTemp("/tmp", "ttal-agent-")
	if err != nil {
		return "", "", -1, fmt.Errorf("create temp workdir: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			slog.Warn("seatbelt: failed to remove temp dir", "path", tmpDir, "err", rmErr)
		}
	}()

	policy, dParams, err := buildPolicy(cfg)
	if err != nil {
		return "", "", -1, fmt.Errorf("build seatbelt policy: %w", err)
	}

	args := make([]string, 0, 2+len(dParams)+3)
	args = append(args, "-p", policy)
	args = append(args, dParams...)
	args = append(args, "--", "bash", "-c", command)

	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", args...)
	cmd.Env = buildEnv(cfg, tmpDir)
	// Use WorkingDir if provided (within allowed paths), otherwise
	// fall back to temp dir to avoid getcwd errors.
	if cfg != nil && cfg.WorkingDir != "" {
		cmd.Dir = cfg.WorkingDir
	} else {
		cmd.Dir = tmpDir
	}

	return runCmd(ctx, cmd)
}

// IsAvailable checks whether sandbox-exec is present on the system.
func (s *SeatbeltSandbox) IsAvailable() bool {
	_, err := os.Stat("/usr/bin/sandbox-exec")
	if err == nil {
		return true
	}
	if !os.IsNotExist(err) {
		slog.Warn("seatbelt: unexpected error checking sandbox-exec", "err", err)
	}
	return false
}
