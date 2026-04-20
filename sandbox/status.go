//go:build linux
// +build linux

package sandbox

import (
	"fmt"
	"os"
	"strings"
)

// Status reports per-check diagnostic results for the sandbox runtime.
type Status struct {
	Ready  bool    `json:"ready"`
	Checks []Check `json:"checks"`
}

// Check is one diagnostic probe with a prescriptive remediation on failure.
// Detail is only meaningful on failure; Remediation is always meaningful on failure.
type Check struct {
	Name        string `json:"name"`
	OK          bool   `json:"ok"`
	Detail      string `json:"detail,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

const (
	initLeafRemediation  = "init-leaf migration requires a cgroup v2 environment; check daemon startup logs"
	memoryDelRemediation = "memory controller not delegated to pod cgroup; " +
		"set runtimeClassName: cgroup-writable on the pod " +
		"(or equivalent containerd cgroup_writable config)"
)

const initLeafPathSuffix = "/init"

// Probe functions — package vars for test injection (match existing pattern
// used by inK8s / cgroupAvailableStatus).
var (
	checkK8sPod          = checkK8sPodImpl
	checkCgroupV2Mounted = checkCgroupV2MountedImpl
	checkInitLeaf        = checkInitLeafImpl
	checkMemoryDelegated = checkMemoryDelegatedImpl

	// readProc1CgroupPathReader is injectable for testing.
	readProc1CgroupPathReader = readProc1CgroupPath
)

// readProc1CgroupPath reads /proc/1/cgroup and returns the cgroup path
// (everything after "0::"). It returns an error if the file cannot be read
// or has an unexpected format.
func readProc1CgroupPath() (string, error) {
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return "", fmt.Errorf("cannot read /proc/1/cgroup: %w", err)
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "0::") {
		return "", fmt.Errorf("unexpected /proc/1/cgroup format: %s", line)
	}
	return strings.TrimPrefix(line, "0::"), nil
}

// NewStatus constructs a Status from a list of checks, computing Ready from the
// checks' OK fields.
func NewStatus(checks []Check) Status {
	ready := true
	for _, c := range checks {
		if !c.OK {
			ready = false
		}
	}
	return Status{Ready: ready, Checks: checks}
}

// CurrentStatus returns a snapshot of the cgroup v2 environment via filesystem probes.
func CurrentStatus() Status {
	checks := []Check{
		checkK8sPod(),
		checkCgroupV2Mounted(),
		checkInitLeaf(),
		checkMemoryDelegated(),
	}
	ready := true
	for _, c := range checks {
		if !c.OK {
			ready = false
		}
	}
	return Status{Ready: ready, Checks: checks}
}

// checkK8sPodImpl probes whether we are running inside a Kubernetes pod.
func checkK8sPodImpl() Check {
	if inK8sPod() {
		return Check{
			Name:   "k8s_pod",
			OK:     true,
			Detail: "KUBERNETES_SERVICE_HOST set",
		}
	}
	return Check{
		Name:        "k8s_pod",
		OK:          false,
		Remediation: "temenos requires running inside a Kubernetes pod with cgroup v2",
	}
}

// checkCgroupV2MountedImpl probes whether /sys/fs/cgroup is a cgroup v2 mount.
func checkCgroupV2MountedImpl() Check {
	_, err := os.Stat("/sys/fs/cgroup/cgroup.controllers")
	if err == nil {
		return Check{
			Name:   "cgroup_v2",
			OK:     true,
			Detail: "/sys/fs/cgroup",
		}
	}
	return Check{
		Name:        "cgroup_v2",
		OK:          false,
		Remediation: "mount cgroup v2 (systemd.unified_cgroup_hierarchy=1 or kernel cmdline equivalent)",
	}
}

// checkInitLeafImpl reads /proc/1/cgroup and checks whether PID 1 is in a cgroup
// named "init". This fingerprint confirms the daemon has completed init-leaf
// migration and now owns the /init leaf cgroup.
//
// Assumes temenos is PID 1 in the pod (standard k8s deployment). Sidecar
// deployments are unsupported and will probe the wrong process's cgroup.
func checkInitLeafImpl() Check {
	if !inK8sPod() {
		return Check{
			Name:        "init_leaf",
			OK:          false,
			Detail:      "not running in a Kubernetes pod (KUBERNETES_SERVICE_HOST not set)",
			Remediation: initLeafRemediation,
		}
	}

	path, err := readProc1CgroupPathReader()
	if err != nil {
		return Check{
			Name:        "init_leaf",
			OK:          false,
			Detail:      err.Error(),
			Remediation: initLeafRemediation,
		}
	}

	if strings.HasSuffix(path, initLeafPathSuffix) || path == initLeafPathSuffix {
		return Check{
			Name:   "init_leaf",
			OK:     true,
			Detail: fmt.Sprintf("PID 1 cgroup: %s", path),
		}
	}
	return Check{
		Name:        "init_leaf",
		OK:          false,
		Detail:      fmt.Sprintf("PID 1 cgroup: %s (does not end in /init)", path),
		Remediation: initLeafRemediation,
	}
}

// checkMemoryDelegatedImpl reads /proc/1/cgroup to derive the daemon's cgroup path,
// strips the /init suffix to find the parent (where setupInitLeaf wrote +memory),
// and checks whether the parent's cgroup.subtree_control contains "memory".
//
// The parent is the same directory semantics as execCgroupBase — setupInitLeaf
// enables +memory on the parent so that the /init leaf can have memory limits.
func checkMemoryDelegatedImpl() Check {
	if !inK8sPod() {
		return Check{
			Name:        "memory_delegated",
			OK:          false,
			Detail:      "not running in a Kubernetes pod (KUBERNETES_SERVICE_HOST not set)",
			Remediation: memoryDelRemediation,
		}
	}

	path, err := readProc1CgroupPathReader()
	if err != nil {
		return Check{
			Name:        "memory_delegated",
			OK:          false,
			Detail:      err.Error(),
			Remediation: memoryDelRemediation,
		}
	}

	// Strip /init suffix to get the parent where +memory was written.
	// If no /init suffix, parent is the same as path.
	parent := strings.TrimSuffix(path, initLeafPathSuffix)

	subtreeCtrlPath := fmt.Sprintf("/sys/fs/cgroup%s/cgroup.subtree_control", parent)
	content, err := os.ReadFile(subtreeCtrlPath)
	if err != nil {
		return Check{
			Name:        "memory_delegated",
			OK:          false,
			Detail:      fmt.Sprintf("cannot read %s: %v", subtreeCtrlPath, err),
			Remediation: memoryDelRemediation,
		}
	}

	if strings.Contains(string(content), "memory") {
		return Check{
			Name:   "memory_delegated",
			OK:     true,
			Detail: fmt.Sprintf("memory in cgroup.subtree_control at %s", subtreeCtrlPath),
		}
	}
	return Check{
		Name:        "memory_delegated",
		OK:          false,
		Detail:      fmt.Sprintf("memory not in cgroup.subtree_control at %s", subtreeCtrlPath),
		Remediation: memoryDelRemediation,
	}
}

// String implements fmt.Stringer for Status.
func (s Status) String() string {
	var b strings.Builder
	for _, c := range s.Checks {
		if c.OK {
			fmt.Fprintf(&b, "✓ %s: %s\n", c.Name, c.Detail)
		} else {
			fmt.Fprintf(&b, "✗ %s: %s\n", c.Name, c.Detail)
			if c.Remediation != "" {
				fmt.Fprintf(&b, "  → %s\n", c.Remediation)
			}
		}
	}
	if s.Ready {
		fmt.Fprint(&b, "ready: yes")
	} else {
		fmt.Fprint(&b, "ready: no")
	}
	return b.String()
}
