//go:build linux

package sandbox

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// execWithCgroup opens the cgroup directory as an FD and attaches it to the
// SysProcAttr for CLONE_INTO_CGROUP.
//
// clone3(CLONE_INTO_CGROUP) is the atomic primitive that places the child PID
// in the target cgroup before exec — it eliminates the race where a child runs
// unconfined briefly. SysProcAttr.UseCgroupFD is the Go 1.22+ wrapper that
// calls clone3 with the FD. The fd must be opened with O_DIRECTORY (kernel
// requirement) and stays valid until cmd.Start() returns; we close it in
// cg.cleanup() after Start, so the fd lifetime is:
//
//	open() → execWithCgroup() → cmd.Start() → defer cg.cleanup() → close()
func execWithCgroup(cmd *exec.Cmd, cg *cgroupExec) error {
	// O_CLOEXEC keeps the fd out of exec'd child processes;
	// clone3 reads it in the parent before the child exec()s.
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
