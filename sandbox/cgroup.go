//go:build linux

package sandbox

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	cgroupRoot   = "/sys/fs/cgroup"
	cgroupPrefix = "temenos-exec-"
)

var (
	cgroupOnce      sync.Once
	cgroupAvailBool bool
)

// cgroupExec manages a per-execution cgroup v2 sub-group with memory limits.
type cgroupExec struct {
	path string // e.g. /sys/fs/cgroup/temenos-exec-a1b2c3d4
}

// newCgroupExec creates a cgroup sub-directory and sets memory.max + memory.swap.max.
// Returns an error if cgroup creation or configuration fails.
func newCgroupExec(memoryMB int) (*cgroupExec, error) {
	id, err := shortID()
	if err != nil {
		return nil, fmt.Errorf("cgroup: generate id: %w", err)
	}
	path := filepath.Join(cgroupRoot, cgroupPrefix+id)

	if err := os.Mkdir(path, 0o700); err != nil {
		return nil, fmt.Errorf("cgroup: mkdir %s: %w", path, err)
	}

	cg := &cgroupExec{path: path}

	// Enable memory controller in parent subtree (idempotent, may fail if already set).
	if err := os.WriteFile(
		filepath.Join(cgroupRoot, "cgroup.subtree_control"), []byte("+memory"), 0o644,
	); err != nil {
		slog.Warn("sandbox: cgroup subtree_control write failed", "err", err)
	}

	memBytes := int64(memoryMB) * 1024 * 1024

	if err := os.WriteFile(filepath.Join(path, "memory.max"), []byte(strconv.FormatInt(memBytes, 10)), 0o644); err != nil {
		cg.cleanup()
		return nil, fmt.Errorf("cgroup: set memory.max: %w", err)
	}

	if err := os.WriteFile(filepath.Join(path, "memory.swap.max"), []byte("0"), 0o644); err != nil {
		cg.cleanup()
		return nil, fmt.Errorf("cgroup: set memory.swap.max: %w", err)
	}

	return cg, nil
}

// addPID writes a process ID to cgroup.procs, moving the process into this cgroup.
func (c *cgroupExec) addPID(pid int) error {
	return os.WriteFile(filepath.Join(c.path, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0o644)
}

// cleanup kills any remaining processes and removes the cgroup directory.
// Safe to call multiple times. Logs warnings on non-critical failures.
func (c *cgroupExec) cleanup() {
	// Atomically signal all tasks to exit via cgroup.kill (Linux 5.14+).
	// Silently ignored on older kernels — the per-PID kill below covers them.
	_ = os.WriteFile(filepath.Join(c.path, "cgroup.kill"), []byte("1"), 0o644)

	// Per-PID SIGKILL for older kernels or any orphaned processes.
	c.killProcs()

	// Poll cgroup.procs until empty before removing the directory.
	// SIGKILL is async — the kernel must finish reaping before the dir can be removed.
	// 10 * 5ms = 50ms max wait.
	for i := 0; i < 10; i++ {
		data, err := os.ReadFile(filepath.Join(c.path, "cgroup.procs"))
		if err != nil || strings.TrimSpace(string(data)) == "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := os.Remove(c.path); err != nil && !os.IsNotExist(err) {
		slog.Warn("sandbox: cgroup cleanup failed", "path", c.path, "err", err)
	}
}

// killProcs sends SIGKILL to each PID listed in cgroup.procs.
// ESRCH (process already gone) is silently ignored; EPERM is logged.
func (c *cgroupExec) killProcs() {
	data, err := os.ReadFile(filepath.Join(c.path, "cgroup.procs"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid <= 1 {
			continue // skip invalid, PID 0, and PID 1 (init)
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			slog.Warn("sandbox: failed to kill cgroup process", "pid", pid, "err", err)
		}
	}
}

// cgroupAvailable returns true if cgroup v2 is mounted and the current process
// has write access to create sub-directories (requires root or SYS_ADMIN cap).
// Result is cached after the first call.
func cgroupAvailable() bool {
	cgroupOnce.Do(func() {
		if _, err := os.Stat(filepath.Join(cgroupRoot, "cgroup.controllers")); err != nil {
			return
		}
		// Verify write access — creating sub-directories requires root or SYS_ADMIN.
		cgroupAvailBool = syscall.Access(cgroupRoot, syscall.W_OK) == nil
	})
	return cgroupAvailBool
}

func shortID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
