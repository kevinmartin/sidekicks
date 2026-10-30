// Package apps provides fixed guest-desktop launch-or-restore behavior. It must
// run inside the workload, never against the user's host display or home.
package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// App identifies a supported desktop application.
type App string

const (
	// Browser identifies the browser application.
	Browser App = "browser"
	// Terminal identifies the terminal application.
	Terminal App = "terminal"
	// Files identifies the file manager application.
	Files App = "files"
)

// BrowserKind identifies the managed browser implementation.
type BrowserKind string

const (
	// Chromium identifies Chromium as the managed browser.
	Chromium BrowserKind = "chromium"
	// Firefox identifies Firefox as the managed browser.
	Firefox BrowserKind = "firefox"
)

var (
	// ErrInvalid indicates an invalid request.
	ErrInvalid = errors.New("invalid desktop application request")
	// ErrUnavailable indicates that the resource is unavailable.
	ErrUnavailable = errors.New("desktop application unavailable")
)

// Window describes a managed desktop window.
type Window struct {
	ID       uint32 `json:"id"`
	Instance string `json:"instance"`
	Class    string `json:"class"`
	Role     string `json:"role,omitempty"`
	Normal   bool   `json:"normal"`
	Mapped   bool   `json:"mapped"`
	Width    uint16 `json:"width"`
	Height   uint16 `json:"height"`
}

// Windows enumerates only EWMH managed clients; Activate requests focus/restore
// of that same client and must not remap utility windows or alter geometry.
type Windows interface {
	List(context.Context) ([]Window, error)
	Activate(context.Context, uint32) error
}

// Command describes a process invocation.
type Command struct {
	Path string
	Args []string
	Env  []string
	Dir  string
}

// Process represents a started process.
type Process struct {
	PID  int
	Done <-chan error
}

// Starter starts application processes.
type Starter interface {
	Start(Command) (Process, error)
}

// Config configures managed application launches and window discovery.
type Config struct {
	Browser         BrowserKind
	HomeURL         string
	HomeDir         string
	ChromiumProfile string
	FirefoxProfile  string
	Backend         Windows
	Starter         Starter
	Environment     []string
	LaunchTimeout   time.Duration
	PollInterval    time.Duration
	// ChromiumNoSandbox is explicit and defaults false. It is only for guest
	// images whose documented sandbox constraints require the legacy POC flag.
	ChromiumNoSandbox bool
}

// Result describes the result of an operation.
type Result struct {
	OK     bool   `json:"ok"`
	App    App    `json:"app"`
	State  string `json:"state"`
	Window uint32 `json:"window,omitempty"`
	PID    int    `json:"pid,omitempty"`
}
type appState struct {
	mu      sync.Mutex
	process *Process
	started time.Time
	exited  bool
	exitErr error
}

// Launcher launches and restores managed desktop applications.
type Launcher struct {
	cfg     Config
	env     []string
	states  map[App]*appState
	lockDir string
}

const pendingGrace = 15 * time.Second

type marker struct {
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
}

