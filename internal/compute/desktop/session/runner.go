package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
)

type systemRunner struct{}
type childProcess struct {
	cmd      *exec.Cmd
	done     chan struct{}
	stopOnce sync.Once
	stopErr  error
}

// PID returns the process identifier.
func (p *childProcess) PID() int {
	return p.cmd.Process.Pid
}

// Done returns a channel closed when the process exits.
func (p *childProcess) Done() <-chan struct{} {
	return p.done
}

// Stop stops the process.
func (p *childProcess) Stop() error {
	p.stopOnce.Do(func() {
		err := signalGroup(p.cmd, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if err != nil {
			p.stopErr = err
			return
		}
		// Do not return merely because the direct parent exited: descendants in its
		// group may still be alive. Always apply the bounded group cleanup.
		timer := time.NewTimer(250 * time.Millisecond)
		<-timer.C
		err = signalGroup(p.cmd, syscall.SIGKILL)
		if err != nil && !errors.Is(err, syscall.ESRCH) {
			p.stopErr = err
		}
		select {
		case <-p.done:
		case <-time.After(time.Second):
			p.stopErr = errors.Join(p.stopErr, errors.New("owned desktop process did not exit"))
		}
	})
	return p.stopErr
}

// Start starts the requested process or operation.
func (systemRunner) Start(ctx context.Context, c Command) (Process, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("desktop session runtime requires linux")
	}
	return startOwned(ctx, c)
}
func startOwned(ctx context.Context, c Command) (Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(c.Argv) == 0 {
		return nil, errors.New("desktop command is empty")
	}
	cmd := exec.Command(c.Argv[0], c.Argv[1:]...)
	cmd.Env = append([]string{}, c.Env...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = time.Second
	setProcessGroup(cmd)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &childProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = p.Stop()
		case <-p.done:
		}
	}()
	return p, nil
}

// Probe checks whether the requested component is ready.
func (systemRunner) Probe(ctx context.Context, c Command) error {
	if len(c.Argv) == 0 {
		return errors.New("desktop probe is empty")
	}
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	cmd.Env = append([]string{}, c.Env...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 100 * time.Millisecond
	setProcessGroup(cmd)
	cmd.Cancel = func() error {
		err := signalGroup(cmd, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("desktop readiness probe failed: %w", err)
	}
	return nil
}
