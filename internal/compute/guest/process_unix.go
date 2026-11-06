//go:build linux || darwin

package guest

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
)

func configureProcess(cmd *exec.Cmd, dir *os.File) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.ExtraFiles = []*os.File{dir}
	cmd.Dir = "/proc/self/fd/" + strconv.Itoa(int(dir.Fd()))
	if runtime.GOOS == "darwin" {
		// Darwin is used for local tests only; production server mode is Linux-only.
		cmd.Dir = dir.Name()
	}
}
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
