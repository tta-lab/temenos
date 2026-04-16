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

// cgroup2fsMagic is the statfs magic for cgroup v2.
const cgroup2fsMagic uint64 = 0x63677270

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
		// Empty file.
		return "", false
	}
	line := scanner.Text()

	// cgroup v2: single line "0::<path>"
	if !strings.HasPrefix(line, "0::") {
		// v1 or malformed — treat as unavailable.
		return "", false
	}
	path := strings.TrimPrefix(line, "0::")
	if path == "" {
		return "", false
	}
	// Prepend the cgroup root.
	return filepath.Join(cgroupRoot, path), true
}

// initLeafOnce ensures init-leaf migration runs exactly once.
var initLeafOnce sync.Once

// initLeafErr holds the result of the one-time init-leaf setup.
var initLeafErr error

// setupInitLeaf migrates the current process into a leaf sub-cgroup ("init/")
// so that we can enable +memory on our parent without violating the
// cgroup v2 "no internal processes" rule. Idempotent: subsequent calls
// return the cached result from the first call.
func setupInitLeaf() error {
	initLeafOnce.Do(func() {
		initLeafErr = runInitLeaf()
	})
	return initLeafErr
}

func runInitLeaf() error {
	// Guard: require cgroup v2.
	cgroup2Root := filepath.Join(cgroupRoot, "cgroup.controllers")
	if _, err := os.Stat(cgroup2Root); err != nil {
		return fmt.Errorf("cgroup v2 not mounted: %w", err)
	}

	// Discover the current delegated path.
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
	// Write our PID to cgroup.procs of init/.
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
		// EBUSY on first attempt: might be a transient kernel state; retry once.
	}
	return errors.New("enable +memory on subtree_control: EBUSY after retry")
}

// inK8sPodOnce caches the result of inK8sPod.
var inK8sPodOnce sync.Once

// inK8sPodResult holds the cached detection result.
var inK8sPodResult struct {
	once sync.Once
	val  bool
}

// inK8sPod returns true when temenos is running inside a Kubernetes pod.
// Detection checks: KUBERNETES_SERVICE_HOST env var present AND cgroup v2
// is mounted at /sys/fs/cgroup.
func inK8sPod() bool {
	inK8sPodResult.once.Do(func() {
		inK8sPodResult.val = detectK8s()
	})
	return inK8sPodResult.val
}

func detectK8s() bool {
	// Check KUBERNETES_SERVICE_HOST.
	_, k8sEnv := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	if !k8sEnv {
		return false
	}

	// Verify cgroup v2 is mounted.
	controllersFile := filepath.Join(cgroupRoot, "cgroup.controllers")
	if _, err := os.Stat(controllersFile); err != nil {
		return false
	}
	return true
}
