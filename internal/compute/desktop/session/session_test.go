package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProcess struct {
	pid  int
	done chan struct{}
	once sync.Once
	stop func()
}

// PID returns the process identifier.
func (p *fakeProcess) PID() int {
	return p.pid
}

// Done returns a channel closed when the process exits.
func (p *fakeProcess) Done() <-chan struct{} {
	return p.done
}

// Stop stops the process.
func (p *fakeProcess) Stop() error {
	p.once.Do(func() {
		if p.stop != nil {
			p.stop()
		}
		close(p.done)
	})
	return nil
}

type fakeRunner struct {
	mu             sync.Mutex
	commands       []Command
	processes      []*fakeProcess
	stops          []string
	failStart      string
	failProbe      bool
	failComponents bool
}

// Start starts the requested process or operation.
func (r *fakeRunner) Start(_ context.Context, c Command) (Process, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, c)
	if c.Name == r.failStart {
		return nil, errors.New("start failed")
	}
	p := &fakeProcess{pid: 100 + len(r.processes), done: make(chan struct{})}
	p.stop = func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.stops = append(r.stops, c.Name)
	}
	r.processes = append(r.processes, p)
	return p, nil
}

// Probe checks whether the requested component is ready.
func (r *fakeRunner) Probe(_ context.Context, c Command) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, c)
	if r.failProbe {
		return errors.New("not ready")
	}
	return nil
}

// Components returns the desktop session components.
func (r *fakeRunner) Components(_ context.Context, _ int, _ []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failComponents {
		return errors.New("component disappeared")
	}
	return nil
}

// TestOwnedSessionOrderReadinessAndClose verifies owned session order readiness and close.
func TestOwnedSessionOrderReadinessAndClose(t *testing.T) {
	t.Setenv("SIDEKICKS_AGENT_TOKEN", "must-not-reach-desktop")
	cfg := testConfig(t)
	runner := &fakeRunner{}
	cfg.Runner = runner
	m, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, command := range runner.commands {
		names = append(names, command.Name)
		for _, entry := range command.Env {
			if strings.Contains(entry, "TOKEN") || strings.Contains(entry, "must-not-reach") {
				t.Fatal("private environment leaked")
			}
		}
	}
	if !reflect.DeepEqual(names, []string{"xvfb", "x-ready", "dbus", "bus-ready", "session"}) {
		t.Fatal(names)
	}
	if got := runner.commands[0].Argv; !reflect.DeepEqual(got, []string{"/usr/bin/Xvfb", ":99", "-screen", "0", "1280x720x24", "-ac", "-nolisten", "tcp"}) {
		t.Fatal(got)
	}
	if err = m.Ready(); err != nil {
		t.Fatal(err)
	}
	if len(m.PIDs()) != 3 {
		t.Fatal("missing owned pids")
	}
	env := m.Env()
	env[0] = "UNTRUSTED=1"
	if m.Env()[0] == env[0] {
		t.Fatal("environment alias")
	}
	runner.mu.Lock()
	runner.failComponents = true
	runner.mu.Unlock()
	if err = m.Ready(); err == nil {
		t.Fatal("stale readiness accepted")
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if err = m.Ready(); err == nil {
		t.Fatal("closed desktop remained ready")
	}
	if !reflect.DeepEqual(runner.stops, []string{"session", "dbus", "xvfb"}) {
		t.Fatal(runner.stops)
	}
}

// TestStartFailureAndCancellationCleanOwnedChildren verifies start failure and cancellation clean owned children.
func TestStartFailureAndCancellationCleanOwnedChildren(t *testing.T) {
	cfg := testConfig(t)
	runner := &fakeRunner{failStart: "dbus"}
	cfg.Runner = runner
	if _, err := Start(context.Background(), cfg); err == nil {
		t.Fatal("accepted failed startup")
	}
	if !reflect.DeepEqual(runner.stops, []string{"xvfb"}) {
		t.Fatal("partial start not cleaned", runner.stops)
	}
	cfg = testConfig(t)
	runner = &fakeRunner{failProbe: true}
	cfg.Runner = runner
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if _, err := Start(ctx, cfg); err == nil {
		t.Fatal("accepted unready X server")
	}
	if len(runner.stops) != 1 {
		t.Fatal("unready child leaked")
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := Start(ctx, testConfig(t)); err == nil {
		t.Fatal("accepted canceled start")
	}
}

// TestUnexpectedProcessExitRevokesWholeReadiness verifies unexpected process exit revokes whole readiness.
func TestUnexpectedProcessExitRevokesWholeReadiness(t *testing.T) {
	cfg := testConfig(t)
	runner := &fakeRunner{}
	cfg.Runner = runner
	m, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_ = runner.processes[1].Stop()
	deadline := time.Now().Add(time.Second)
	for {
		if m.Ready() != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("unexpected exit did not revoke readiness")
		}
		time.Sleep(time.Millisecond)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.stops) != 3 {
		t.Fatal("sibling desktop processes leaked")
	}
}

// TestWaitReadyStopsOnExitedChild verifies wait ready stops on exited child.
func TestWaitReadyStopsOnExitedChild(t *testing.T) {
	p := &fakeProcess{pid: 1, done: make(chan struct{})}
	close(p.done)
	if waitReady(context.Background(), p, func(context.Context) error {
		return nil
	}) == nil {
		t.Fatal("dead child accepted")
	}
}

// TestRealOwnedProcessStopAndProbe verifies real owned process stop and probe.
func TestRealOwnedProcessStopAndProbe(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Unix process groups")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	process, err := startOwned(ctx, Command{Name: "test-owned", Argv: []string{"/bin/sh", "-c", "sleep 30"}, Env: []string{"PATH=/usr/bin:/bin"}})
	if err != nil {
		t.Fatal(err)
	}
	if process.PID() < 1 {
		t.Fatal("missing pid")
	}
	if err = process.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-process.Done():
	case <-time.After(time.Second):
		t.Fatal("owned child alive")
	}
	runner := systemRunner{}
	if err = runner.Probe(context.Background(), Command{Argv: []string{"/usr/bin/true"}}); err != nil {
		t.Fatal(err)
	}
	if err = runner.Probe(context.Background(), Command{Argv: []string{"/usr/bin/false"}}); err == nil {
		t.Fatal("failed probe accepted")
	}
	short, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err = runner.Probe(short, Command{Argv: []string{"/bin/sh", "-c", "sleep 30"}, Env: []string{"PATH=/usr/bin:/bin"}}); err == nil {
		t.Fatal("probe cancellation ignored")
	}
	if _, err = startOwned(context.Background(), Command{}); err == nil {
		t.Fatal("empty command accepted")
	}
	if _, err = startOwned(context.Background(), Command{Argv: []string{"/missing-sidekicks-process"}}); err == nil {
		t.Fatal("missing command accepted")
	}
	if err = runner.Probe(context.Background(), Command{}); err == nil {
		t.Fatal("empty probe accepted")
	}
	if runtime.GOOS != "linux" {
		if _, err = runner.Start(context.Background(), Command{Argv: []string{"/bin/true"}}); err == nil {
			t.Fatal("Darwin desktop runtime claimed")
		}
	}
	command := exec.Command("/bin/true")
	if err = signalGroup(command, 0); err == nil {
		t.Fatal("unstarted process accepted")
	}
}

