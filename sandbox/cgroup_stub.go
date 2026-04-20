//go:build !linux
// +build !linux

package sandbox

import "errors"

// setupInitLeaf is a no-op on non-Linux.
//
//nolint:unused
func setupInitLeaf() error { return errors.New("setupInitLeaf: requires Linux") }

// cgroupAvailable returns false on non-Linux.
//
//nolint:unused
func cgroupAvailable() bool { return false }

// inK8sPod always returns false on non-Linux.
//
//nolint:unused
func inK8sPod() bool { return false }

// SetupCgroupV2 is a no-op on non-Linux.
func SetupCgroupV2() error { return nil }

// Status describes the sandbox environment on non-Linux platforms.
type Status struct {
	Ready  bool    `json:"ready"`
	Checks []Check `json:"checks"`
}

// Check is one diagnostic probe result on non-Linux.
type Check struct {
	Name        string `json:"name"`
	OK          bool   `json:"ok"`
	Detail      string `json:"detail,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

// String implements fmt.Stringer for non-Linux.
func (s Status) String() string { return "temenos doctor: not available on this platform" }

// CurrentStatus always returns a single platform-unsupported check on non-Linux.
func CurrentStatus() Status {
	return Status{
		Ready: false,
		Checks: []Check{{
			Name:        "platform",
			OK:          false,
			Detail:      "non-Linux",
			Remediation: "temenos cgroup v2 sandbox requires Linux",
		}},
	}
}
