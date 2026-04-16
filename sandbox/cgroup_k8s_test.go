//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
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
