package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute/control"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/apps"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/session"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/viewer"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/x11"
	"github.com/kevinmartin/sidekicks/internal/compute/neko"
	assets "github.com/kevinmartin/sidekicks/web"
)

type desktopWorkload struct {
	session      *session.Manager
	driver       *x11.Driver
	gate         *control.Manager
	launcher     *apps.Launcher
	viewer       *viewer.Service
	nekoClient   *neko.Client
	nekoHandler  *neko.Handler
	stopSites    []func()
	stopChildren []func()
	childDone    []<-chan struct{}
	leaseMu      sync.Mutex
	localLease   control.Lease
	fixture      *assets.FixtureSite
}

// desktopDependencies is per bootstrap invocation; production never mutates global hooks.
type desktopDependencies struct {
	startSession func(context.Context, session.Config) (*session.Manager, error)
	openX11      func(context.Context, string, string) (*x11.Driver, error)
	newWindows   func(string, string) (apps.Windows, error)
	newLauncher  func(apps.Config) (*apps.Launcher, error)
	writeFile    func(string, []byte, os.FileMode) error
	startSite    func(context.Context, string, http.Handler) (func(), error)
	startChild   func(context.Context, []string, string, ...string) (func(), <-chan struct{}, error)
	waitTCP      func(context.Context, string) error
	prepareNeko  func(*desktopWorkload, context.Context, string) error
}

