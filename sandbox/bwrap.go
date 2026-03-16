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

	// Mount discovered tool directories (GOPATH/bin, cargo, etc.)
	// as read-only inside the sandbox. See paths.go.
	args = appendBwrapToolBinds(args)

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

// appendBwrapToolBinds adds --ro-bind entries for each tool directory's
// ReadDirs that exist on disk and aren't already covered by the static
// bwrap mounts (/usr, /bin, /lib).
func appendBwrapToolBinds(args []string) []string {
	// These are already mounted by the static args above.
	static := map[string]bool{
		"/usr": true,
		"/bin": true,
		"/lib": true,
	}

	seen := make(map[string]bool)
	for _, td := range allToolDirs() {
		for _, rd := range td.ReadDirs {
			if static[rd] || seen[rd] {
				continue
			}
			// Skip paths that are subdirs of already-mounted trees.
			if isSubdirOf(rd, "/usr") || isSubdirOf(rd, "/bin") || isSubdirOf(rd, "/lib") {
				continue
			}
			seen[rd] = true
			args = append(args, "--ro-bind", rd, rd)
		}
	}
	return args
}

// isSubdirOf checks if child starts with parent + "/".
func isSubdirOf(child, parent string) bool {
	return len(child) > len(parent) && child[:len(parent)+1] == parent+"/"
}
