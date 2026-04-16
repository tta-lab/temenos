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
	"strings"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/containerd/cgroups/v3/cgroup2"
)

const (
	cgroupRoot   = "/sys/fs/cgroup"
	cgroupPrefix = "temenos-exec-"
)

var (
	cgroupOnce      sync.Once
	cgroupAvailBool bool
	discoveredPath  string
)

// cgroupExec manages a per-execution cgroup v2 sub-group with memory limits.
// Wraps cgroup2.Manager.
type cgroupExec struct {
	mgr  *cgroup2.Manager
	path string // e.g. /sys/fs/cgroup/user.slice/.../temenos-exec-a1b2c3d4
	fd   int    // cgroup dir FD for UseCgroupFD; -1 if not set
}

// newCgroupExec creates a cgroup sub-directory and sets memory.max + memory.swap.max.
// Returns an error if cgroup creation or configuration fails.
func newCgroupExec(memoryMB int) (*cgroupExec, error) {
	id, err := shortID()
	if err != nil {
		return nil, fmt.Errorf("cgroup: generate id: %w", err)
	}

	delegatedPath, ok := discoverDelegatedPath("/proc/self/cgroup")
	if !ok {
		return nil, errors.New(
			"cgroup: cannot discover delegated path from /proc/self/cgroup",
		)
	}

	cgroupPath := filepath.Join(delegatedPath, cgroupPrefix+id)

	memLimit := int64(memoryMB) * 1024 * 1024
	mgr, err := cgroup2.NewManager(cgroupRoot, cgroupPath, &cgroup2.Resources{
		Memory: &cgroup2.Memory{
			Max:  &memLimit,
			Swap: ptr(int64(0)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("cgroup: create manager: %w", err)
	}

	return &cgroupExec{mgr: mgr, path: cgroupPath, fd: -1}, nil
}

// cleanup kills remaining processes, closes the cgroup FD, and removes the cgroup directory.
func (c *cgroupExec) cleanup() {
	if err := c.mgr.Kill(); err != nil {
		slog.Warn("sandbox: cgroup kill failed", "path", c.path, "err", err)
	}
	if c.fd != -1 {
		_ = unix.Close(c.fd)
		c.fd = -1
	}
	if err := c.mgr.Delete(); err != nil {
		slog.Warn("sandbox: cgroup delete failed", "path", c.path, "err", err)
	}
}

// cgroupAvailable returns true if cgroup v2 is mounted, the delegated path is
// discoverable, and 'memory' is in the delegated cgroup's controllers.
// Result is cached after the first call.
func cgroupAvailable() bool {
	cgroupOnce.Do(func() {
		cgroupAvailBool = checkCgroupAvailable()
	})
	return cgroupAvailBool
}

func checkCgroupAvailable() bool {
	controllersFile := filepath.Join(cgroupRoot, "cgroup.controllers")
	if _, err := os.Stat(controllersFile); err != nil {
		return false
	}

	delegated, ok := discoverDelegatedPath("/proc/self/cgroup")
	if !ok {
		return false
	}
	discoveredPath = delegated

	return hasController(delegated, "memory")
}

// hasController returns true if controller (e.g. "memory") is listed in
// the cgroup.controllers file of path.
func hasController(path, controller string) bool {
	data, err := os.ReadFile(filepath.Join(path, "cgroup.controllers"))
	return err == nil && strings.Contains(string(data), controller)
}

func shortID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ptr[T any](v T) *T { return &v }
