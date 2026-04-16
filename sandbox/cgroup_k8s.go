//go:build linux

package sandbox

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// discoverDelegatedPath parses /proc/self/cgroup and returns the absolute path
// to the current cgroup if it's a cgroup v2 single-line entry. Returns ("", false)
// for v1, empty file, or malformed input.
func discoverDelegatedPath(procFile string) (string, bool) {
	f, err := os.Open(procFile)
	if err != nil {
		return "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return "", false
	}
	line := scanner.Text()

	// cgroup v2: single line "0::<path>"
	if !strings.HasPrefix(line, "0::") {
		return "", false
	}
	path := strings.TrimPrefix(line, "0::")
	if path == "" {
		return "", false
	}
	return filepath.Join(cgroupRoot, path), true
}

// initLeafOnce ensures init-leaf migration runs exactly once.
var initLeafOnce sync.Once

// initLeafErr holds the result of the one-time init-leaf setup.
var initLeafErr error

// initLeafSucceeded is true when setupInitLeaf was called and succeeded.
var initLeafSucceeded bool

// execCgroupBase is the path under which per-exec cgroups are created.
// Set by setupInitLeaf after migration (so it refers to selfCgroup, not selfCgroup/init).
var execCgroupBase string

// statCgroupControllers checks whether /sys/fs/cgroup/cgroup.controllers exists.
// Extracted as a package var so tests can swap the probe without touching the filesystem.
var statCgroupControllers = func() bool {
	_, err := os.Stat(filepath.Join(cgroupRoot, "cgroup.controllers"))
	return err == nil
}

// setupInitLeaf migrates the current process into a leaf sub-cgroup ("init/")
// so that we can enable +memory on our parent without violating the
// cgroup v2 "no internal processes" rule. Idempotent: subsequent calls
// return the cached result from the first call.
func setupInitLeaf() error {
	initLeafOnce.Do(func() {
		initLeafErr = runInitLeaf()
		initLeafSucceeded = initLeafErr == nil
	})
	return initLeafErr
}

// runInitLeaf is the zero-arg wrapper. Tests should call runInitLeafAt for control.
func runInitLeaf() error {
	return runInitLeafAt(cgroupRoot, "/proc/self/cgroup")
}

// runInitLeafAt performs the init-leaf migration under the given cgroup root.
// root must be the absolute path to the cgroup v2 root (e.g. "/sys/fs/cgroup").
// procFile is the path to the cgroup membership file (normally "/proc/self/cgroup").
func runInitLeafAt(root, procFile string) error {
	cgroup2Root := filepath.Join(root, "cgroup.controllers")
	if _, err := os.Stat(cgroup2Root); err != nil {
		return fmt.Errorf("cgroup v2 not mounted: %w", err)
	}

	selfCgroup, ok := discoverDelegatedPath(procFile)
	if !ok {
		return errors.New("cannot discover current cgroup path from /proc/self/cgroup")
	}

	// If already inside init/, nothing to do.
	if strings.HasSuffix(selfCgroup, "/init") || selfCgroup == root+"/init" {
		execCgroupBase = selfCgroup
		return nil
	}

	// Create init/ leaf under the current cgroup.
	initDir := filepath.Join(selfCgroup, "init")
	if err := os.Mkdir(initDir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("mkdir %s: %w", initDir, err)
	}

	// Move the current process into init/.
	procsFile := filepath.Join(initDir, "cgroup.procs")
	if err := os.WriteFile(procsFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0o644); err != nil {
		return fmt.Errorf("migrate self to %s: %w", procsFile, err)
	}

	// Now selfCgroup has no internal processes (daemon is in selfCgroup/init/).
	// Enable +memory on selfCgroup's subtree_control so that newCgroupExec
	// can create memory-limited children under selfCgroup/temenos-exec-*.
	// Note: we write to selfCgroup/cgroup.subtree_control, NOT init/cgroup.subtree_control.
	// Exec cgroups are created as siblings of init/, both under selfCgroup.
	if err := enableMemoryController(selfCgroup); err != nil {
		return fmt.Errorf("enable +memory on %s: %w", filepath.Join(selfCgroup, "cgroup.subtree_control"), err)
	}

	// Set execCgroupBase to selfCgroup so that newCgroupExec creates
	// per-exec cgroups as siblings of init/ under selfCgroup.
	execCgroupBase = selfCgroup

	return nil
}

// enableMemoryController writes "+memory" to path/cgroup.subtree_control.
// Returns nil if already set or successfully written. EBUSY triggers a retry.
func enableMemoryController(path string) error {
	subtreeCtrl := filepath.Join(path, "cgroup.subtree_control")
	for attempt := 0; attempt < 2; attempt++ {
		err := os.WriteFile(subtreeCtrl, []byte("+memory"), 0o644)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EBUSY) {
			return err
		}
	}
	return errors.New("enable +memory on subtree_control: EBUSY after retry")
}

// inK8sPod returns true when temenos is running inside a Kubernetes pod.
//
// KUBERNETES_SERVICE_HOST is set by the kubelet for every container in a pod.
// We use it only as a hint to enable filesystem probes — any process can set
// this env var, but the cgroup namespace boundary prevents containers from
// escaping their cgroup tree, so the env var alone cannot cause harm. The real
// trust signal comes from the cgroup namespace (containers cannot see or write
// the host cgroup root).
func inK8sPod() bool {
	// Probe the filesystem first (fast, no side-effects).
	if !statCgroupControllers() {
		return false
	}
	// Then check the env var.
	_, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	return ok
}

// SetupCgroupV2 performs one-time cgroup v2 init-leaf setup.
// Call once at daemon startup when --cgroupv2-memory-limit is set.
func SetupCgroupV2() error {
	if !inK8sPod() {
		return errors.New("not running inside a Kubernetes pod (KUBERNETES_SERVICE_HOST not set or cgroup v2 not mounted)")
	}
	if err := setupInitLeaf(); err != nil {
		return err
	}
	if !cgroupAvailable() {
		return errors.New("cgroup v2 with memory delegation not available after init-leaf setup")
	}
	return nil
}