func defaultDesktopDependencies() desktopDependencies {
	return desktopDependencies{startSession: session.Start, openX11: x11.Open, newWindows: func(display, cookie string) (apps.Windows, error) {
		return apps.NewX11(display, cookie)
	}, newLauncher: apps.New, writeFile: os.WriteFile, startSite: startSite, startChild: startChild, waitTCP: waitTCP, prepareNeko: (*desktopWorkload).prepareNeko}
}
func prepareDesktop(ctx context.Context, kind, token string) (*desktopWorkload, error) {
	return prepareDesktopWith(ctx, kind, token, defaultDesktopDependencies())
}
func prepareDesktopWith(ctx context.Context, kind, token string, deps desktopDependencies) (result *desktopWorkload, err error) {
	d := &desktopWorkload{}
	defer func() {
		if err != nil {
			d.close()
		}
	}()
	d.session, err = deps.startSession(ctx, session.Config{})
	if err != nil {
		return nil, err
	}

	xctx, xcancel := context.WithTimeout(ctx, 5*time.Second)
	defer xcancel()
	d.driver, err = deps.openX11(xctx, ":99", "")
	if err != nil {
		return nil, err
	}
	d.gate = control.New(d.driver)
	windows, err := deps.newWindows(":99", "")
	if err != nil {
		return nil, err
	}
	browserKind := apps.Chromium
	if kind == "desktop-neko" {
		browserKind = apps.Firefox
	}
	d.launcher, err = deps.newLauncher(apps.Config{Browser: browserKind, HomeURL: "http://127.0.0.1:8081/home", HomeDir: session.DefaultHome, Backend: windows, Environment: d.session.Env()})
	if err != nil {
		return nil, err
	}
	site := http.NewServeMux()
	d.fixture = assets.NewFixtureSite()
	site.Handle("/fixture", d.fixture)
	site.Handle("/fixture/state", d.fixture)
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	homeToken := hex.EncodeToString(nonce[:])
	if err = deps.writeFile("/run/sidekicks-home.token", []byte(homeToken), 0600); err != nil {
		return nil, err
	}
	internal := func(r *http.Request) bool {
		host, _, e := net.SplitHostPort(r.RemoteAddr)
		return e == nil && host == "127.0.0.1" && r.Host == "127.0.0.1:8081"
	}
	site.HandleFunc("/home", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !internal(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		b, _ := assets.Assets.ReadFile("desktop/home.html")
		b = bytes.ReplaceAll(b, []byte("__SIDEKICKS_DESKTOP_TOKEN__"), []byte(homeToken))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
	site.HandleFunc("/desktop/launch", func(w http.ResponseWriter, r *http.Request) {
		if !internal(r) || r.Header.Get("Origin") != "http://127.0.0.1:8081" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Sidekicks-Desktop")), []byte(homeToken)) != 1 || d.gate.View().Owner == "" {
			http.Error(w, "active desktop ownership required", http.StatusForbidden)
			return
		}
		var body struct {
			App apps.App `json:"app"`
		}
		if !strictJSON(w, r, &body) {
			return
		}
		if err := d.localLaunch(r.Context(), body.App); err != nil {
			http.Error(w, "desktop ownership changed", http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	stop, err := deps.startSite(ctx, "127.0.0.1:8081", site)
	if err != nil {
		return nil, err
	}
	d.stopSites = append(d.stopSites, stop)
	switch kind {
	case "desktop-novnc":
		stop, done, childErr := deps.startChild(ctx, d.session.Env(), "/usr/local/bin/x11vnc", rfbServerArgs()...)
		if childErr != nil {
			return nil, childErr
		}
		d.childDone = append(d.childDone, done)
		d.stopChildren = append(d.stopChildren, stop)
		if err = deps.waitTCP(ctx, "127.0.0.1:5900"); err != nil {
			return nil, err
		}
		d.viewer, err = viewer.New(viewer.Config{Gate: d.gate, Driver: d.driver, RFBTarget: "127.0.0.1:5900", Assets: desktopAssets(kind), OnAcquire: d.onAcquire})
		if err != nil {
			return nil, err
		}
	case "desktop-neko":
		if err = deps.prepareNeko(d, ctx, token); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unsupported desktop workload")
	}
	stop, err = deps.startSite(ctx, "0.0.0.0:6080", d.viewer)
	if err != nil {
		return nil, err
	}
	d.stopSites = append(d.stopSites, stop)
	if os.Getenv("SIDEKICKS_DESKTOP_START_BROWSER") != "0" {
		if _, err = d.launcher.Launch(ctx, apps.Browser); err != nil {
			return nil, fmt.Errorf("initial guest browser: %w", err)
		}
		if _, err = d.launcher.WaitFor(ctx, apps.Browser); err != nil {
			return nil, fmt.Errorf("initial guest browser did not map: %w", err)
		}
	}
	return d, nil
}
func (d *desktopWorkload) close() {
	for i := len(d.stopSites) - 1; i >= 0; i-- {
		d.stopSites[i]()
	}
	if d.viewer != nil {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = d.viewer.Close(c)
		cancel()
	}
	if d.gate != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = d.gate.Close(ctx)
		cancel()
	}
	if d.nekoClient != nil {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = d.nekoClient.Close(c)
		cancel()
	}
	for i := len(d.stopChildren) - 1; i >= 0; i-- {
		d.stopChildren[i]()
	}
	if d.driver != nil {
		_ = d.driver.Close()
	}
	if d.session != nil {
		_ = d.session.Close()
	}
}
func desktopAssets(kind string) http.Handler {
	vendor := http.StripPrefix("/vendor/novnc/", http.FileServer(http.Dir("/opt/novnc")))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := ""
		mime := ""
		switch r.URL.Path {
		case "/":
			if kind == "desktop-neko" {
				name = "desktop/neko/index.html"
			} else {
				name = "desktop/novnc/index.html"
			}
			mime = "text/html; charset=utf-8"
		case "/viewer.css":
			name = "desktop/viewer.css"
			mime = "text/css"
		case "/input.mjs":
			name = "desktop/input.mjs"
			mime = "application/javascript"
		case "/viewer.js":
			mime = "application/javascript"
			if kind == "desktop-neko" {
				b, err := os.ReadFile("/usr/share/sidekicks/viewers/neko.js")
				if err != nil {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", mime)
				_, _ = w.Write(b)
				return
			}
			name = "desktop/novnc/viewer.mjs"
		default:
			if kind == "desktop-novnc" && strings.HasPrefix(r.URL.Path, "/vendor/novnc/") {
				vendor.ServeHTTP(w, r)
				return
			}
			http.NotFound(w, r)
			return
		}
		b, err := assets.Assets.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mime)
		_, _ = w.Write(b)
	})
}
func startChild(ctx context.Context, env []string, path string, args ...string) (func(), <-chan struct{}, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append([]string(nil), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Only process names are surfaced; runtime diagnostics may contain private settings.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("guest child %s could not start", path)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cmd.Wait()
	}()
	return func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}, done, nil
}
func waitTCP(ctx context.Context, address string) error {
	timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		c, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(timeout, "tcp", address)
		if err == nil {
			c.Close()
			return nil
		}
		select {
		case <-timeout.Done():
			return errors.New("guest desktop backend did not become ready")
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func (d *desktopWorkload) private(w http.ResponseWriter, r *http.Request) {
	write := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch r.URL.Path {
	case "/desktop/connect":
		if d.nekoHandler != nil {
			var body struct {
				Ports []struct {
					Guest uint16 `json:"guest"`
					Host  uint16 `json:"host"`
				} `json:"ports"`
			}
			if !strictJSON(w, r, &body) {
				return
			}
			ports := map[uint16]uint16{}
			for _, p := range body.Ports {
				if _, ok := ports[p.Guest]; ok {
					write(400, map[string]string{"error": "duplicate media port"})
					return
				}
				ports[p.Guest] = p.Host
			}
			if err := d.nekoHandler.SetMediaPorts(ports); err != nil {
				write(409, map[string]string{"error": "media port mapping rejected"})
				return
			}
		}
		ticket, err := d.viewer.NewTicket()
		if err != nil {
			write(503, map[string]string{"error": "viewer capacity unavailable"})
			return
		}
		write(200, map[string]any{"ticket": ticket, "port": 6080})
	case "/desktop/state":
		state, err := d.driver.State(r.Context())
		if err != nil {
			write(503, map[string]string{"error": "actual input state unavailable"})
			return
		}
		write(200, map[string]any{"ready": d.ready() == nil, "control": d.gate.View(), "actual": state, "fixture": d.fixture.Snapshot(), "pids": d.session.PIDs()})
	case "/desktop/launch":
		var body struct {
			App     apps.App `json:"app"`
			Session string   `json:"session"`
			Token   string   `json:"token"`
		}
		if !strictJSON(w, r, &body) {
			return
		}
		creds := control.Credentials{Session: body.Session, Token: body.Token}
		lease, e := d.gate.Validate(creds)
		if e != nil || lease.Owner != control.Agent {
			write(409, map[string]string{"error": "active desktop ownership required"})
			return
		}
		err := d.gate.DispatchGated(r.Context(), creds, func(c context.Context) error {
			_, e := d.launcher.Launch(c, body.App)
			return e
		})
		if err != nil {
			write(409, map[string]string{"error": "launch rejected"})
			return
		}
		write(200, map[string]bool{"ok": true})
	case "/desktop/control":
		if r.Method == "GET" {
			write(200, d.gate.View())
			return
		}
		var b struct {
			Action  string        `json:"action"`
			Owner   control.Owner `json:"owner"`
			Session string        `json:"session"`
			Token   string        `json:"token"`
			LeaseMS int           `json:"leaseMs"`
		}
		if !strictJSON(w, r, &b) {
			return
		}
		var err error
		switch b.Action {
		case "acquire":
			if b.Owner != control.Agent {
				write(403, map[string]string{"error": "human control requires an authenticated viewer"})
				return
			}
			var l control.Lease
			l, err = d.gate.Acquire(r.Context(), control.Request{Owner: b.Owner, Session: b.Session, Token: b.Token, Mode: control.Gated, Duration: time.Duration(b.LeaseMS) * time.Millisecond})
			if err == nil {
				d.remember(l)
				write(200, l)
				return
			}
		case "release":
			err = d.gate.Release(r.Context(), control.Credentials{Session: b.Session, Token: b.Token})
		default:
			write(400, map[string]string{"error": "unsupported control action"})
			return
		}
		if err != nil {
			write(409, map[string]string{"error": "control rejected"})
			return
		}
		write(200, map[string]bool{"ok": true})
	case "/desktop/input":
		var b struct {
			Session string          `json:"session"`
			Token   string          `json:"token"`
			Events  []desktop.Event `json:"events"`
		}
		if !strictJSON(w, r, &b) {
			return
		}
		width, height := d.driver.Size()
		if desktop.ValidateEvents(b.Events, width, height) != nil {
			c := control.Credentials{Session: b.Session, Token: b.Token}
			if l, e := d.gate.Validate(c); e == nil && l.Mode == control.Gated {
				_ = d.gate.Release(r.Context(), c)
			}
			write(422, map[string]string{"error": "invalid input batch"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		for _, e := range b.Events {
			e := e
			if err := d.gate.DispatchGated(ctx, control.Credentials{Session: b.Session, Token: b.Token}, func(c context.Context) error {
				return d.driver.Input(c, e)
			}); err != nil {
				write(409, map[string]string{"error": "input rejected or unavailable"})
				return
			}
		}
		write(200, map[string]bool{"ok": true})
	case "/desktop/capture":
		b, err := d.driver.CapturePNG(r.Context())
		if err != nil {
			write(503, map[string]string{"error": "capture unavailable"})
			return
		}
		if len(b) > 1<<20 {
			write(413, map[string]string{"error": "capture exceeds private transport limit"})
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(b)
	default:
		http.NotFound(w, r)
	}
}
func strictJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(v) != nil || dec.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid bounded json", http.StatusBadRequest)
		return false
	}
	return true
}

func (d *desktopWorkload) ready() error {
	for _, done := range d.childDone {
		select {
		case <-done:
			return errors.New("guest desktop backend stopped")
		default:
		}
	}
	if err := d.session.Ready(); err != nil {
		return err
	}
	if d.nekoClient != nil && !d.nekoClient.Ready() {
		return errors.New("neko controller is unavailable")
	}
	if !d.driver.Ready() {
		return errors.New("desktop x11 input is unavailable; guest restart required")
	}
	if !d.gate.View().Ready {
		return errors.New("desktop input is unavailable")
	}
	return nil
}
func (d *desktopWorkload) execGate(ctx context.Context, run func(context.Context) error) error {
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	lease, err := d.gate.Acquire(ctx, control.Request{Owner: control.Agent, Session: "exec-" + hex.EncodeToString(nonce[:]), Duration: 60 * time.Second})
	if err != nil {
		return err
	}
	d.remember(lease)
	credentials := control.Credentials{Session: lease.Session, Token: lease.Token}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = d.gate.Release(c, credentials)
	}()
	return d.gate.DispatchGated(ctx, credentials, run)
}
func launchRequest(args []string) error {
	if len(args) != 1 || (args[0] != "browser" && args[0] != "terminal" && args[0] != "files") {
		return errors.New("launch requires one fixed desktop app")
	}
	token, err := os.ReadFile("/run/sidekicks-home.token")
	if err != nil {
		return errors.New("desktop launcher is unavailable")
	}
	body, _ := json.Marshal(map[string]string{"app": args[0]})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:8081/desktop/launch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:8081")
	req.Header.Set("X-Sidekicks-Desktop", string(token))
	res, err := (&http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return errors.New("desktop launcher request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("desktop launch requires active input ownership")
	}
	return nil
}
func (d *desktopWorkload) remember(l control.Lease) {
	d.leaseMu.Lock()
	defer d.leaseMu.Unlock()
	if l.Generation >= d.localLease.Generation {
		d.localLease = l
	}
}
func (d *desktopWorkload) localLaunch(ctx context.Context, app apps.App) error {
	d.leaseMu.Lock()
	l := d.localLease
	d.leaseMu.Unlock()
	return d.gate.Dispatch(ctx, control.Credentials{Session: l.Session, Token: l.Token}, func(c context.Context) error {
		_, err := d.launcher.Launch(c, app)
		return err
	})
}
func (d *desktopWorkload) onAcquire(session string, l control.Lease) error {
	if d.nekoHandler != nil {
		if err := d.nekoHandler.BindLease(session, l); err != nil {
			return err
		}
	}
	d.remember(l)
	return nil
}

// rfbServerArgs fixes the raw server to guest-loopback video-only operation.
// All keyboard/pointer injection belongs to synchronized Go X11 dispatch.
func rfbServerArgs() []string {
	return []string{"-display", ":99", "-rfbport", "5900", "-localhost", "-forever", "-shared", "-nopw", "-nowireframe", "-viewonly"}
}
