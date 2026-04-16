//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// execWithCgroup runs the command with cgroup v2 memory limits.
// Caller must call cg.cleanup() after runCmd returns.
func execWithCgroup(cmd *exec.Cmd, cg *cgroupExec) error {
	cgFD, err := os.Open(cg.path)
	if err != nil {
		return fmt.Errorf("sandbox: open cgroup dir: %w", err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		UseCgroupFD: true,
		CgroupFD:    int(cgFD.Fd()),
	}
	return nil
}
