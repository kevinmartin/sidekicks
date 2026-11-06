package apps

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

type fakeWindows struct {
	mu          sync.Mutex
	windows     []Window
	active      []uint32
	listErr     error
	activateErr error
}

// List returns the available windows.
func (f *fakeWindows) List(context.Context) ([]Window, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Window(nil), f.windows...), f.listErr
}

// Activate activates the specified window.
func (f *fakeWindows) Activate(_ context.Context, id uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active = append(f.active, id)
	return f.activateErr
}
func (f *fakeWindows) set(w ...Window) {
	f.mu.Lock()
	f.windows = append([]Window(nil), w...)
	f.mu.Unlock()
}

type fakeStarter struct {
	mu       sync.Mutex
	commands []Command
	start    func(Command) (Process, error)
}

// Start records the command and returns the configured or default fake process.
func (f *fakeStarter) Start(c Command) (Process, error) {
	f.mu.Lock()
	f.commands = append(f.commands, c)
	f.mu.Unlock()
	if f.start != nil {
		return f.start(c)
	}
	return Process{PID: os.Getpid(), Done: make(chan error)}, nil
}
func (f *fakeStarter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.commands)
}
func normal(app App, id uint32) Window {
	w := Window{ID: id, Normal: true, Mapped: true, Width: 1120, Height: 548}
	switch app {
	case Browser:
		w.Class = "SidekicksBrowser"
	case Terminal:
		w.Class = "Xfce4-terminal"
		w.Role = "SidekicksTerminal"
	case Files:
		w.Class = "Thunar"
	}
	return w
}
func config(t *testing.T, b *fakeWindows, s *fakeStarter) Config {
	t.Helper()
	return Config{Browser: Chromium, HomeURL: "http://127.0.0.1:8080/home", HomeDir: t.TempDir(), Backend: b, Starter: s, Environment: []string{"DISPLAY=:99", "DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/sidekicks-session-bus", "SIDEKICKS_TOKEN=private", "PATH=/malicious", "LD_PRELOAD=/bad", "HTTP_PROXY=http://private"}, LaunchTimeout: 30 * time.Millisecond, PollInterval: time.Millisecond}
}
func launcher(t *testing.T, c Config) *Launcher {
	t.Helper()
	l, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	return l
}
func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	until := time.Now().Add(time.Second)
	for !fn() {
		if time.Now().After(until) {
			t.Fatal("condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestMarkerFailureRetainsCrossLauncherCoordination verifies an unrecorded child cannot cause duplicate launches.
func TestMarkerFailureRetainsCrossLauncherCoordination(t *testing.T) {
	windows := &fakeWindows{}
	starter := &fakeStarter{}
	cfg := config(t, windows, starter)
	first := launcher(t, cfg)
	second := launcher(t, cfg)
	starter.start = func(Command) (Process, error) {
		if err := os.MkdirAll(first.markerPath(Terminal), 0700); err != nil {
			return Process{}, err
		}
		return Process{PID: os.Getpid(), Done: make(chan error)}, nil
	}
	if _, err := first.Launch(context.Background(), Terminal); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("marker failure was not reported: %v", err)
	}
	result, err := second.Launch(context.Background(), Terminal)
	if (err != nil && !errors.Is(err, ErrUnavailable)) || (err == nil && result.State != "launching") || starter.count() != 1 {
		t.Fatalf("unrecorded child permitted a duplicate: %+v %v", result, err)
	}
	windows.set(normal(Terminal, 42))
	eventually(t, func() bool {
		result, err = second.Launch(context.Background(), Terminal)
		return err == nil && result.State == "restored"
	})
	if starter.count() != 1 {
		t.Fatal("duplicate launched after the window appeared")
	}
}

// TestStaleMarkerDoesNotTrustReusedPID verifies launch coordination checks process identity.
func TestStaleMarkerDoesNotTrustReusedPID(t *testing.T) {
	windows := &fakeWindows{}
	starter := &fakeStarter{}
	l := launcher(t, config(t, windows, starter))
	if err := os.MkdirAll(l.lockDir, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(marker{PID: os.Getpid(), Started: time.Now().Add(-time.Hour), Identity: "different-start-identity"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.markerPath(Terminal), data, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := l.Launch(context.Background(), Terminal)
	if err != nil || result.State != "launching" || starter.count() != 1 {
		t.Fatalf("stale marker suppressed the new launch: %+v %v", result, err)
	}
}

// TestRestoreSameWindowAndCloseStaysClosedUntilExplicitLaunch verifies restore same window and close stays closed until explicit launch.
func TestRestoreSameWindowAndCloseStaysClosedUntilExplicitLaunch(t *testing.T) {
	for _, app := range []App{Browser, Terminal, Files} {
		t.Run(string(app), func(t *testing.T) {
			b := &fakeWindows{}
			s := &fakeStarter{}
			c := config(t, b, s)
			l := launcher(t, c)
			w := normal(app, 20)
			b.set(w)
			for i := 0; i < 2; i++ {
				r, e := l.Launch(context.Background(), app)
				if e != nil || r.State != "restored" || r.Window != 20 {
					t.Fatal(r, e)
				}
			}
			if s.count() != 0 || len(b.active) != 2 {
				t.Fatal("restore spawned or failed to activate")
			}
			b.set()
			time.Sleep(5 * time.Millisecond)
			if s.count() != 0 {
				t.Fatal("close automatically reopened app")
			}
			s.start = func(Command) (Process, error) {
				b.set(normal(app, 21))
				return Process{PID: os.Getpid(), Done: make(chan error)}, nil
			}
			r, e := l.Launch(context.Background(), app)
			if e != nil || r.State != "opened" || r.Window != 21 || s.count() != 1 {
				t.Fatal(r, e, s.count())
			}
			if w.Width != 1120 || w.Height != 548 {
				t.Fatal("geometry changed")
			}
		})
	}
}

// TestMatchingRejectsUtilityGenericAndTinyWindows verifies matching rejects utility generic and tiny windows.
func TestMatchingRejectsUtilityGenericAndTinyWindows(t *testing.T) {
	tests := []struct {
		app  App
		w    Window
		want bool
	}{
		{Browser, normal(Browser, 1), true}, {Terminal, normal(Terminal, 1), true}, {Files, normal(Files, 1), true},
		{Terminal, Window{ID: 1, Class: "SidekicksTerminal", Normal: true, Width: 200, Height: 100}, true},
		{Browser, Window{ID: 1, Instance: "sidekicksbrowser", Normal: true, Width: 200, Height: 100}, true},
		{Terminal, Window{ID: 1, Class: "Xfce4-terminal", Normal: true, Width: 200, Height: 100}, false},
		{Browser, Window{ID: 1, Class: "Chromium", Normal: true, Width: 200, Height: 100}, false},
		{Files, Window{ID: 1, Class: "Thunar", Normal: false, Width: 200, Height: 100}, false},
		{Files, Window{ID: 1, Class: "Thunar", Normal: true, Width: 100, Height: 80}, false},
		{Files, Window{Class: "Thunar", Normal: true, Width: 200, Height: 100}, false}, {"other", normal(Files, 1), false},
	}
	for _, tt := range tests {
		if got := matches(tt.app, tt.w); got != tt.want {
			t.Fatalf("%s %+v = %v", tt.app, tt.w, got)
		}
	}
	b := &fakeWindows{}
	first := normal(Browser, 1)
	last := normal(Browser, 2)
	b.set(first, last)
	l := launcher(t, config(t, b, &fakeStarter{}))
	r, e := l.Launch(context.Background(), Browser)
	if e != nil || r.Window != 2 {
		t.Fatal(r, e)
	}
}

// TestConcurrentAndCrossLauncherClicksNeverQueueDuplicateProcesses verifies concurrent and cross launcher clicks never queue duplicate processes.
func TestConcurrentAndCrossLauncherClicksNeverQueueDuplicateProcesses(t *testing.T) {
	b := &fakeWindows{}
	s := &fakeStarter{}
	c := config(t, b, s)
	l := launcher(t, c)
	other := launcher(t, c)
	entered := make(chan struct{})
	resume := make(chan struct{})
	s.start = func(Command) (Process, error) {
		close(entered)
		<-resume
		b.set(normal(Browser, 44))
		return Process{PID: os.Getpid(), Done: make(chan error)}, nil
	}
	done := make(chan error, 1)
	go func() {
		_, e := l.Launch(context.Background(), Browser)
		done <- e
	}()
	<-entered
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := l.Launch(context.Background(), Browser)
			if e != nil || r.State != "launching" {
				t.Error(r, e)
			}
		}()
	}
	r, e := other.Launch(context.Background(), Browser)
	if e != nil || r.State != "launching" {
		t.Fatal(r, e)
	}
	wg.Wait()
	close(resume)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if s.count() != 1 {
		t.Fatal("duplicate processes", s.count())
	}
}

// TestCancelledLaunchLeavesChildPendingRatherThanKillingOrDuplicating verifies cancelled launch leaves child pending rather than killing or duplicating.
func TestCancelledLaunchLeavesChildPendingRatherThanKillingOrDuplicating(t *testing.T) {
	b := &fakeWindows{}
	s := &fakeStarter{}
	l := launcher(t, config(t, b, s))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var started atomic.Bool
	s.start = func(Command) (Process, error) {
		started.Store(true)
		return Process{PID: os.Getpid(), Done: make(chan error)}, nil
	}
	result := make(chan error, 1)
	go func() {
		_, e := l.Launch(ctx, Browser)
		result <- e
	}()
	eventually(t, started.Load)
	cancel()
	if e := <-result; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	r, e := l.Launch(context.Background(), Browser)
	if e != nil || r.State != "launching" || s.count() != 1 {
		t.Fatal(r, e, s.count())
	}
	b.set(normal(Browser, 13))
	r, e = l.Launch(context.Background(), Browser)
	if e != nil || r.State != "restored" || r.Window != 13 || s.count() != 1 {
		t.Fatal(r, e)
	}
}

// TestProcessFailureDelegationAndPendingMarkerBehavior verifies process failure delegation and pending marker behavior.
func TestProcessFailureDelegationAndPendingMarkerBehavior(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		b := &fakeWindows{}
		s := &fakeStarter{}
		l := launcher(t, config(t, b, s))
		done := make(chan error, 1)
		done <- errors.New("exited")
		s.start = func(Command) (Process, error) {
			return Process{PID: os.Getpid(), Done: done}, nil
		}
		if _, e := l.Launch(context.Background(), Terminal); !errors.Is(e, ErrUnavailable) {
			t.Fatal(e)
		}
		if _, e := os.Stat(l.markerPath(Terminal)); !errors.Is(e, os.ErrNotExist) {
			t.Fatal(e)
		}
	})
	t.Run("dbus delegate", func(t *testing.T) {
		b := &fakeWindows{}
		s := &fakeStarter{}
		l := launcher(t, config(t, b, s))
		s.start = func(Command) (Process, error) {
			done := make(chan error, 1)
			done <- nil
			go func() {
				time.Sleep(3 * time.Millisecond)
				b.set(normal(Files, 16))
			}()
			return Process{PID: os.Getpid(), Done: done}, nil
		}
		r, e := l.Launch(context.Background(), Files)
		if e != nil || r.State != "opened" || s.count() != 1 {
			t.Fatal(r, e)
		}
	})
	t.Run("timeout no duplicate", func(t *testing.T) {
		b := &fakeWindows{}
		s := &fakeStarter{}
		c := config(t, b, s)
		l := launcher(t, c)
		r, e := l.Launch(context.Background(), Files)
		if e != nil || r.State != "launching" {
			t.Fatal(r, e)
		}
		r, e = l.Launch(context.Background(), Files)
		if e != nil || r.State != "launching" || s.count() != 1 {
			t.Fatal(r, e)
		}
		other := launcher(t, c)
		r, e = other.Launch(context.Background(), Files)
		if e != nil || r.State != "launching" || s.count() != 1 {
			t.Fatal(r, e)
		}
	})
	t.Run("invalid marker", func(t *testing.T) {
		b := &fakeWindows{}
		s := &fakeStarter{}
		l := launcher(t, config(t, b, s))
		if e := os.MkdirAll(l.lockDir, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(l.markerPath(Files), []byte(`{"pid":0}`), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := l.Launch(context.Background(), Files); !errors.Is(e, ErrUnavailable) || s.count() != 0 {
			t.Fatal(e)
		}
	})
}

// TestFixedCommandsSanitizeEnvironmentAndPreserveManagedProfileData verifies fixed commands sanitize environment and preserve managed profile data.
func TestFixedCommandsSanitizeEnvironmentAndPreserveManagedProfileData(t *testing.T) {
	b := &fakeWindows{}
	s := &fakeStarter{}
	c := config(t, b, s)
	l := launcher(t, c)
	if e := os.MkdirAll(filepath.Join(c.HomeDir, ".config", "sidekicks", "chromium", "Default"), 0700); e != nil {
		t.Fatal(e)
	}
	name := filepath.Join(c.HomeDir, ".config", "sidekicks", "chromium", "Default", "Preferences")
	if e := os.WriteFile(name, []byte(`{"custom":"keep","browser":{"other":true},"session":{"other":7}}`), 0600); e != nil {
		t.Fatal(e)
	}
	command, e := l.command(Browser)
	if e != nil {
		t.Fatal(e)
	}
	if command.Path != "/usr/bin/chromium" || strings.Join(command.Args, " ") == "" || command.Args[len(command.Args)-1] != c.HomeURL {
		t.Fatal(command)
	}
	for _, v := range command.Env {
		if strings.Contains(v, "private") || strings.HasPrefix(v, "LD_") || strings.HasPrefix(v, "SIDEKICKS_") || strings.Contains(v, "/malicious") {
			t.Fatal("private/unsafe env propagated", v)
		}
	}
	if !strings.Contains(strings.Join(command.Env, "\n"), "DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/sidekicks-session-bus") {
		t.Fatal(command.Env)
	}
	if strings.Contains(strings.Join(command.Args, " "), "--no-sandbox") {
		t.Fatal("sandbox silently disabled")
	}
	data, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	var prefs map[string]any
	if e = json.Unmarshal(data, &prefs); e != nil {
		t.Fatal(e)
	}
	if prefs["custom"] != "keep" || prefs["homepage"] != c.HomeURL || prefs["browser"].(map[string]any)["custom_chrome_frame"] != true {
		t.Fatal(prefs)
	}
	script, e := os.ReadFile(filepath.Join(c.HomeDir, ".config", "sidekicks", "newtab", "newtab.js"))
	if e != nil || !strings.Contains(string(script), c.HomeURL) {
		t.Fatal(string(script), e)
	}
	before := string(data)
	b.set(normal(Browser, 1))
	if _, e = l.Launch(context.Background(), Browser); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(name)
	if string(after) != before {
		t.Fatal("restore rewrote live profile")
	}
	terminal, e := l.command(Terminal)
	if e != nil {
		t.Fatal(e)
	}
	joined := strings.Join(terminal.Args, " ")
	if terminal.Path != "/usr/bin/xfce4-terminal" || !strings.Contains(joined, "--role=SidekicksTerminal") || strings.Contains(joined, "--class") || !strings.Contains(joined, "--color-bg=#10263d") {
		t.Fatal(terminal)
	}
	files, e := l.command(Files)
	if e != nil || files.Path != "/usr/bin/thunar" || len(files.Args) != 1 || files.Args[0] != c.HomeDir {
		t.Fatal(files, e)
	}
	c.Browser = Firefox
	l = launcher(t, c)
	firefox, e := l.command(Browser)
	if e != nil || firefox.Path != "/usr/bin/firefox-esr" || firefox.Args[len(firefox.Args)-1] != c.HomeURL {
		t.Fatal(firefox, e)
	}
	c.ChromiumNoSandbox = true
	c.Browser = Chromium
	l = launcher(t, c)
	legacy, e := l.command(Browser)
	if e != nil || legacy.Args[0] != "--no-sandbox" {
		t.Fatal(legacy, e)
	}
}

// TestInvalidConfigurationAndProfileFailClosed verifies invalid configuration and profile fail closed.
func TestInvalidConfigurationAndProfileFailClosed(t *testing.T) {
	base := config(t, &fakeWindows{}, &fakeStarter{})
	tests := map[string]func(*Config){"browser": func(c *Config) {
		c.Browser = "other"
	}, "home remote": func(c *Config) {
		c.HomeURL = "http://example.com/home"
	}, "home path": func(c *Config) {
		c.HomeURL = "http://127.0.0.1:8080/fixture"
	}, "home credentials": func(c *Config) {
		c.HomeURL = "http://user@127.0.0.1/home"
	}, "home query": func(c *Config) {
		c.HomeURL = "http://127.0.0.1/home?x"
	}, "home escaped": func(c *Config) {
		c.HomeURL = "http://127.0.0.1/%68ome"
	}, "home badport": func(c *Config) {
		c.HomeURL = "http://127.0.0.1:99999/home"
	}, "home root": func(c *Config) {
		c.HomeDir = "/"
	}, "home relative": func(c *Config) {
		c.HomeDir = "relative"
	}, "outside profile": func(c *Config) {
		c.ChromiumProfile = "/tmp/outside"
	}, "backend": func(c *Config) {
		c.Backend = nil
	}, "display": func(c *Config) {
		c.Environment = []string{"DISPLAY=host:0"}
	}, "bus": func(c *Config) {
		c.Environment = []string{"DISPLAY=:99", "DBUS_SESSION_BUS_ADDRESS=tcp:host=example.com"}
	}, "timeout": func(c *Config) {
		c.LaunchTimeout = time.Hour
	}, "poll": func(c *Config) {
		c.PollInterval = time.Hour
	}}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			c := base
			change(&c)
			if _, e := New(c); !errors.Is(e, ErrInvalid) {
				t.Fatal(e)
			}
		})
	}
	l := launcher(t, base)
	if _, e := l.Launch(context.Background(), "arbitrary"); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := l.Launch(ctx, Browser); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	dir := filepath.Join(l.cfg.ChromiumProfile, "Default")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	for _, data := range []string{`null`, `{"browser":false}`, `{"session":[]}`, `not json`} {
		if e := os.WriteFile(filepath.Join(dir, "Preferences"), []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := l.command(Browser); !errors.Is(e, ErrUnavailable) {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(dir, "Preferences"), make([]byte, (4<<20)+1), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := l.command(Browser); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
}

// TestHandlerAndState verifies handler and state.
func TestHandlerAndState(t *testing.T) {
	b := &fakeWindows{}
	b.set(normal(Browser, 1))
	s := &fakeStarter{}
	l := launcher(t, config(t, b, s))
	h := l.Handler()
	for _, tt := range []struct {
		method, body, content string
		status                int
	}{{"GET", "", "", 405}, {"POST", `{"app":"browser"}`, "text/plain", 415}, {"POST", `{"app":"browser","command":"sh"}`, "application/json", 400}, {"POST", `{"app":"browser"} {}`, "application/json", 400}, {"POST", `{"app":"bad"}`, "application/json", 400}, {"POST", strings.Repeat("x", 1025), "application/json", 400}, {"POST", `{"app":"browser"}`, "application/json", 200}} {
		r := httptest.NewRequest(tt.method, "http://127.0.0.1/desktop/launch", strings.NewReader(tt.body))
		if tt.content != "" {
			r.Header.Set("Content-Type", tt.content)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.status {
			t.Fatal(tt, w.Code, w.Body.String())
		}
	}
	state, e := l.State(context.Background())
	if e != nil || !state.Ready || state.Windows[Browser] != 1 {
		t.Fatal(state, e)
	}
	b.set()
	state, e = l.State(context.Background())
	if e != nil || !state.Ready || len(state.Windows) != 0 {
		t.Fatal(state, e)
	}
	b.mu.Lock()
	b.listErr = errors.New("gone")
	b.mu.Unlock()
	if _, e = l.State(context.Background()); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"app":"browser"}`))
	r.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal(w.Code)
	}
}

// TestEWMHDecodingAndLocalDisplayValidation verifies ewmh decoding and local display validation.
func TestEWMHDecodingAndLocalDisplayValidation(t *testing.T) {
	if _, e := NewX11("host:0", ""); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if _, e := NewX11(":99.0", ""); e != nil {
		t.Fatal(e)
	}
	value := make([]byte, 8)
	xgb.Put32(value[0:], 42)
	xgb.Put32(value[4:], 88)
	list, e := decodeClients(&xproto.GetPropertyReply{Format: 32, Type: xproto.AtomWindow, Value: value})
	if e != nil || len(list) != 2 || list[0] != 42 || list[1] != 88 {
		t.Fatal(list, e)
	}
	for _, p := range []*xproto.GetPropertyReply{nil, {Format: 8, Type: xproto.AtomWindow, Value: value}, {Format: 32, Type: xproto.AtomString, Value: value}, {Format: 32, Type: xproto.AtomWindow, Value: []byte{1}}} {
		if _, e := decodeClients(p); !errors.Is(e, ErrUnavailable) {
			t.Fatal(e)
		}
	}
	instance, class, ok := classNames([]byte("browser\x00SidekicksBrowser\x00"))
	if !ok || instance != "browser" || class != "SidekicksBrowser" {
		t.Fatal(instance, class, ok)
	}
	for _, data := range [][]byte{nil, []byte("no-null"), []byte("a\x00b\x00extra"), make([]byte, 4097)} {
		if _, _, ok := classNames(data); ok {
			t.Fatal("malformed class accepted")
		}
	}
	normalAtom := xproto.Atom(100)
	normalValue := make([]byte, 4)
	xgb.Put32(normalValue, 100)
	if !normalType(&xproto.GetPropertyReply{}, normalAtom) || !normalType(&xproto.GetPropertyReply{Format: 32, Type: xproto.AtomAtom, Value: normalValue}, normalAtom) {
		t.Fatal("normal rejected")
	}
	xgb.Put32(normalValue, 101)
	if normalType(&xproto.GetPropertyReply{Format: 32, Type: xproto.AtomAtom, Value: normalValue}, normalAtom) || normalType(nil, normalAtom) {
		t.Fatal("utility accepted")
	}
}

// TestLaunchErrorsDoNotExecuteFallbackCommands verifies launch errors do not execute fallback commands.
func TestLaunchErrorsDoNotExecuteFallbackCommands(t *testing.T) {
	t.Run("enumeration", func(t *testing.T) {
		b := &fakeWindows{listErr: errors.New("no wm")}
		s := &fakeStarter{}
		l := launcher(t, config(t, b, s))
		if _, e := l.Launch(context.Background(), Files); !errors.Is(e, ErrUnavailable) || s.count() != 0 {
			t.Fatal(e)
		}
	})
	t.Run("activation", func(t *testing.T) {
		b := &fakeWindows{activateErr: errors.New("focus rejected")}
		b.set(normal(Files, 1))
		s := &fakeStarter{}
		l := launcher(t, config(t, b, s))
		if _, e := l.Launch(context.Background(), Files); !errors.Is(e, ErrUnavailable) || s.count() != 0 {
			t.Fatal(e)
		}
	})
	t.Run("process start", func(t *testing.T) {
		b := &fakeWindows{}
		s := &fakeStarter{start: func(Command) (Process, error) {
			return Process{}, errors.New("missing app")
		}}
		l := launcher(t, config(t, b, s))
		if _, e := l.Launch(context.Background(), Terminal); !errors.Is(e, ErrUnavailable) || s.count() != 1 {
			t.Fatal(e)
		}
	})
	t.Run("lock symlink", func(t *testing.T) {
		b := &fakeWindows{}
		s := &fakeStarter{}
		l := launcher(t, config(t, b, s))
		if e := os.MkdirAll(l.lockDir, 0700); e != nil {
			t.Fatal(e)
		}
		target := filepath.Join(t.TempDir(), "target")
		if e := os.WriteFile(target, []byte("untouched"), 0600); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink(target, filepath.Join(l.lockDir, "files.lock")); e != nil {
			t.Fatal(e)
		}
		if _, e := l.Launch(context.Background(), Files); !errors.Is(e, ErrUnavailable) || s.count() != 0 {
			t.Fatal(e)
		}
		data, _ := os.ReadFile(target)
		if string(data) != "untouched" {
			t.Fatal("symlink target modified")
		}
	})
	t.Run("stale marker", func(t *testing.T) {
		b := &fakeWindows{}
		s := &fakeStarter{}
		l := launcher(t, config(t, b, s))
		if e := os.MkdirAll(l.lockDir, 0700); e != nil {
			t.Fatal(e)
		}
		data, _ := json.Marshal(marker{PID: 99999999, Started: time.Now().Add(-time.Hour)})
		if e := os.WriteFile(l.markerPath(Files), data, 0600); e != nil {
			t.Fatal(e)
		}
		s.start = func(Command) (Process, error) {
			b.set(normal(Files, 12))
			return Process{PID: os.Getpid(), Done: make(chan error)}, nil
		}
		r, e := l.Launch(context.Background(), Files)
		if e != nil || r.State != "opened" || s.count() != 1 {
			t.Fatal(r, e)
		}
	})
	t.Run("oversized marker", func(t *testing.T) {
		b := &fakeWindows{}
		s := &fakeStarter{}
		l := launcher(t, config(t, b, s))
		if e := os.MkdirAll(l.lockDir, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(l.markerPath(Files), make([]byte, 1025), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := l.Launch(context.Background(), Files); !errors.Is(e, ErrUnavailable) || s.count() != 0 {
			t.Fatal(e)
		}
	})
}

// TestStarterReapsShortFixedProcessWithoutRequestLifetime verifies starter reaps short fixed process without request lifetime.
func TestStarterReapsShortFixedProcessWithoutRequestLifetime(t *testing.T) {
	if _, e := os.Stat("/usr/bin/true"); e != nil {
		t.Skip("fixed true executable unavailable")
	}
	p, e := (ExecStarter{}).Start(Command{Path: "/usr/bin/true", Env: []string{"PATH=/usr/bin:/bin"}, Dir: t.TempDir()})
	if e != nil || p.PID < 1 {
		t.Fatal(p, e)
	}
	select {
	case e = <-p.Done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("child was not reaped")
	}
	if processAlive(99999999) || processAlive(0) || !processAlive(os.Getpid()) {
		t.Fatal("process liveness query")
	}
	if _, e := (ExecStarter{}).Start(Command{Path: "/does/not/exist", Dir: t.TempDir()}); e == nil {
		t.Fatal("missing executable accepted")
	}
}

// TestEnvironmentAndBackendConfigurationDefaults verifies environment and backend configuration defaults.
func TestEnvironmentAndBackendConfigurationDefaults(t *testing.T) {
	b := &fakeWindows{}
	c := config(t, b, &fakeStarter{})
	c.Browser = ""
	c.Starter = nil
	c.LaunchTimeout = 0
	c.PollInterval = 0
	c.Environment = []string{"ignored", "DISPLAY=:1.0"}
	l := launcher(t, c)
	if l.cfg.Browser != Chromium || l.cfg.LaunchTimeout != 3*time.Second || l.cfg.PollInterval != 50*time.Millisecond {
		t.Fatal(l.cfg)
	}
	for _, input := range [][]string{{"DISPLAY=:0\x00"}, {"DISPLAY=:0", "DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/bus;tcp:host=remote"}, {"DISPLAY=:0", "DBUS_SESSION_BUS_ADDRESS=unix:path=relative"}} {
		if _, e := environment(c.HomeDir, input); !errors.Is(e, ErrInvalid) {
			t.Fatal(input, e)
		}
	}
	if _, e := l.command("bad"); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if e := (&X11{}).Activate(context.Background(), 0); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}

// TestWaitForActualMappedAppAndCloseReadiness verifies wait for actual mapped app and close readiness.
func TestWaitForActualMappedAppAndCloseReadiness(t *testing.T) {
	b := &fakeWindows{}
	s := &fakeStarter{}
	c := config(t, b, s)
	c.LaunchTimeout = 100 * time.Millisecond
	l := launcher(t, c)
	// EWMH may list a managed normal client before it is actually viewable.
	w := normal(Browser, 42)
	w.Mapped = false
	b.set(w)
	done := make(chan struct{})
	go func() {
		time.Sleep(10 * time.Millisecond)
		w.Mapped = true
		b.set(w)
		close(done)
	}()
	actual, e := l.WaitFor(context.Background(), Browser)
	<-done
	if e != nil || actual.ID != 42 || !actual.Mapped || s.count() != 0 || len(b.active) != 0 {
		t.Fatal(actual, e)
	}
	// This barrier must not turn into an auto-reopen supervisor or make a user's
	// subsequent app close invalidate a still-working Xfce/session query.
	b.set()
	state, e := l.State(context.Background())
	if e != nil || !state.Ready || len(state.Windows) != 0 || s.count() != 0 {
		t.Fatal(state, e)
	}
}

// TestWaitForLaunchingResultCannotBecomeFalseReady verifies wait for launching result cannot become false ready.
func TestWaitForLaunchingResultCannotBecomeFalseReady(t *testing.T) {
	b := &fakeWindows{}
	s := &fakeStarter{}
	l := launcher(t, config(t, b, s))
	result, e := l.Launch(context.Background(), Browser)
	if e != nil || result.State != "launching" {
		t.Fatal(result, e)
	}
	started := time.Now()
	actual, e := l.WaitFor(context.Background(), Browser)
	if !errors.Is(e, ErrUnavailable) || !errors.Is(e, context.DeadlineExceeded) || actual.ID != 0 || s.count() != 1 {
		t.Fatal(actual, e, s.count())
	}
	if elapsed := time.Since(started); elapsed < l.cfg.LaunchTimeout || elapsed > time.Second {
		t.Fatal("unbounded or premature wait", elapsed)
	}
}

// TestWaitForReportsKnownChildFailureWithoutRelaunch verifies wait for reports known child failure without relaunch.
func TestWaitForReportsKnownChildFailureWithoutRelaunch(t *testing.T) {
	b := &fakeWindows{}
	s := &fakeStarter{}
	done := make(chan error, 1)
	s.start = func(Command) (Process, error) {
		return Process{PID: os.Getpid(), Done: done}, nil
	}
	l := launcher(t, config(t, b, s))
	result, e := l.Launch(context.Background(), Browser)
	if e != nil || result.State != "launching" {
		t.Fatal(result, e)
	}
	done <- errors.New("pthread_create resource unavailable")
	actual, e := l.WaitFor(context.Background(), Browser)
	if !errors.Is(e, ErrUnavailable) || errors.Is(e, context.DeadlineExceeded) || actual.ID != 0 || s.count() != 1 || !strings.Contains(e.Error(), "exited before mapping") {
		t.Fatal(actual, e)
	}
	if strings.Contains(e.Error(), "pthread") {
		t.Fatal("backend diagnostic leaked")
	}
}

// TestWaitForCancellationAndInvalidApp verifies wait for cancellation and invalid app.
func TestWaitForCancellationAndInvalidApp(t *testing.T) {
	b := &fakeWindows{}
	s := &fakeStarter{}
	l := launcher(t, config(t, b, s))
	if _, e := l.WaitFor(context.Background(), "bad"); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := l.WaitFor(ctx, Browser); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	wrong := normal(Files, 12)
	b.set(wrong)
	if _, e := l.WaitFor(context.Background(), Browser); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	if s.count() != 0 || len(b.active) != 0 {
		t.Fatal("wait caused launch or activation")
	}
}

// TestWaitForRetriesTransientWindowManagerStartup verifies wait for retries transient window manager startup.
func TestWaitForRetriesTransientWindowManagerStartup(t *testing.T) {
	b := &fakeWindows{listErr: errors.New("not yet ready")}
	s := &fakeStarter{}
	c := config(t, b, s)
	c.LaunchTimeout = 100 * time.Millisecond
	l := launcher(t, c)
	done := make(chan struct{})
	go func() {
		time.Sleep(10 * time.Millisecond)
		b.mu.Lock()
		b.listErr = nil
		b.windows = []Window{normal(Terminal, 18)}
		b.mu.Unlock()
		close(done)
	}()
	w, e := l.WaitFor(context.Background(), Terminal)
	<-done
	if e != nil || w.ID != 18 || !w.Mapped || s.count() != 0 {
		t.Fatal(w, e)
	}
}
