//go:build linux

package sandbox

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	cgroupRoot   = "/sys/fs/cgroup"
	cgroupPrefix = "temenos-exec-"
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
	_ = os.WriteFile(filepath.Join(cgroupRoot, "cgroup.subtree_control"), []byte("+memory"), 0o644)

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
	procsPath := filepath.Join(c.path, "cgroup.procs")
	data, err := os.ReadFile(procsPath)
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || pid <= 1 {
				continue // skip invalid, PID 0, and PID 1 (init)
			}
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}

	if err := os.Remove(c.path); err != nil && !os.IsNotExist(err) {
		slog.Warn("sandbox: cgroup cleanup failed", "path", c.path, "err", err)
	}
}

// cgroupAvailable returns true if cgroup v2 is mounted and readable.
func cgroupAvailable() bool {
	_, err := os.Stat(filepath.Join(cgroupRoot, "cgroup.controllers"))
	return err == nil
}

func shortID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
