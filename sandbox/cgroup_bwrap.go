//go:build linux

package sandbox

import (
	"fmt"
	"os/exec"
	"syscall"
)

// execWithCgroup opens the cgroup directory as an FD and attaches it to the
// SysProcAttr for CLONE_INTO_CGROUP. The fd stays alive as part of cg.fd
// (kept by the caller's defer of cg.cleanup()) until after cmd.Start().
func execWithCgroup(cmd *exec.Cmd, cg *cgroupExec) error {
	// Store the fd on the cgroupExec so it stays alive until cleanup().
	// syscall.Close-on-exec is disabled so the fd is inherited by clone3.
	fd, err := syscall.Open(cg.path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("sandbox: open cgroup dir: %w", err)
	}
	cg.fd = fd
	cmd.SysProcAttr = &syscall.SysProcAttr{
		UseCgroupFD: true,
		CgroupFD:    fd,
	}
	return nil
}
