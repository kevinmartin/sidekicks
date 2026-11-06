//go:build linux || darwin

package session

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
func signalGroup(cmd *exec.Cmd, signal syscall.Signal) error {
	if cmd.Process == nil {
		return syscall.ESRCH
	}
	return syscall.Kill(-cmd.Process.Pid, signal)
}