// TestProcessTreeUsesActualOwnedLiveComponents verifies process tree uses actual owned live components.
func TestProcessTreeUsesActualOwnedLiveComponents(t *testing.T) {
	states := map[int]processState{100: {pid: 100, name: "xfce4-session", uid: 1000, state: "S"}, 101: {pid: 101, ppid: 100, name: "xfwm4", uid: 1000, state: "S"}, 102: {pid: 102, ppid: 100, name: "xfsettingsd", uid: 1000, state: "S"}, 103: {pid: 103, ppid: 100, name: "xfce4-panel", uid: 1000, state: "S"}, 104: {pid: 104, ppid: 100, name: "xfdesktop", uid: 1000, state: "S"}}
	if err := checkComponents(states, 100, 1000, requiredComponents); err != nil {
		t.Fatal(err)
	}
	states[105] = processState{pid: 105, ppid: 100, name: "xfwm4", uid: 1000, state: "S"}
	if checkComponents(states, 100, 1000, requiredComponents) == nil {
		t.Fatal("duplicate compositor accepted")
	}
	delete(states, 105)
	state := states[104]
	state.state = "Z"
	states[104] = state
	if checkComponents(states, 100, 1000, requiredComponents) == nil {
		t.Fatal("zombie component accepted")
	}
	state.state = "S"
	state.ppid = 1
	states[104] = state
	if checkComponents(states, 100, 1000, requiredComponents) == nil {
		t.Fatal("foreign component accepted")
	}
	delete(states, 100)
	if checkComponents(states, 100, 1000, requiredComponents) == nil {
		t.Fatal("missing session accepted")
	}
}

// TestProcessObservationParserAndFilesystem verifies process observation parser and filesystem.
func TestProcessObservationParserAndFilesystem(t *testing.T) {
	raw := "Name:\txfce4-session\nState:\tS (sleeping)\nPPid:\t1\nUid:\t1000 1000 1000 1000\n"
	state, err := parseProcessState(100, raw)
	if err != nil || state.name != "xfce4-session" {
		t.Fatal(state, err)
	}
	if _, err = parseProcessState(100, "Name: only"); err == nil {
		t.Fatal("partial status accepted")
	}
	root := t.TempDir()
	states := map[int]string{100: raw, 101: "Name: xfwm4\nState: S\nPPid: 100\nUid: 1000\n"}
	for pid, status := range states {
		dir := filepath.Join(root, strconv.Itoa(pid))
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = observeComponents(context.Background(), root, 100, 1000, []string{"xfwm4"}); err != nil {
		t.Fatal(err)
	}
	if err = observeComponents(context.Background(), "/missing-proc", 100, 1000, nil); err == nil {
		t.Fatal("missing proc accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = observeComponents(ctx, root, 100, 1000, nil); err == nil {
		t.Fatal("canceled observation accepted")
	}
}
