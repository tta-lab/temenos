//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDiscoverDelegatedPath(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantOk  bool
	}{
		{
			name:    "v2 single line deep path",
			content: "0::/user.slice/user-1000.slice/user@1000.service/app.slice/app.service\n",
			want:    "/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/app.slice/app.service",
			wantOk:  true,
		},
		{
			name:    "v2 nested deeper",
			content: "0::/system.slice/temenos.service\n",
			want:    "/sys/fs/cgroup/system.slice/temenos.service",
			wantOk:  true,
		},
		{
			name:    "v2 root",
			content: "0::/\n",
			want:    "/sys/fs/cgroup",
			wantOk:  true,
		},
		{
			name:    "v1 multi-line ignored",
			content: "12:memory:/user.slice\n11:cpu,cpuacct:/user.slice\n",
			want:    "",
			wantOk:  false,
		},
		{
			name:    "empty file",
			content: "",
			want:    "",
			wantOk:  false,
		},
		{
			name:    "malformed no colon",
			content: "/user.slice/user-1000.slice/user@1000.service\n",
			want:    "",
			wantOk:  false,
		},
		{
			name:    "malformed no path after colon",
			content: "0::\n",
			want:    "",
			wantOk:  false,
		},
		{
			name:    "v2 whitespace path",
			content: "0::/user.slice/user-1000.slice/user@1000.service\n",
			want:    "/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service",
			wantOk:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			procFile := filepath.Join(tmp, "cgroup")
			if err := os.WriteFile(procFile, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}

			got, ok := discoverDelegatedPath(procFile)
			if ok != tc.wantOk {
				t.Errorf("discoverDelegatedPath(%q) ok = %v, want %v", tc.content, ok, tc.wantOk)
				return
			}
			if got != tc.want {
				t.Errorf("discoverDelegatedPath(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

func TestInK8sPod(t *testing.T) {
	tests := []struct {
		name    string
		envSet  bool
		cgroup2 bool
		want    bool
	}{
		{
			name:    "kubernetes env set and cgroup2 mounted",
			envSet:  true,
			cgroup2: true,
			want:    true,
		},
		{
			name:    "kubernetes env set but cgroup v1",
			envSet:  true,
			cgroup2: false,
			want:    false,
		},
		{
			name:    "kubernetes env not set",
			envSet:  false,
			cgroup2: true,
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Save and restore the injected hook.
			orig := statCgroupControllers
			defer func() { statCgroupControllers = orig }()

			statCgroupControllers = func() bool { return tc.cgroup2 }

			// t.Setenv is safe even when unset (clears the var).
			if tc.envSet {
				t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
			} else {
				t.Setenv("KUBERNETES_SERVICE_HOST", "")
			}

			got := inK8sPod()
			if got != tc.want {
				t.Errorf("inK8sPod() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunInitLeaf(t *testing.T) {
	// Save and restore package globals between subtests.
	origExecCgroupBase := execCgroupBase
	origInitLeafOnce := initLeafOnce
	origInitLeafErr := initLeafErr
	origInitLeafSucceeded := initLeafSucceeded
	t.Cleanup(func() {
		execCgroupBase = origExecCgroupBase
		initLeafOnce = origInitLeafOnce
		initLeafErr = origInitLeafErr
		initLeafSucceeded = origInitLeafSucceeded
	})

	// writeFile is a helper to write content to a path, fatal on error.
	writeFile := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writeFile %s: %v", path, err)
		}
	}

	// buildTree creates a minimal fake cgroup v2 tree under root:
	//   root/
	//     cgroup.controllers  — presence marks cgroup v2
	//     cgroup.subtree_control
	//     cgroup.procs
	//     fake/
	//       path/
	//         cgroup.procs
	buildTree := func(root string) {
		writeFile(filepath.Join(root, "cgroup.controllers"), "memory pids cpu io")
		writeFile(filepath.Join(root, "cgroup.subtree_control"), "")
		writeFile(filepath.Join(root, "cgroup.procs"), "")
		if err := os.MkdirAll(filepath.Join(root, "fake", "path"), 0o755); err != nil {
			t.Fatalf("mkdir fake/path: %v", err)
		}
		writeFile(filepath.Join(root, "fake", "path", "cgroup.procs"), "")
	}

	t.Run("happy path", func(t *testing.T) {
		root := t.TempDir()
		procFile := filepath.Join(root, "cgroup_map")
		writeFile(procFile, "0::/fake/path\n")
		buildTree(root)

		err := runInitLeafAt(root, procFile)
		if err != nil {
			t.Fatalf("runInitLeafAt: %v", err)
		}

		selfCgroup := filepath.Join(root, "fake", "path")

		// init/ created and contains our PID.
		initProcs := filepath.Join(selfCgroup, "init", "cgroup.procs")
		data, err := os.ReadFile(initProcs)
		if err != nil {
			t.Fatalf("read init/cgroup.procs: %v", err)
		}
		pidStr := strings.TrimSpace(string(data))
		if pidStr != fmt.Sprintf("%d", os.Getpid()) {
			t.Errorf("init/cgroup.procs = %q, want %d", pidStr, os.Getpid())
		}

		// +memory written to selfCgroup/cgroup.subtree_control.
		subtreeCtrl := filepath.Join(selfCgroup, "cgroup.subtree_control")
		ctrlData, err := os.ReadFile(subtreeCtrl)
		if err != nil {
			t.Fatalf("read cgroup.subtree_control: %v", err)
		}
		if !strings.Contains(string(ctrlData), "memory") {
			t.Errorf("cgroup.subtree_control = %q, want memory", string(ctrlData))
		}

		// execCgroupBase set to selfCgroup.
		if execCgroupBase != selfCgroup {
			t.Errorf("execCgroupBase = %q, want %q", execCgroupBase, selfCgroup)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		root := t.TempDir()
		procFile := filepath.Join(root, "cgroup_map")
		writeFile(procFile, "0::/fake/path\n")
		buildTree(root)

		err1 := runInitLeafAt(root, procFile)
		if err1 != nil {
			t.Fatalf("first runInitLeafAt: %v", err1)
		}

		execCgroupBaseBefore := execCgroupBase

		// Reset initLeafOnce so we can call runInitLeafAt again within this subtest.
		initLeafOnce = sync.Once{}

		err2 := runInitLeafAt(root, procFile)
		if err2 != nil {
			t.Fatalf("second runInitLeafAt: %v", err2)
		}

		if execCgroupBase != execCgroupBaseBefore {
			t.Errorf("execCgroupBase changed on second call: %q -> %q",
				execCgroupBaseBefore, execCgroupBase)
		}
	})

	t.Run("already-in-init", func(t *testing.T) {
		root := t.TempDir()
		procFile := filepath.Join(root, "cgroup_map")
		// Simulate a daemon restart: we are already inside the init/ leaf.
		writeFile(procFile, "0::/fake/path/init\n")
		buildTree(root)

		// Pre-create init/ and place our PID there.
		initPath := filepath.Join(root, "fake", "path", "init")
		if err := os.MkdirAll(initPath, 0o755); err != nil {
			t.Fatalf("mkdir init: %v", err)
		}
		writeFile(filepath.Join(initPath, "cgroup.procs"),
			fmt.Sprintf("%d\n", os.Getpid()))

		err := runInitLeafAt(root, procFile)
		if err != nil {
			t.Fatalf("runInitLeafAt (already-in-init): %v", err)
		}

		// execCgroupBase should be the PARENT of init/ so per-exec cgroups land
		// as siblings of init/, not nested under it.
		// NOTE: this subtest currently FAILS because the code sets
		// execCgroupBase = selfCgroup (which ends in "/init"). Step 14 fixes
		// this with strings.TrimSuffix(selfCgroup, "/init"). This comment
		// documents the intentional cross-step coupling.
		wantBase := strings.TrimSuffix(initPath, "/init")
		if execCgroupBase != wantBase {
			t.Errorf("execCgroupBase = %q, want %q (TrimSuffix fix pending Step 14)",
				execCgroupBase, wantBase)
		}
	})
}
