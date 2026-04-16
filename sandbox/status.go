//go:build linux

package sandbox

// Status describes the current cgroup v2 environment.
type Status struct {
	InK8sPod      bool   `json:"in_k8s_pod"`
	CgroupV2      bool   `json:"cgroup_v2"`
	DelegatedPath string `json:"delegated_path,omitempty"`
	MemoryCtrl    bool   `json:"memory_ctrl"`
	InitLeafDone  bool   `json:"init_leaf_done"`
}

// CurrentStatus returns a snapshot of the cgroup v2 environment.
func CurrentStatus() Status {
	s := Status{
		InK8sPod:      inK8sPod(),
		CgroupV2:      cgroupAvailable(),
		DelegatedPath: discoveredPath,
		MemoryCtrl:    hasController(discoveredPath, "memory"),
	}
	// InitLeafDone is true when init-leaf setup was actually invoked and succeeded.
	// initLeafErr is nil only if setupInitLeaf() has been called AND succeeded.
	// If setupInitLeaf() was never called, initLeafErr is nil and initLeafDone
	// would be false — but we need a way to distinguish "never called" from
	// "called and succeeded". Use a separate boolean flag.
	s.InitLeafDone = initLeafSucceeded
	return s
}

func (s Status) String() string {
	switch {
	case !s.CgroupV2:
		return "cgroup v2: not available (requires Linux with cgroup v2 mounted)"
	case !s.InK8sPod:
		return "cgroup v2: available, not in k8s pod (memory limits will be a no-op)"
	case !s.MemoryCtrl:
		return "k8s pod: " + s.DelegatedPath + ", cgroup v2: ok, memory controller: not delegated"
	case s.InitLeafDone:
		return "k8s pod: " + s.DelegatedPath + ", cgroup v2: ready, memory limits: enabled"
	default:
		return "k8s pod: " + s.DelegatedPath + ", cgroup v2: in-k8s detection passed, init-leaf: not run (daemon not started)"
	}
}
