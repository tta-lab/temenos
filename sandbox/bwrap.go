package sandbox

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// BwrapSandbox executes commands using bubblewrap (bwrap) namespace isolation.
// Used on Linux.
type BwrapSandbox struct {
	BwrapPath string
	Timeout   time.Duration
}

// Exec runs a bash command inside the bubblewrap sandbox.
func (s *BwrapSandbox) Exec(
	ctx context.Context, command string, cfg *ExecConfig,
) (stdout, stderr string, exitCode int, err error) {
	timeout := effectiveTimeout(s.Timeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := s.buildArgs(command, cfg)
	cmd := exec.CommandContext(ctx, s.BwrapPath, args...)
	cmd.Env = buildEnv(cfg, "/home/agent")

	return runCmd(ctx, cmd)
}

// IsAvailable checks whether bwrap is available at the configured path.
func (s *BwrapSandbox) IsAvailable() bool {
	_, err := exec.LookPath(s.BwrapPath)
	return err == nil
}

func (s *BwrapSandbox) buildArgs(command string, cfg *ExecConfig) []string {
	args := []string{
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/bin", "/bin",
		"--tmpfs", "/tmp",
		"--tmpfs", "/home/agent",
		"--unshare-all",
		"--share-net",
		"--dev", "/dev",
		"--ro-bind", "/etc/resolv.conf", "/etc/resolv.conf",
		"--ro-bind", "/etc/ssl/certs", "/etc/ssl/certs",
		"--ro-bind", "/etc/hosts", "/etc/hosts",
		"--die-with-parent",
	}

	if runtime.GOOS == "linux" {
		args = append(args,
			"--ro-bind", "/lib", "/lib",
			"--symlink", "/usr/lib64", "/lib64",
		)
	}

	if cfg != nil {
		for _, m := range cfg.MountDirs {
			if m.ReadOnly {
				args = append(args, "--ro-bind", m.Source, m.Target)
			} else {
				args = append(args, "--bind", m.Source, m.Target)
			}
		}
	}

	args = append(args, "--", "bash", "-c", command)
	return args
}
