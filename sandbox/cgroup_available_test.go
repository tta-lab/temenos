package sandbox

import "testing"

func TestCgroupAvailable(t *testing.T) {
	// Just verify it doesn't panic on any platform.
	// Returns false on macOS, true on Linux with cgroup v2.
	_ = cgroupAvailable()
}
