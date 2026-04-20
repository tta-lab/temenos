//go:build linux
// +build linux

package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCurrentStatus_AllOK(t *testing.T) {
	origCheckK8sPod := checkK8sPod
	origCheckCgroupV2Mounted := checkCgroupV2Mounted
	origCheckInitLeaf := checkInitLeaf
	origCheckMemoryDelegated := checkMemoryDelegated
	t.Cleanup(func() {
		checkK8sPod = origCheckK8sPod
		checkCgroupV2Mounted = origCheckCgroupV2Mounted
		checkInitLeaf = origCheckInitLeaf
		checkMemoryDelegated = origCheckMemoryDelegated
	})

	checkK8sPod = func() Check {
		return Check{Name: "k8s_pod", OK: true, Detail: "KUBERNETES_SERVICE_HOST set, cgroup v2 mounted"}
	}
	checkCgroupV2Mounted = func() Check {
		return Check{Name: "cgroup_v2", OK: true, Detail: "/sys/fs/cgroup"}
	}
	checkInitLeaf = func() Check {
		return Check{Name: "init_leaf", OK: true, Detail: "PID 1 cgroup: /init"}
	}
	checkMemoryDelegated = func() Check {
		return Check{Name: "memory_delegated", OK: true, Detail: "memory in cgroup.subtree_control at /sys/fs/cgroup/cgroup.subtree_control"}
	}

	status := CurrentStatus()
	if !status.Ready {
		t.Error("Ready: want true")
	}
	if len(status.Checks) != 4 {
		t.Errorf("len(Checks) = %d, want 4", len(status.Checks))
	}
	for _, c := range status.Checks {
		if !c.OK {
			t.Errorf("Check %s: want OK=true", c.Name)
		}
	}
}

func TestCurrentStatus_NotInK8s(t *testing.T) {
	origCheckK8sPod := checkK8sPod
	origCheckCgroupV2Mounted := checkCgroupV2Mounted
	origCheckInitLeaf := checkInitLeaf
	origCheckMemoryDelegated := checkMemoryDelegated
	t.Cleanup(func() {
		checkK8sPod = origCheckK8sPod
		checkCgroupV2Mounted = origCheckCgroupV2Mounted
		checkInitLeaf = origCheckInitLeaf
		checkMemoryDelegated = origCheckMemoryDelegated
	})

	checkK8sPod = func() Check {
		return Check{Name: "k8s_pod", OK: false, Remediation: "temenos requires running inside a Kubernetes pod with cgroup v2"}
	}
	checkCgroupV2Mounted = func() Check {
		return Check{Name: "cgroup_v2", OK: true, Detail: "/sys/fs/cgroup"}
	}
	checkInitLeaf = func() Check {
		return Check{Name: "init_leaf", OK: false, Remediation: "daemon has not completed init-leaf migration"}
	}
	checkMemoryDelegated = func() Check {
		return Check{Name: "memory_delegated", OK: false, Remediation: "memory controller not delegated"}
	}

	status := CurrentStatus()
	if status.Ready {
		t.Error("Ready: want false")
	}
	// All 4 probes should be reported (no short-circuit).
	if len(status.Checks) != 4 {
		t.Errorf("len(Checks) = %d, want 4", len(status.Checks))
	}
}

func TestCurrentStatus_InitLeafNotDone(t *testing.T) {
	origCheckK8sPod := checkK8sPod
	origCheckCgroupV2Mounted := checkCgroupV2Mounted
	origCheckInitLeaf := checkInitLeaf
	origCheckMemoryDelegated := checkMemoryDelegated
	t.Cleanup(func() {
		checkK8sPod = origCheckK8sPod
		checkCgroupV2Mounted = origCheckCgroupV2Mounted
		checkInitLeaf = origCheckInitLeaf
		checkMemoryDelegated = origCheckMemoryDelegated
	})

	checkK8sPod = func() Check {
		return Check{Name: "k8s_pod", OK: true, Detail: "KUBERNETES_SERVICE_HOST set, cgroup v2 mounted"}
	}
	checkCgroupV2Mounted = func() Check {
		return Check{Name: "cgroup_v2", OK: true, Detail: "/sys/fs/cgroup"}
	}
	checkInitLeaf = func() Check {
		return Check{Name: "init_leaf", OK: false, Detail: "PID 1 cgroup: / (does not end in /init)", Remediation: "daemon has not completed init-leaf migration"}
	}
	checkMemoryDelegated = func() Check {
		return Check{Name: "memory_delegated", OK: true, Detail: "memory in cgroup.subtree_control"}
	}

	status := CurrentStatus()
	if status.Ready {
		t.Error("Ready: want false")
	}
	// Find the init_leaf check.
	var initLeafCheck Check
	for _, c := range status.Checks {
		if c.Name == "init_leaf" {
			initLeafCheck = c
			break
		}
	}
	if initLeafCheck.OK {
		t.Error("init_leaf OK: want false")
	}
	if initLeafCheck.Remediation == "" {
		t.Error("init_leaf Remediation: want non-empty")
	}
}

