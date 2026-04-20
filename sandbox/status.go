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
type Check struct {
	Name        string `json:"name"`
	OK          bool   `json:"ok"`
	Detail      string `json:"detail,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

// Probe functions — package vars for test injection (match existing pattern
// used by inK8s / cgroupAvailableStatus).
var (
	checkK8sPod          = checkK8sPodImpl
	checkCgroupV2Mounted = checkCgroupV2MountedImpl
	checkInitLeaf        = checkInitLeafImpl
	checkMemoryDelegated = checkMemoryDelegatedImpl
)

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
			Detail: "KUBERNETES_SERVICE_HOST set, cgroup v2 mounted",
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
	// Use inK8sPod as a prerequisite — if not in k8s, skip the PID 1 probe.
	if !inK8sPod() {
		return Check{
			Name:        "init_leaf",
			OK:          false,
			Remediation: "daemon has not completed init-leaf migration; check daemon startup logs for SetupCgroupV2 errors",
		}
	}

	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return Check{
			Name:        "init_leaf",
			OK:          false,
			Detail:      fmt.Sprintf("cannot read /proc/1/cgroup: %v", err),
			Remediation: "daemon has not completed init-leaf migration; check daemon startup logs for SetupCgroupV2 errors",
		}
	}

	// Format: "0::/path" — extract the path after the second colon.
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "0::") {
		return Check{
			Name:        "init_leaf",
			OK:          false,
			Detail:      fmt.Sprintf("unexpected /proc/1/cgroup format: %s", line),
			Remediation: "daemon has not completed init-leaf migration; check daemon startup logs for SetupCgroupV2 errors",
		}
	}
	path := strings.TrimPrefix(line, "0::")

	if strings.HasSuffix(path, "/init") || path == "/init" {
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
		Remediation: "daemon has not completed init-leaf migration; check daemon startup logs for SetupCgroupV2 errors",
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
			Remediation: "memory controller not delegated to pod cgroup; set runtimeClassName: cgroup-writable on the pod (or equivalent containerd cgroup_writable config)",
		}
	}

	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return Check{
			Name:        "memory_delegated",
			OK:          false,
			Detail:      fmt.Sprintf("cannot read /proc/1/cgroup: %v", err),
			Remediation: "memory controller not delegated to pod cgroup; set runtimeClassName: cgroup-writable on the pod (or equivalent containerd cgroup_writable config)",
		}
	}

	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "0::") {
		return Check{
			Name:        "memory_delegated",
			OK:          false,
			Detail:      fmt.Sprintf("unexpected /proc/1/cgroup format: %s", line),
			Remediation: "memory controller not delegated to pod cgroup; set runtimeClassName: cgroup-writable on the pod (or equivalent containerd cgroup_writable config)",
		}
	}
	path := strings.TrimPrefix(line, "0::")

	// Strip /init suffix to get the parent where +memory was written.
	parent := strings.TrimSuffix(path, "/init")
	if parent == path {
		// No /init suffix — parent is same as path (edge case: already at root).
		parent = path
	}

	subtreeCtrlPath := fmt.Sprintf("/sys/fs/cgroup%s/cgroup.subtree_control", parent)
	content, err := os.ReadFile(subtreeCtrlPath)
	if err != nil {
		return Check{
			Name:        "memory_delegated",
			OK:          false,
			Detail:      fmt.Sprintf("cannot read %s: %v", subtreeCtrlPath, err),
			Remediation: "memory controller not delegated to pod cgroup; set runtimeClassName: cgroup-writable on the pod (or equivalent containerd cgroup_writable config)",
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
		Remediation: "memory controller not delegated to pod cgroup; set runtimeClassName: cgroup-writable on the pod (or equivalent containerd cgroup_writable config)",
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
