package sandbox

import (
	"context"
	"os/exec"
	"time"
)

// NoopSandbox runs commands directly without any sandbox isolation.
// Intended for local development when no platform sandbox is available
// and AllowUnsandboxed is true.
type NoopSandbox struct {
	Timeout time.Duration
}

// Exec runs a bash command directly on the host, without sandboxing.
func (n *NoopSandbox) Exec(
	ctx context.Context, command string, cfg *ExecConfig,
) (stdout, stderr string, exitCode int, err error) {
	timeout := effectiveTimeout(n.Timeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Env = buildEnv(cfg, "")

	return runCmd(ctx, cmd)
}

// IsAvailable always returns true — no external binary required.
func (n *NoopSandbox) IsAvailable() bool { return true }
