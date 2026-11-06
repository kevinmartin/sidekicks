package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Command describes a process invocation.
type Command struct {
	Name string
	Argv []string
	Env  []string
}

// Process represents a started process.
type Process interface {
	PID() int
	Done() <-chan struct{}
	Stop() error
}

// Runner starts and probes desktop session components.
type Runner interface {
	Start(context.Context, Command) (Process, error)
	Probe(context.Context, Command) error
	Components(context.Context, int, []string) error
}

// Manager owns the desktop session lifecycle.
type Manager struct {
	config    Config
	runner    Runner
	env       []string
	ctx       context.Context
	cancel    context.CancelFunc
	processes []ownedProcess
	closeOnce sync.Once
	closeErr  error
}
type ownedProcess struct {
	name    string
	process Process
}

// Start renders a new private desktop home and starts one X server, one session
// bus and one Xfce session, in that order. It refuses an already populated home.
// No globally named process is killed and no inherited bus/token is passed on.
func Start(ctx context.Context, config Config) (*Manager, error) {
	cfg, err := defaults(config)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err = Generate(cfg); err != nil {
		return nil, err
	}
	runner := cfg.Runner
	if runner == nil {
		runner = systemRunner{}
	}
	ctx, cancel := context.WithCancel(ctx)
	manager := &Manager{config: cfg, runner: runner, ctx: ctx, cancel: cancel}
	manager.env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + cfg.Home, "USER=sidekicks", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "DISPLAY=" + cfg.Display, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + cfg.Home + "/.run/session-bus", "XDG_CURRENT_DESKTOP=XFCE", "XDG_SESSION_DESKTOP=xfce", "DESKTOP_SESSION=xfce", "XDG_SESSION_TYPE=x11", "XDG_CONFIG_HOME=" + cfg.Home + "/.config", "XDG_CONFIG_DIRS=" + cfg.Home + "/.config-system:/etc/xdg", "XDG_DATA_HOME=" + cfg.Home + "/.local/share", "XDG_RUNTIME_DIR=" + cfg.Home + "/.run"}
	startup, stop := context.WithTimeout(ctx, 20*time.Second)
	defer stop()
	for _, command := range []Command{
		{Name: "xvfb", Argv: []string{"/usr/bin/Xvfb", cfg.Display, "-screen", "0", "1280x720x24", "-ac", "-nolisten", "tcp"}},
		{Name: "dbus", Argv: []string{"/usr/bin/dbus-daemon", "--session", "--nofork", "--nopidfile", "--address=unix:path=" + cfg.Home + "/.run/session-bus"}},
		{Name: "session", Argv: []string{"/usr/bin/xfce4-session"}},
	} {
		command.Env = manager.Env()
		process, err := runner.Start(ctx, command)
		if err != nil {
			_ = manager.Close()
			return nil, fmt.Errorf("start desktop %s: %w", command.Name, err)
		}
		manager.processes = append(manager.processes, ownedProcess{command.Name, process})
		var ready func(context.Context) error
		switch command.Name {
		case "xvfb":
			ready = func(ctx context.Context) error {
				return runner.Probe(ctx, Command{Name: "x-ready", Argv: []string{"/usr/bin/xdpyinfo", "-display", cfg.Display}, Env: manager.Env()})
			}
		case "dbus":
			ready = func(ctx context.Context) error {
				return runner.Probe(ctx, Command{Name: "bus-ready", Argv: []string{"/usr/bin/dbus-send", "--session", "--type=method_call", "--print-reply", "--dest=org.freedesktop.DBus", "/", "org.freedesktop.DBus.ListNames"}, Env: manager.Env()})
			}
		case "session":
			ready = func(ctx context.Context) error {
				return runner.Components(ctx, process.PID(), requiredComponents)
			}
		}
		if err = waitReady(startup, process, ready); err != nil {
			_ = manager.Close()
			return nil, fmt.Errorf("desktop %s not ready: %w", command.Name, err)
		}
	}
	for _, owned := range manager.processes {
		go func(p Process) {
			select {
			case <-p.Done():
				_ = manager.Close()
			case <-manager.ctx.Done():
				_ = manager.Close()
			}
		}(owned.process)
	}
	return manager, nil
}

var requiredComponents = []string{"xfwm4", "xfsettingsd", "xfce4-panel", "xfdesktop"}

func waitReady(ctx context.Context, process Process, probe func(context.Context) error) error {
	for {
		select {
		case <-process.Done():
			return errors.New("owned process exited")
		default:
		}
		check, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		err := probe(check)
		cancel()
		if err == nil {
			return nil
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-process.Done():
			timer.Stop()
			return errors.New("owned process exited")
		case <-timer.C:
		}
	}
}

// Env returns only the explicit guest desktop environment. It intentionally
// excludes the agent token and all parent environment variables.
func (m *Manager) Env() []string {
	return append([]string(nil), m.env...)
}

// PIDs returns the current component process identifiers.
func (m *Manager) PIDs() map[string]int {
	result := make(map[string]int, len(m.processes))
	for _, p := range m.processes {
		result[p.name] = p.process.PID()
	}
	return result
}

// Ready rechecks current owned process liveness and the Xfce component process
// tree. It never treats a historical ready marker as present readiness.
func (m *Manager) Ready() error {
	if err := m.ctx.Err(); err != nil {
		return err
	}
	for _, owned := range m.processes {
		select {
		case <-owned.process.Done():
			return fmt.Errorf("desktop %s is stopped", owned.name)
		default:
		}
	}
	if len(m.processes) != 3 {
		return errors.New("desktop startup is incomplete")
	}
	check, cancel := context.WithTimeout(m.ctx, time.Second)
	defer cancel()
	return m.runner.Components(check, m.processes[2].process.PID(), requiredComponents)
}

// Close releases the receiver's resources.
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		m.cancel()
		for i := len(m.processes) - 1; i >= 0; i-- {
			m.closeErr = errors.Join(m.closeErr, m.processes[i].process.Stop())
		}
	})
	return m.closeErr
}
