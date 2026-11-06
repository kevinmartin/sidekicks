//go:build linux || darwin

package apps

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

var errBusy = errors.New("application launch already in progress")

// ExecStarter starts a process using an operating-system command.
type ExecStarter struct{}

// Start starts the requested process or operation.
func (ExecStarter) Start(c Command) (Process, error) {
	command := exec.Command(c.Path, c.Args...)
	command.Env = c.Env
	command.Dir = c.Dir
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return Process{}, err
	}
	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
		close(done)
	}()
	return Process{PID: command.Process.Pid, Done: done}, nil
}
func lockFile(name string) (func(), error) {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errBusy
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
func processAlive(pid int) bool {
	if pid < 1 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
