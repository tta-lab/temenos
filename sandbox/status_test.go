//go:build linux
// +build linux

package sandbox

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestReadProc1CgroupPath(t *testing.T) {
	tests := []struct {
		name    string
		proc1   func() (string, error)
		want    string
		wantErr bool
	}{
		{
			name:    "valid init leaf",
			proc1:   func() (string, error) { return "/init", nil },
			want:    "/init",
			wantErr: false,
		},
		{
			name:    "valid nested path",
			proc1:   func() (string, error) { return "/kubepods/burstable/pod123/init", nil },
			want:    "/kubepods/burstable/pod123/init",
			wantErr: false,
		},
		{
			name:    "read error",
			proc1:   func() (string, error) { return "", errors.New("no such file") },
			want:    "",
			wantErr: true,
		},
		{
			name:    "bad format",
			proc1:   func() (string, error) { return "", errors.New("unexpected /proc/1/cgroup format: 1::/init") },
			want:    "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := readProc1CgroupPathReader
			readProc1CgroupPathReader = tc.proc1
			t.Cleanup(func() { readProc1CgroupPathReader = orig })

			got, err := readProc1CgroupPathReader()
			if (err != nil) != tc.wantErr {
				t.Errorf("readProc1CgroupPathReader() error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("readProc1CgroupPathReader() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCheckInitLeafImpl_Parsing(t *testing.T) {
	origRead := readProc1CgroupPathReader
	origInK8s := checkK8sPod
	t.Cleanup(func() {
		readProc1CgroupPathReader = origRead
		checkK8sPod = origInK8s
	})

	checkK8sPod = func() Check { return Check{Name: "k8s_pod", OK: true} }

	tests := []struct {
		name            string
		proc1           func() (string, error)
		wantOK          bool
		wantDetail      string
		wantRemediation bool
	}{
		{
			name:       "in /init — OK",
			proc1:      func() (string, error) { return "/init", nil },
			wantOK:     true,
			wantDetail: "PID 1 cgroup: /init",
		},
		{
			name:       "nested /init — OK",
			proc1:      func() (string, error) { return "/kubepods/burstable/pod123/init", nil },
			wantOK:     true,
			wantDetail: "PID 1 cgroup: /kubepods/burstable/pod123/init",
		},
		{
			name:            "not in /init — fail",
			proc1:           func() (string, error) { return "/", nil },
			wantOK:          false,
			wantDetail:      "does not end in /init",
			wantRemediation: true,
		},
		{
			name:            "read error",
			proc1:           func() (string, error) { return "", errors.New("cannot read /proc/1/cgroup: no such file") },
			wantOK:          false,
			wantDetail:      "cannot read",
			wantRemediation: true,
		},
		{
			name:            "bad format",
			proc1:           func() (string, error) { return "", errors.New("unexpected /proc/1/cgroup format: 1::/init") },
			wantOK:          false,
			wantDetail:      "unexpected",
			wantRemediation: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := readProc1CgroupPathReader
			readProc1CgroupPathReader = tc.proc1
			t.Cleanup(func() { readProc1CgroupPathReader = orig })

			got := checkInitLeaf()
			if got.OK != tc.wantOK {
				t.Errorf("checkInitLeaf() OK = %v, want %v", got.OK, tc.wantOK)
			}
			if !strings.Contains(got.Detail, tc.wantDetail) {
				t.Errorf("checkInitLeaf() Detail = %q, want to contain %q", got.Detail, tc.wantDetail)
			}
			if tc.wantRemediation && got.Remediation == "" {
				t.Error("checkInitLeaf() Remediation: want non-empty")
			}
		})
	}
}

func TestCheckInitLeaf_NotInK8s(t *testing.T) {
	origInK8s := checkK8sPod
	origRead := readProc1CgroupPathReader
	t.Cleanup(func() {
		checkK8sPod = origInK8s
		readProc1CgroupPathReader = origRead
	})

	checkK8sPod = func() Check {
		return Check{Name: "k8s_pod", OK: false}
	}
	readProc1CgroupPathReader = func() (string, error) {
		t.Error("readProc1CgroupPathReader should not be called when not in k8s")
		return "", errors.New("should not be called")
	}

	got := checkInitLeaf()
	if got.OK {
		t.Error("checkInitLeaf() OK: want false")
	}
	if !strings.Contains(got.Detail, "not running in a Kubernetes pod") {
		t.Errorf("checkInitLeaf() Detail = %q, want to contain 'not running in a Kubernetes pod'", got.Detail)
	}
}

func TestCheckMemoryDelegatedImpl(t *testing.T) {
	origInK8s := checkK8sPod
	origRead := readProc1CgroupPathReader
	t.Cleanup(func() {
		checkK8sPod = origInK8s
		readProc1CgroupPathReader = origRead
	})

	checkK8sPod = func() Check { return Check{Name: "k8s_pod", OK: true} }

	t.Run("not in k8s", func(t *testing.T) {
		orig := checkK8sPod
		checkK8sPod = func() Check { return Check{Name: "k8s_pod", OK: false} }
		t.Cleanup(func() { checkK8sPod = orig })

		got := checkMemoryDelegated()
		if got.OK {
			t.Error("checkMemoryDelegated() OK: want false")
		}
		if !strings.Contains(got.Detail, "not running in a Kubernetes pod") {
			t.Errorf("Detail = %q, want 'not running in a Kubernetes pod'", got.Detail)
		}
	})

	t.Run("read error", func(t *testing.T) {
		origRead := readProc1CgroupPathReader
		readProc1CgroupPathReader = func() (string, error) {
			return "", errors.New("cannot read /proc/1/cgroup: no such file")
		}
		t.Cleanup(func() { readProc1CgroupPathReader = origRead })

		got := checkMemoryDelegated()
		if got.OK {
			t.Error("checkMemoryDelegated() OK: want false")
		}
		if !strings.Contains(got.Detail, "cannot read") {
			t.Errorf("Detail = %q, want to contain 'cannot read'", got.Detail)
		}
	})
}

func TestNewStatus(t *testing.T) {
	t.Run("all ok", func(t *testing.T) {
		checks := []Check{
			{Name: "a", OK: true},
			{Name: "b", OK: true},
		}
		s := NewStatus(checks)
		if !s.Ready {
			t.Error("Ready: want true")
		}
		if len(s.Checks) != 2 {
			t.Errorf("len(Checks) = %d, want 2", len(s.Checks))
		}
	})

	t.Run("one fails", func(t *testing.T) {
		checks := []Check{
			{Name: "a", OK: true},
			{Name: "b", OK: false, Remediation: "fix it"},
		}
		s := NewStatus(checks)
		if s.Ready {
			t.Error("Ready: want false")
		}
	})
}

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
		return Check{Name: "k8s_pod", OK: true, Detail: "KUBERNETES_SERVICE_HOST set"}
	}
	checkCgroupV2Mounted = func() Check {
		return Check{Name: "cgroup_v2", OK: true, Detail: "/sys/fs/cgroup"}
	}
	checkInitLeaf = func() Check {
		return Check{Name: "init_leaf", OK: true, Detail: "PID 1 cgroup: /init"}
	}
	checkMemoryDelegated = func() Check {
		return Check{Name: "memory_delegated", OK: true, Detail: "memory in cgroup.subtree_control"}
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
		return Check{Name: "k8s_pod", OK: false, Remediation: "requires k8s"}
	}
	checkCgroupV2Mounted = func() Check {
		return Check{Name: "cgroup_v2", OK: true, Detail: "/sys/fs/cgroup"}
	}
	checkInitLeaf = func() Check {
		return Check{Name: "init_leaf", OK: false, Remediation: "daemon not started"}
	}
	checkMemoryDelegated = func() Check {
		return Check{Name: "memory_delegated", OK: false, Remediation: "memory not delegated"}
	}

	status := CurrentStatus()
	if status.Ready {
		t.Error("Ready: want false")
	}
	if len(status.Checks) != 4 {
		t.Errorf("len(Checks) = %d, want 4", len(status.Checks))
	}
}

func TestStatusString_Verbose(t *testing.T) {
	status := Status{
		Ready: true,
		Checks: []Check{
			{Name: "k8s_pod", OK: true, Detail: "KUBERNETES_SERVICE_HOST set"},
			{Name: "cgroup_v2", OK: true, Detail: "/sys/fs/cgroup"},
			{Name: "init_leaf", OK: true, Detail: "PID 1 cgroup: /init"},
			{Name: "memory_delegated", OK: true, Detail: "memory in cgroup.subtree_control"},
		},
	}

	got := status.String()
	if !strings.Contains(got, "✓ k8s_pod: KUBERNETES_SERVICE_HOST set") {
		t.Errorf("String() = %q, want to contain ✓ k8s_pod line", got)
	}
	if !strings.Contains(got, "ready: yes") {
		t.Errorf("String() = %q, want to contain 'ready: yes'", got)
	}

	failed := Status{
		Ready: false,
		Checks: []Check{
			{Name: "k8s_pod", OK: true, Detail: "KUBERNETES_SERVICE_HOST set"},
			{
				Name:        "memory_delegated",
				OK:          false,
				Detail:      "memory not in cgroup.subtree_control",
				Remediation: "set runtimeClassName: cgroup-writable",
			},
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
