//go:build !linux

package sandbox

// cgroupExec is a no-op on non-Linux platforms.
type cgroupExec struct{}

func newCgroupExec(_ int) (*cgroupExec, error) { return nil, nil }
func (c *cgroupExec) addPID(_ int) error       { return nil }
func (c *cgroupExec) cleanup()                 {}

// cgroupAvailable always returns false on non-Linux platforms.
func cgroupAvailable() bool { return false }

// discoverDelegatedPath always returns ("", false) on non-Linux.
func discoverDelegatedPath(_ string) (string, bool) { return "", false }

// setupInitLeaf always returns nil on non-Linux.
func setupInitLeaf() error { return nil }

// inK8sPod always returns false on non-Linux.
func inK8sPod() bool { return false }
