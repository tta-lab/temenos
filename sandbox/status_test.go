//go:build linux

package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusString(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		substr string
	}{
		{
			name: "cgroup v2 not available",
			status: Status{
				CgroupV2: false,
			},
			substr: "cgroup v2: not available",
		},
		{
			name: "cgroup v2 available, not in k8s pod",
			status: Status{
				CgroupV2:      true,
				InK8sPod:      false,
				DelegatedPath: "/sys/fs/cgroup/user.slice/user-1000.slice",
			},
			substr: "cgroup v2: available, not in k8s pod",
		},
		{
			name: "in k8s pod, memory controller not delegated",
			status: Status{
				CgroupV2:      true,
				InK8sPod:      true,
				DelegatedPath: "/sys/fs/cgroup/user.slice/user-1000.slice",
				MemoryCtrl:    false,
			},
			substr: "memory controller: not delegated",
		},
		{
			name: "init leaf done, memory limits enabled",
			status: Status{
				CgroupV2:      true,
				InK8sPod:      true,
				DelegatedPath: "/sys/fs/cgroup/user.slice/user-1000.slice",
				MemoryCtrl:    true,
				InitLeafDone:  true,
			},
			substr: "memory limits: enabled",
		},
		{
			name: "in k8s, init leaf not run",
			status: Status{
				CgroupV2:      true,
				InK8sPod:      true,
				DelegatedPath: "/sys/fs/cgroup/user.slice/user-1000.slice",
				MemoryCtrl:    true,
				InitLeafDone:  false,
			},
			substr: "init-leaf: not run",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.status.String()
			if !strings.Contains(got, tc.substr) {
				t.Errorf("Status.String() = %q, want to contain %q", got, tc.substr)
			}
		})
	}
}

const fakeCgroupPath = "/fake/cgroup/path"

func TestCurrentStatus(t *testing.T) {
	// Save and restore globals.
	origExecCgroupBase := execCgroupBase
	origInitLeafSucceeded := initLeafSucceeded
	origDiscoveredPath := discoveredPath
	origInK8s := inK8s
	origCgroupAvailableStatus := cgroupAvailableStatus
	t.Cleanup(func() {
		execCgroupBase = origExecCgroupBase
		initLeafSucceeded = origInitLeafSucceeded
		discoveredPath = origDiscoveredPath
		inK8s = origInK8s
		cgroupAvailableStatus = origCgroupAvailableStatus
	})

	// Simulate post-init-leaf state.
	execCgroupBase = fakeCgroupPath
	initLeafSucceeded = true
	discoveredPath = fakeCgroupPath

	// Stub both probes to return true.
	inK8s = func() bool { return true }
	cgroupAvailableStatus = func() bool { return true }

	status := CurrentStatus()

	if !status.InK8sPod {
		t.Error("InK8sPod: want true")
	}
	if !status.InitLeafDone {
		t.Error("InitLeafDone: want true")
	}
	if status.DelegatedPath != fakeCgroupPath {
		t.Errorf("DelegatedPath = %q, want %s", status.DelegatedPath, fakeCgroupPath)
	}
	if !status.CgroupV2 {
		t.Error("CgroupV2: want true")
	}
}

func TestStatusJSON(t *testing.T) {
	status := Status{
		InK8sPod:      true,
		CgroupV2:      true,
		DelegatedPath: "/sys/fs/cgroup/user.slice/user-1000.slice",
		MemoryCtrl:    true,
		InitLeafDone:  true,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got Status
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.InK8sPod != status.InK8sPod {
		t.Errorf("InK8sPod: got %v, want %v", got.InK8sPod, status.InK8sPod)
	}
	if got.CgroupV2 != status.CgroupV2 {
		t.Errorf("CgroupV2: got %v, want %v", got.CgroupV2, status.CgroupV2)
	}
	if got.DelegatedPath != status.DelegatedPath {
		t.Errorf("DelegatedPath: got %v, want %v", got.DelegatedPath, status.DelegatedPath)
	}
	if got.MemoryCtrl != status.MemoryCtrl {
		t.Errorf("MemoryCtrl: got %v, want %v", got.MemoryCtrl, status.MemoryCtrl)
	}
	if got.InitLeafDone != status.InitLeafDone {
		t.Errorf("InitLeafDone: got %v, want %v", got.InitLeafDone, status.InitLeafDone)
	}
}
