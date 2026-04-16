//go:build linux

package sandbox

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/containerd/cgroups/v3/cgroup2"
)

const (
	cgroupRoot   = "/sys/fs/cgroup"
	cgroupPrefix = "temenos-exec-"
)

var (
	cgroupOnce      sync.Once
	cgroupAvailBool bool
	// discoveredPath holds the delegated cgroup path discovered at availability check time.
	discoveredPath string
)

// cgroupExec manages a per-execution cgroup v2 sub-group with memory limits.
// Wraps cgroup2.Manager.
type cgroupExec struct {
	mgr  *cgroup2.Manager
	path string // e.g. /sys/fs/cgroup/user.slice/.../temenos-exec-a1b2c3d4
}

// newCgroupExec creates a cgroup sub-directory and sets memory.max + memory.swap.max.
// Returns an error if cgroup creation or configuration fails.
func newCgroupExec(memoryMB int) (*cgroupExec, error) {
	id, err := shortID()
	if err != nil {
		return nil, fmt.Errorf("cgroup: generate id: %w", err)
	}

	// Discover the delegated path — required for cgroup v2 delegation to work.
	// This is cached by cgroupAvailable() so subsequent calls are free.
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

	return &cgroupExec{mgr: mgr, path: cgroupPath}, nil
}

// addPID adds a process to this cgroup.

// cleanup kills remaining processes and removes the cgroup directory.
func (c *cgroupExec) cleanup() {
	// Kill sends SIGKILL to all processes in the cgroup.
	// Returns nil if empty, error if processes were killed.
	_ = c.mgr.Kill()

	// Delete the cgroup directory.
	_ = c.mgr.Delete()
}

// cgroupAvailable returns true if:
//   - cgroup v2 is mounted (cgroup.controllers exists under cgroupRoot)
//   - the delegated path can be discovered from /proc/self/cgroup
//   - 'memory' is listed in the delegated path's cgroup.controllers
//
// Result is cached after the first call.
func cgroupAvailable() bool {
	cgroupOnce.Do(func() {
		cgroupAvailBool = checkCgroupAvailable()
	})
	return cgroupAvailBool
}

func checkCgroupAvailable() bool {
	// Step 1: cgroup v2 mounted?
	controllersFile := filepath.Join(cgroupRoot, "cgroup.controllers")
	if _, err := os.Stat(controllersFile); err != nil {
		return false
	}

	// Step 2: discover delegated path from /proc/self/cgroup.
	delegated, ok := discoverDelegatedPath("/proc/self/cgroup")
	if !ok {
		return false
	}
	discoveredPath = delegated

	// Step 3: memory controller available in the delegated cgroup?
	ctrlFile := filepath.Join(delegated, "cgroup.controllers")
	data, err := os.ReadFile(ctrlFile)
	if err != nil {
		return false
	}
	if !strings.Contains(string(data), "memory") {
		return false
	}

	return true
}

func shortID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ptr[T any](v T) *T { return &v }
