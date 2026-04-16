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

func runInitLeaf() error {
	cgroup2Root := filepath.Join(cgroupRoot, "cgroup.controllers")
	if _, err := os.Stat(cgroup2Root); err != nil {
		return fmt.Errorf("cgroup v2 not mounted: %w", err)
	}

	selfCgroup, ok := discoverDelegatedPath("/proc/self/cgroup")
	if !ok {
		return errors.New("cannot discover current cgroup path from /proc/self/cgroup")
	}

	// If already inside init/, nothing to do.
	if strings.HasSuffix(selfCgroup, "/init") || selfCgroup == cgroupRoot+"/init" {
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

	// Enable +memory on our parent so children can use it.
	subtreeCtrl := filepath.Join(selfCgroup, "cgroup.subtree_control")
	for attempt := 0; attempt < 2; attempt++ {
		err := os.WriteFile(subtreeCtrl, []byte("+memory"), 0o644)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EBUSY) {
			return fmt.Errorf("enable +memory on %s: %w", subtreeCtrl, err)
		}
	}
	return errors.New("enable +memory on subtree_control: EBUSY after retry")
}

// inK8sPod returns true when temenos is running inside a Kubernetes pod.
func inK8sPod() bool {
	_, k8sEnv := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	if !k8sEnv {
		return false
	}
	controllersFile := filepath.Join(cgroupRoot, "cgroup.controllers")
	_, err := os.Stat(controllersFile)
	return err == nil
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