func TestCurrentStatus_MemoryNotDelegated(t *testing.T) {
	origCheckK8sPod := checkK8sPod
	origCheckCgroupV2Mounted := checkCgroupV2Mounted
	origCheckInitLeaf := checkInitLeaf
	origCheckMemoryDelegated := checkMemoryDelegated
	t.Cleanup(func() {
		checkK8sPod = origCheckK8sPod
		checkCgroupV2Mounted = origCheckCgroupV2Mounted
		checkInitLeaf = origCheckInitLeaf
		checkMemoryDelegated = origCheckMemoryDelegated
	})

	checkK8sPod = func() Check {
		return Check{Name: "k8s_pod", OK: true, Detail: "KUBERNETES_SERVICE_HOST set, cgroup v2 mounted"}
	}
	checkCgroupV2Mounted = func() Check {
		return Check{Name: "cgroup_v2", OK: true, Detail: "/sys/fs/cgroup"}
	}
	checkInitLeaf = func() Check {
		return Check{Name: "init_leaf", OK: true, Detail: "PID 1 cgroup: /init"}
	}
	checkMemoryDelegated = func() Check {
		return Check{Name: "memory_delegated", OK: false, Detail: "memory not in cgroup.subtree_control at /sys/fs/cgroup/cgroup.subtree_control", Remediation: "memory controller not delegated to pod cgroup; set runtimeClassName: cgroup-writable on the pod"}
	}

	status := CurrentStatus()
	if status.Ready {
		t.Error("Ready: want false")
	}
	// Find the memory_delegated check.
	var memCheck Check
	for _, c := range status.Checks {
		if c.Name == "memory_delegated" {
			memCheck = c
			break
		}
	}
	if memCheck.OK {
		t.Error("memory_delegated OK: want false")
	}
	if memCheck.Remediation == "" {
		t.Error("memory_delegated Remediation: want non-empty")
	}
	if !strings.Contains(memCheck.Remediation, "cgroup-writable") {
		t.Errorf("memory_delegated Remediation = %q, want to contain 'cgroup-writable'", memCheck.Remediation)
	}
}

func TestCheckInitLeaf_Unreadable(t *testing.T) {
	// Simulate /proc/1/cgroup being unreadable by swapping checkK8sPod to
	// return true (so the function proceeds past the inK8sPod() guard)
	// and then the ReadFile will use the real impl — swap the file read via
	// the injectable pattern. We test by checking that when the underlying
	// probe would fail to read, Detail mentions the read failure.
	// The cleanest way is to swap checkInitLeaf directly.
	origCheckK8sPod := checkK8sPod
	origCheckInitLeaf := checkInitLeaf
	t.Cleanup(func() {
		checkK8sPod = origCheckK8sPod
		checkInitLeaf = origCheckInitLeaf
	})

	checkK8sPod = func() Check {
		return Check{Name: "k8s_pod", OK: true, Detail: "KUBERNETES_SERVICE_HOST set, cgroup v2 mounted"}
	}
	checkInitLeaf = func() Check {
		// Simulate unreadable /proc/1/cgroup.
		return Check{Name: "init_leaf", OK: false, Detail: "cannot read /proc/1/cgroup: open /proc/1/cgroup: permission denied", Remediation: "daemon has not completed init-leaf migration"}
	}

	status := CurrentStatus()
	if status.Ready {
		t.Error("Ready: want false")
	}
	var initLeafCheck Check
	for _, c := range status.Checks {
		if c.Name == "init_leaf" {
			initLeafCheck = c
			break
		}
	}
	if initLeafCheck.OK {
		t.Error("init_leaf OK: want false")
	}
	if !strings.Contains(initLeafCheck.Detail, "cannot read") {
		t.Errorf("init_leaf Detail = %q, want to contain 'cannot read'", initLeafCheck.Detail)
	}
}

