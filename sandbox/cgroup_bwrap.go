//go:build linux

package sandbox

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// execWithCgroup opens the cgroup directory as an FD and attaches it to the
// SysProcAttr for CLONE_INTO_CGROUP. The fd stays alive as part of cg.fd
// (kept by the caller's defer of cg.cleanup()) until after cmd.Start().
func execWithCgroup(cmd *exec.Cmd, cg *cgroupExec) error {
	// O_CLOEXEC ensures the fd is not inherited by other execs.
	fd, err := unix.Open(cg.path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
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