// New constructs a managed desktop application launcher.
func New(c Config) (*Launcher, error) {
	if c.Browser == "" {
		c.Browser = Chromium
	}
	if c.Browser != Chromium && c.Browser != Firefox {
		return nil, ErrInvalid
	}
	u, err := url.Parse(c.HomeURL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "/home" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return nil, fmt.Errorf("%w: home must be an exact guest-loopback /home url", ErrInvalid)
	}
	if u.Port() != "" {
		n, e := strconv.Atoi(u.Port())
		if e != nil || n < 1 || n > 65535 {
			return nil, ErrInvalid
		}
	}
	if c.HomeDir == "" || !filepath.IsAbs(c.HomeDir) || strings.ContainsRune(c.HomeDir, 0) || filepath.Clean(c.HomeDir) != c.HomeDir || c.HomeDir == "/" {
		return nil, fmt.Errorf("%w: explicit guest home directory required", ErrInvalid)
	}
	if c.ChromiumProfile == "" {
		c.ChromiumProfile = filepath.Join(c.HomeDir, ".config", "sidekicks", "chromium")
	}
	if c.FirefoxProfile == "" {
		c.FirefoxProfile = filepath.Join(c.HomeDir, ".config", "sidekicks", "firefox")
	}
	for _, p := range []string{c.ChromiumProfile, c.FirefoxProfile} {
		if !within(c.HomeDir, p) {
			return nil, fmt.Errorf("%w: profile must be inside guest home", ErrInvalid)
		}
	}
	if c.Backend == nil {
		return nil, fmt.Errorf("%w: window backend required", ErrInvalid)
	}
	if c.Starter == nil {
		c.Starter = ExecStarter{}
	}
	if c.LaunchTimeout == 0 {
		c.LaunchTimeout = 3 * time.Second
	}
	if c.PollInterval == 0 {
		c.PollInterval = 50 * time.Millisecond
	}
	if c.LaunchTimeout < time.Millisecond || c.LaunchTimeout > 30*time.Second || c.PollInterval < time.Millisecond || c.PollInterval > c.LaunchTimeout {
		return nil, ErrInvalid
	}
	env, err := environment(c.HomeDir, c.Environment)
	if err != nil {
		return nil, err
	}
	return &Launcher{cfg: c, env: env, states: map[App]*appState{Browser: {}, Terminal: {}, Files: {}}, lockDir: filepath.Join(c.HomeDir, ".cache", "sidekicks", "launch")}, nil
}
func within(root, p string) bool {
	if !filepath.IsAbs(p) || strings.ContainsRune(p, 0) || filepath.Clean(p) != p {
		return false
	}
	rel, e := filepath.Rel(root, p)
	return e == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func environment(home string, input []string) ([]string, error) {
	kept := map[string]string{}
	for _, entry := range input {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if key == "DISPLAY" || key == "DBUS_SESSION_BUS_ADDRESS" {
			if strings.ContainsRune(value, 0) {
				return nil, ErrInvalid
			}
			kept[key] = value
		}
	}
	display := kept["DISPLAY"]
	if !displayPattern.MatchString(display) {
		return nil, fmt.Errorf("%w: numeric guest-local display required", ErrInvalid)
	}
	env := []string{"HOME=" + home, "PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "XDG_CURRENT_DESKTOP=XFCE", "DESKTOP_SESSION=xfce", "XDG_SESSION_TYPE=x11", "DISPLAY=" + display}
	if bus := kept["DBUS_SESSION_BUS_ADDRESS"]; bus != "" {
		const prefix = "unix:path="
		if !strings.HasPrefix(bus, prefix) || !filepath.IsAbs(strings.TrimPrefix(bus, prefix)) || strings.ContainsAny(bus, ";,\n\r") {
			return nil, fmt.Errorf("%w: guest-local session bus required", ErrInvalid)
		}
		env = append(env, "DBUS_SESSION_BUS_ADDRESS="+bus)
	}
	return env, nil
}
func matches(app App, w Window) bool {
	if !w.Normal || w.ID == 0 || w.Width <= 100 || w.Height <= 80 {
		return false
	}
	class, name := strings.ToLower(w.Class), strings.ToLower(w.Instance)
	switch app {
	case Browser:
		return class == "sidekicksbrowser" || name == "sidekicksbrowser"
	case Terminal:
		return class == "sidekicksterminal" || name == "sidekicksterminal" || (class == "xfce4-terminal" && w.Role == "SidekicksTerminal")
	case Files:
		return class == "thunar" || name == "thunar"
	}
	return false
}
func (l *Launcher) find(ctx context.Context, app App) (Window, bool, error) {
	windows, err := l.cfg.Backend.List(ctx)
	if err != nil {
		return Window{}, false, err
	}
	for i := len(windows) - 1; i >= 0; i-- {
		if matches(app, windows[i]) {
			return windows[i], true, nil
		}
	}
	return Window{}, false, nil
}
func (l *Launcher) markerPath(app App) string {
	return filepath.Join(l.lockDir, string(app)+".json")
}
func (l *Launcher) readMarker(app App) (marker, bool, error) {
	p := l.markerPath(app)
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return marker{}, false, nil
	}
	if err != nil {
		return marker{}, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024 {
		return marker{}, false, ErrUnavailable
	}
	var m marker
	if err = json.NewDecoder(f).Decode(&m); err != nil || m.PID < 1 || m.Started.IsZero() || m.Started.After(time.Now().Add(time.Minute)) {
		return marker{}, false, ErrUnavailable
	}
	return m, true, nil
}
func (l *Launcher) writeMarker(app App, p Process) error {
	data, _ := json.Marshal(marker{p.PID, time.Now()})
	tmp, err := os.CreateTemp(l.lockDir, ".pending-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, l.markerPath(app))
}
func (l *Launcher) clearMarker(app App) {
	_ = os.Remove(l.markerPath(app))
}

// Launch never closes an existing application, navigates its tabs, changes its
// geometry, or kills a child when an HTTP request ends. Close remains closed
// until an explicit subsequent Launch. Concurrent clicks do not queue spawns.
func (l *Launcher) Launch(ctx context.Context, app App) (Result, error) {
	result := Result{OK: true, App: app}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	state, ok := l.states[app]
	if !ok {
		return Result{}, ErrInvalid
	}
	if !state.mu.TryLock() {
		result.State = "launching"
		return result, nil
	}
	defer state.mu.Unlock()
	if err := os.MkdirAll(l.lockDir, 0700); err != nil {
		return Result{}, fmt.Errorf("%w: launch lock directory", ErrUnavailable)
	}
	unlock, err := lockFile(filepath.Join(l.lockDir, string(app)+".lock"))
	if errors.Is(err, errBusy) {
		result.State = "launching"
		return result, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("%w: launch lock", ErrUnavailable)
	}
	defer unlock()
	if w, found, err := l.find(ctx, app); err != nil {
		return Result{}, fmt.Errorf("%w: window enumeration", ErrUnavailable)
	} else if found {
		l.clearMarker(app)
		state.process = nil
		if err = l.cfg.Backend.Activate(ctx, w.ID); err != nil {
			return Result{}, fmt.Errorf("%w: window activation", ErrUnavailable)
		}
		result.State = "restored"
		result.Window = w.ID
		return result, nil
	}
	if state.process != nil && !state.exited {
		select {
		case state.exitErr = <-state.process.Done:
			state.exited = true
		default:
		}
	}
	if state.process != nil {
		if state.exited && state.exitErr != nil {
			state.process = nil
			l.clearMarker(app)
			return Result{}, fmt.Errorf("%w: application exited before creating a window", ErrUnavailable)
		}
		if !state.exited || time.Since(state.started) < pendingGrace {
			result.State = "launching"
			result.PID = state.process.PID
			return result, nil
		}
		state.process = nil
		l.clearMarker(app)
	}
	if previous, found, e := l.readMarker(app); e != nil {
		return Result{}, fmt.Errorf("%w: invalid pending launch record", ErrUnavailable)
	} else if found {
		if processAlive(previous.PID) || time.Since(previous.Started) < pendingGrace {
			result.State = "launching"
			result.PID = previous.PID
			return result, nil
		}
		l.clearMarker(app)
	}
	command, err := l.command(app)
	if err != nil {
		return Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	process, err := l.cfg.Starter.Start(command)
	if err != nil || process.PID < 1 || process.Done == nil {
		return Result{}, fmt.Errorf("%w: process start", ErrUnavailable)
	}
	state.process = &process
	state.started = time.Now()
	state.exited = false
	state.exitErr = nil
	if err = l.writeMarker(app, process); err != nil {
		return Result{}, fmt.Errorf("%w: pending launch record", ErrUnavailable)
	}
	timer := time.NewTimer(l.cfg.LaunchTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(l.cfg.PollInterval)
	defer ticker.Stop()
	for {
		if w, found, e := l.find(ctx, app); e != nil {
			return Result{}, fmt.Errorf("%w: window enumeration", ErrUnavailable)
		} else if found {
			l.clearMarker(app)
			state.process = nil
			result.State = "opened"
			result.Window = w.ID
			result.PID = process.PID
			return result, nil
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-timer.C:
			result.State = "launching"
			result.PID = process.PID
			return result, nil
		case e := <-process.Done:
			state.exited = true
			state.exitErr = e
			if e != nil {
				state.process = nil
				l.clearMarker(app)
				return Result{}, fmt.Errorf("%w: application exited before creating a window", ErrUnavailable)
			}
			// Thunar may delegate to its D-Bus service then exit successfully. Keep
			// waiting for the actual managed window, without spawning a second copy.
			process.Done = nil
		case <-ticker.C:
		}
	}
}

// WaitFor is a startup barrier, not a supervisor. It waits for an actual
// mapped, normal managed app window, never launching, restoring or reopening it.
// The configured launch timeout bounds even a Background context. Call this
// once after initial Launch before publishing workload readiness; later user
// app closure must not invalidate session/compositor readiness.
func (l *Launcher) WaitFor(ctx context.Context, app App) (Window, error) {
	state, ok := l.states[app]
	if !ok {
		return Window{}, ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, l.cfg.LaunchTimeout)
	defer cancel()
	ticker := time.NewTicker(l.cfg.PollInterval)
	defer ticker.Stop()
	for {
		if err := bounded.Err(); err != nil {
			return Window{}, fmt.Errorf("%w: waiting for mapped %s: %w", ErrUnavailable, app, err)
		}
		windows, err := l.cfg.Backend.List(bounded)
		if err == nil {
			for i := len(windows) - 1; i >= 0; i-- {
				if matches(app, windows[i]) && windows[i].Mapped {
					return windows[i], nil
				}
			}
		}
		if state.mu.TryLock() {
			if state.process != nil && !state.exited {
				select {
				case state.exitErr = <-state.process.Done:
					state.exited = true
				default:
				}
			}
			failed := state.exited && state.exitErr != nil
			if failed {
				state.process = nil
				l.clearMarker(app)
			}
			state.mu.Unlock()
			if failed {
				return Window{}, fmt.Errorf("%w: %s exited before mapping a normal window", ErrUnavailable, app)
			}
		}
		select {
		case <-bounded.Done():
			return Window{}, fmt.Errorf("%w: waiting for mapped %s: %w", ErrUnavailable, app, bounded.Err())
		case <-ticker.C:
		}
	}
}

// State reports whether the window manager is ready and lists managed windows.
type State struct {
	Ready   bool           `json:"ready"`
	Windows map[App]uint32 `json:"windows"`
}

// State reports window-manager query availability, including when every app is
// closed. Session/compositor readiness is a separate guest lifecycle concern.
func (l *Launcher) State(ctx context.Context) (State, error) {
	windows, err := l.cfg.Backend.List(ctx)
	if err != nil {
		return State{}, ErrUnavailable
	}
	state := State{Ready: true, Windows: map[App]uint32{}}
	for _, app := range []App{Browser, Terminal, Files} {
		for i := len(windows) - 1; i >= 0; i-- {
			if matches(app, windows[i]) {
				state.Windows[app] = windows[i].ID
				break
			}
		}
	}
	return state, nil
}

// Handler is intentionally authentication-free plumbing. Mount it only behind
// the root agent's authenticated/internal-Home authority and ownership gate.
// Hiding application menus is UX curation, never a security boundary.
func (l *Launcher) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaErr != nil || mediaType != "application/json" {
			http.Error(w, "json required", http.StatusUnsupportedMediaType)
			return
		}
		var body struct {
			App App `json:"app"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			http.Error(w, "invalid application request", http.StatusBadRequest)
			return
		}
		var extra any
		if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
			http.Error(w, "invalid application request", http.StatusBadRequest)
			return
		}
		result, err := l.Launch(r.Context(), body.App)
		if err != nil {
			status := http.StatusServiceUnavailable
			if errors.Is(err, ErrInvalid) {
				status = http.StatusBadRequest
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				status = http.StatusRequestTimeout
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}
