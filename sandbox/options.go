package sandbox

import "time"

// Options configures the sandbox constructor.
type Options struct {
	BwrapPath        string        // Linux only; defaults to "bwrap"
	Timeout          time.Duration // default execution timeout
	AllowUnsandboxed bool          // if true, fall back to NoopSandbox when no platform sandbox is found
	MemoryLimitMB    int           // Linux only; cgroup v2 memory limit in MB; 0 = no limit
	KubernetesMode   bool          // Linux only; skips --proc /proc and --unshare-pid for k8s nested environments
}
