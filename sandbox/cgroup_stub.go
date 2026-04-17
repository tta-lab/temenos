//go:build !linux

package sandbox

import "errors"

// cgroupExec is a no-op on non-Linux platforms.
//
//nolint:unused
type cgroupExec struct{}

// newCgroupExec is a no-op on non-Linux.
//
//nolint:unused
func newCgroupExec(_ int) (*cgroupExec, error) { return nil, nil }

// addPID is a no-op on non-Linux.
//
//nolint:unused
func (c *cgroupExec) addPID(_ int) error { return nil }

// cleanup is a no-op on non-Linux.
//
//nolint:unused
func (c *cgroupExec) cleanup() {}

// cgroupAvailable always returns false on non-Linux platforms.
func cgroupAvailable() bool { return false }

// cgroupV2Reason on non-Linux platforms always reports the platform sentinel.
func cgroupV2Reason() error { return errors.New("cgroup v2 not supported on this platform") }

// discoverDelegatedPath always returns ("", false) on non-Linux.
//
//nolint:unused
func discoverDelegatedPath(_ string) (string, bool) { return "", false }

// setupInitLeaf always returns nil on non-Linux.
//
//nolint:unused
func setupInitLeaf() error { return nil }

// inK8sPod always returns false on non-Linux.
//
//nolint:unused
func inK8sPod() bool { return false }

// SetupCgroupV2 always returns nil on non-Linux.
func SetupCgroupV2() error { return nil }

// Status describes the sandbox environment. Empty on non-Linux.
type Status struct{}

// String implements fmt.Stringer for non-Linux.
func (s Status) String() string { return "sandbox status: not available on this platform" }

// CurrentStatus always returns an empty status on non-Linux.
func CurrentStatus() Status { return Status{} }