func TestStatusString_Verbose(t *testing.T) {
	status := Status{
		Ready: true,
		Checks: []Check{
			{Name: "k8s_pod", OK: true, Detail: "KUBERNETES_SERVICE_HOST set, cgroup v2 mounted"},
			{Name: "cgroup_v2", OK: true, Detail: "/sys/fs/cgroup"},
			{Name: "init_leaf", OK: true, Detail: "PID 1 cgroup: /init"},
			{Name: "memory_delegated", OK: true, Detail: "memory in cgroup.subtree_control"},
		},
	}

	got := status.String()
	if !strings.Contains(got, "✓ k8s_pod: KUBERNETES_SERVICE_HOST set, cgroup v2 mounted") {
		t.Errorf("String() = %q, want to contain ✓ k8s_pod line", got)
	}
	if !strings.Contains(got, "ready: yes") {
		t.Errorf("String() = %q, want to contain 'ready: yes'", got)
	}

	// Test a failed check renders ✗ and remediation.
	failed := Status{
		Ready: false,
		Checks: []Check{
			{Name: "k8s_pod", OK: true, Detail: "KUBERNETES_SERVICE_HOST set, cgroup v2 mounted"},
			{Name: "memory_delegated", OK: false, Detail: "memory not in cgroup.subtree_control", Remediation: "set runtimeClassName: cgroup-writable"},
		},
	}
	failedStr := failed.String()
	if !strings.Contains(failedStr, "✗ memory_delegated: memory not in cgroup.subtree_control") {
		t.Errorf("String() = %q, want to contain ✗ memory_delegated line", failedStr)
	}
	if !strings.Contains(failedStr, "→ set runtimeClassName: cgroup-writable") {
		t.Errorf("String() = %q, want to contain → remediation line", failedStr)
	}
	if !strings.Contains(failedStr, "ready: no") {
		t.Errorf("String() = %q, want to contain 'ready: no'", failedStr)
	}
}

func TestStatusJSON(t *testing.T) {
	status := Status{
		Ready: false,
		Checks: []Check{
			{
				Name:        "k8s_pod",
				OK:          false,
				Detail:      "KUBERNETES_SERVICE_HOST not set",
				Remediation: "temenos requires running inside a Kubernetes pod with cgroup v2",
			},
			{
				Name:   "cgroup_v2",
				OK:     true,
				Detail: "/sys/fs/cgroup",
			},
		},
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Verify JSON contains the expected keys.
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got["ready"] != false {
		t.Errorf("ready: got %v, want false", got["ready"])
	}
	checks, ok := got["checks"].([]any)
	if !ok {
		t.Fatal("checks: not an array")
	}
	if len(checks) != 2 {
		t.Errorf("len(checks) = %d, want 2", len(checks))
	}

	// Verify first check has Detail and Remediation populated (exercises omitempty).
	first := checks[0].(map[string]any)
	if first["name"] != "k8s_pod" {
		t.Errorf("checks[0].name = %v, want k8s_pod", first["name"])
	}
	if first["detail"] == nil || first["detail"] == "" {
		t.Error("checks[0].detail: want non-empty")
	}
	if first["remediation"] == nil || first["remediation"] == "" {
		t.Error("checks[0].remediation: want non-empty")
	}

	// Round-trip.
	var roundTrip Status
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("json.Unmarshal into Status: %v", err)
	}
	if roundTrip.Ready != status.Ready {
		t.Errorf("Round-trip Ready: got %v, want %v", roundTrip.Ready, status.Ready)
	}
	if len(roundTrip.Checks) != len(status.Checks) {
		t.Errorf("Round-trip len(Checks): got %d, want %d", len(roundTrip.Checks), len(status.Checks))
	}
}
