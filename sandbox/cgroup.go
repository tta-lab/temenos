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
	"sync/atomic"
	"syscall"

	"github.com/containerd/cgroups/v3/cgroup2"
	"golang.org/x/sys/unix"
)

// Call graph:
//
// daemon.Run → SetupCgroupV2 → setupInitLeaf → runInitLeaf  (one-time)
//
// per-exec:
//   BwrapSandbox.Exec → newCgroupExec → execWithCgroup → cmd.Start → defer cleanup

const (
	cgroupRoot   = "/sys/fs/cgroup"
	cgroupPrefix = "temenos-exec-"
)

var (
	cgroupOnce      sync.Once
	cgroupReady     atomic.Bool
	cgroupAvailBool bool
	cgroupReason    error // nil iff cgroupAvailBool == true; else one of the sentinels below.
	discoveredPath  string
)

// Sentinel reasons for why cgroup v2 with memory delegation is unavailable.
// Returned by cgroupV2Reason() and used by SetupCgroupV2 to emit actionable
// diagnostics. errors.Is checks let callers (e.g. SetupCgroupV2 wrapping the
// runtimeClassName hint) branch without string sniffing.
var (
	ErrCgroupNotMounted          = errors.New("cgroup v2 root not mounted (cgroup.controllers missing)")
	ErrCgroupNoDelegatedPath     = errors.New("cannot discover delegated cgroup path from /proc/self/cgroup")
	ErrCgroupMemoryControllerOff = errors.New("memory controller not enabled on delegated cgroup (cgroup.subtree_control missing +memory)")
	ErrCgroupNotWritable         = errors.New("delegated cgroup path exists but is not writable by this process")
)

// cgroupExec manages a per-execution cgroup v2 sub-group with memory limits.
type cgroupExec struct {
	mgr  *cgroup2.Manager
	path string // e.g. /sys/fs/cgroup/user.slice/.../temenos-exec-a1b2c3d4
	fd   int    // cgroup dir FD for UseCgroupFD; -1 if not set
}

// newCgroupExec creates a cgroup sub-directory and sets memory.max + memory.swap.max.
func newCgroupExec(memoryMB int) (*cgroupExec, error) {
	// Defense-in-depth: refuse to create per-exec cgroups before init-leaf ran.
	if !cgroupReady.Load() {
		return nil, errors.New("cgroup: cgroup v2 not initialized (SetupCgroupV2 must succeed first)")
	}

	id, err := shortID()
	if err != nil {
		return nil, fmt.Errorf("cgroup: generate id: %w", err)
	}

	// Use execCgroupBase if set (post-init-leaf), otherwise fall back to discovery.
	basePath := execCgroupBase
	if basePath == "" {
		var ok bool
		basePath, ok = discoverDelegatedPath("/proc/self/cgroup")
		if !ok {
			return nil, errors.New(
				"cgroup: cannot discover delegated path from /proc/self/cgroup",
			)
		}
	}

	cgroupPath := filepath.Join(basePath, cgroupPrefix+id)

	// Strip cgroupRoot to get the relative path. cgroup2.NewManager joins
	// cgroupRoot + group internally and rejects absolute paths in group.
	relativePath, ok := strings.CutPrefix(cgroupPath, cgroupRoot)
	if !ok || relativePath == "" {
		return nil, fmt.Errorf("cgroup: path %q is not under cgroupRoot %q", cgroupPath, cgroupRoot)
	}

	memLimit := int64(memoryMB) * 1024 * 1024
	mgr, err := cgroup2.NewManager(cgroupRoot, relativePath, &cgroup2.Resources{
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
		syscall.Close(c.fd)
		c.fd = -1
	}
	if err := c.mgr.Delete(); err != nil {
		slog.Warn("sandbox: cgroup delete failed", "path", c.path, "err", err)
	}
}

// cgroupAvailable returns true if cgroup v2 with memory delegation is fully
// usable: mounted, delegated path discoverable, memory controller present,
// and the delegated path is writable.
func cgroupAvailable() bool {
	cgroupOnce.Do(initCgroupStatus)
	return cgroupAvailBool
}

// cgroupV2Reason returns nil when cgroupAvailable() is true. Otherwise it
// returns one of the Err* sentinels above describing exactly why v2 isn't
// usable. The result is cached alongside the bool — both share cgroupOnce.
func cgroupV2Reason() error {
	cgroupOnce.Do(initCgroupStatus)
	return cgroupReason
}

func initCgroupStatus() {
	cgroupAvailBool, cgroupReason = checkCgroupAvailableAt(cgroupRoot)
}

func checkCgroupAvailable() bool {
	ok, _ := checkCgroupAvailableAt(cgroupRoot)
	return ok
}

// checkCgroupAvailableAt is the injectable variant for testing.
// Returns (true, nil) when v2 is mounted, the delegated path is discoverable,
// the memory controller is delegated, and the path is W_OK-writable.
// Otherwise returns (false, sentinel) with one of the Err* sentinels.
func checkCgroupAvailableAt(root string) (bool, error) {
	return checkCgroupAvailableWith(root, "/proc/self/cgroup")
}

// checkCgroupAvailableWith is the fully-injectable variant for tests; root is
// the cgroup v2 root and procFile is the path to the membership file (normally
// /proc/self/cgroup).
func checkCgroupAvailableWith(root, procFile string) (bool, error) {
	controllersFile := filepath.Join(root, "cgroup.controllers")
	if _, err := os.Stat(controllersFile); err != nil {
		return false, ErrCgroupNotMounted
	}
	delegated, ok := discoverDelegatedPathAt(procFile, root)
	if !ok {
		return false, ErrCgroupNoDelegatedPath
	}
	discoveredPath = delegated
	if !hasController(delegated, "memory") {
		return false, ErrCgroupMemoryControllerOff
	}
	if unix.Access(delegated, unix.W_OK) != nil {
		return false, ErrCgroupNotWritable
	}
	return true, nil
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
