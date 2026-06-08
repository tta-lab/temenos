//go:build linux

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// BwrapSandbox executes commands using bubblewrap (bwrap) namespace isolation.
// Used on Linux.
// BwrapSandbox executes commands using bubblewrap (bwrap) namespace isolation.
// Used on Linux.
type BwrapSandbox struct {
	BwrapPath      string
	Timeout        time.Duration
	MemoryLimitMB  int  // 0 = no limit
	KubernetesMode bool // skips --proc /proc and --unshare-pid for k8s nested environments
}

const (
	bwrapNixStorePath = "/nix/store"
	roBind            = "--ro-bind"
	procArg           = "--proc"
	staticBin         = "/bin"
	staticProc        = "/proc"
	staticUsr         = "/usr"
)

var bwrapNixStoreStat = os.Stat

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

	// No memory limit configured — pure passthrough.
	if s.MemoryLimitMB <= 0 {
		return runCmd(ctx, cmd)
	}
	// Memory limit was requested but cgroup v2 isn't usable. Run unbounded but
	// surface the reason — silent fallback was an observability hole. Tag with
	// memory_limit_mb so ops can grep for "wanted limit, didn't get it" events.
	if !cgroupAvailable() {
		slog.Warn("sandbox: cgroup v2 not available — running without memory limit",
			"sandbox.memory_limit_mb", s.MemoryLimitMB,
			"reason", cgroupV2Reason())
		return runCmd(ctx, cmd)
	}

	cg, err := newCgroupExec(s.MemoryLimitMB)
	if err != nil {
		return "", "", -1, fmt.Errorf("sandbox: cgroup setup failed: %w", err)
	}
	defer cg.cleanup()

	// Set up cgroup FD for atomic child placement via clone3(CLONE_INTO_CGROUP).
	if err := execWithCgroup(cmd, cg); err != nil {
		return "", "", -1, err
	}

	return runCmd(ctx, cmd)
}

// IsAvailable checks whether bwrap is available at the configured path.
func (s *BwrapSandbox) IsAvailable() bool {
	_, err := exec.LookPath(s.BwrapPath)
	return err == nil
}

func (s *BwrapSandbox) buildArgs(command string, cfg *ExecConfig) []string {
	// bwrap flags reference:
	// https://manpages.debian.org/bookworm/bubblewrap/bwrap.1.en.html
	//
	// The static mounts below create the minimum runtime view:
	//   - --ro-bind /usr, /bin, /lib: expose trusted system tools and shared libs read-only.
	//   - --tmpfs /tmp and /home/agent: give tools writable scratch space without host writes.
	//   - --proc /proc: mount a fresh procfs so /proc/self/exe works without exposing host /proc.
	//     Skipped in Kubernetes mode (nested container): mounting procfs inside a new PID namespace
	//     requires CAP_SYS_ADMIN, which is not recommended for k8s workloads.
	//   - --unshare-all: isolate user, ipc, pid, network, uts, and cgroup namespaces where possible.
	//   - --share-net: intentionally keep host network access after --unshare-all.
	//   - --dev /dev: create a minimal device filesystem for stdio, null, random, etc.
	//   - --ro-bind resolv.conf, certs, hosts: make DNS and TLS work with shared networking.
	//   - --die-with-parent: kill sandboxed processes if the daemon-side bwrap parent dies.
	//   - --symlink /usr/lib64 /lib64: support distributions where /lib64 points into /usr.
	var args []string
	if s.KubernetesMode {
		// In nested Kubernetes, skip --proc /proc (needs CAP_SYS_ADMIN).
		// The pod's own /proc is already an unprivileged procfs.
		args = []string{
			roBind, staticUsr, staticUsr,
			roBind, staticBin, staticBin,
			"--tmpfs", "/tmp",
			"--tmpfs", "/home/agent",
			"--unshare-all",
		}
	} else {
		args = []string{
			roBind, staticUsr, staticUsr,
			roBind, staticBin, staticBin,
			"--tmpfs", "/tmp",
			"--tmpfs", "/home/agent",
			procArg, staticProc,
			"--unshare-all",
		}
	}
	args = append(args,
		"--share-net",
		"--dev", "/dev",
		roBind, "/etc/resolv.conf", "/etc/resolv.conf",
		roBind, "/etc/ssl/certs", "/etc/ssl/certs",
		roBind, "/etc/hosts", "/etc/hosts",
		"--die-with-parent",
	)

	if runtime.GOOS == "linux" {
		args = append(args,
			roBind, "/lib", "/lib",
			"--symlink", "/usr/lib64", "/lib64",
		)
	}

	// Mount discovered tool directories (GOPATH/bin, cargo, etc.)
	// as read-only inside the sandbox. See paths.go.
	args = appendBwrapToolBinds(args)
	args = appendNixStoreBind(args)

	if cfg != nil {
		for _, m := range cfg.MountDirs {
			if m.MetadataOnly {
				continue
			}
			if _, err := os.Stat(m.Source); err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					slog.Warn("sandbox: unexpected error checking mount source; skipped",
						"path", m.Source, "err", err)
				}
				continue
			}
			if m.ReadOnly {
				args = append(args, "--ro-bind", m.Source, m.Target)
			} else {
				args = append(args, "--bind", m.Source, m.Target)
			}
		}
	}

	if cfg != nil && cfg.WorkingDir != "" {
		args = append(args, "--chdir", cfg.WorkingDir)
	}

	args = append(args, "--", "bash", "-c", command)
	return args
}

// bwrapStaticRoots are the top-level directories already mounted by
// buildArgs. Tool ReadDirs under these trees are skipped.
var bwrapStaticRoots = []string{"/usr", "/bin", "/lib"}

// appendBwrapToolBinds adds --ro-bind entries for each tool directory's
// ReadDirs that exist on disk and aren't already covered by the static
// bwrap mounts (/usr, /bin, /lib).
func appendBwrapToolBinds(args []string) []string {
	seen := make(map[string]bool)
	for _, td := range allToolDirs() {
		for _, rd := range td.ReadDirs {
			if seen[rd] {
				continue
			}
			if coveredByStaticRoot(rd) {
				continue
			}
			// Validate the ReadDir exists — bwrap fails on missing bind sources.
			if _, err := os.Stat(rd); err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					slog.Warn("sandbox: unexpected error checking bwrap ReadDir; bind mount skipped",
						"path", rd, "err", err)
				}
				continue
			}
			seen[rd] = true
			args = append(args, "--ro-bind", rd, rd)
		}
	}
	return args
}

// appendNixStoreBind adds a read-only bind mount for /nix/store if it exists.
// NixOS profile entries are symlinks into the store, so both the profile and
// store need to be visible inside the sandbox.
func appendNixStoreBind(args []string) []string {
	if _, err := bwrapNixStoreStat(bwrapNixStorePath); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("sandbox: unexpected error checking nix store; skipped",
				"path", bwrapNixStorePath, "err", err)
		}
		return args
	}
	if isBound(args, bwrapNixStorePath) {
		return args
	}
	return append(args, "--ro-bind", bwrapNixStorePath, bwrapNixStorePath)
}

func isBound(args []string, source string) bool {
	for i := 0; i+1 < len(args); i++ {
		if (args[i] == "--ro-bind" || args[i] == "--bind") && args[i+1] == source {
			return true
		}
	}
	return false
}

// coveredByStaticRoot returns true if path is equal to or a subdir of
// any bwrap static root mount.
func coveredByStaticRoot(path string) bool {
	for _, root := range bwrapStaticRoots {
		if path == root || isSubdirOf(path, root) {
			return true
		}
	}
	return false
}
