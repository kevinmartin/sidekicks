package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/kevinmartin/sidekicks/internal/compute/control"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/apps"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/session"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/viewer"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/x11"
	"github.com/kevinmartin/sidekicks/internal/compute/guest"
	"github.com/kevinmartin/sidekicks/internal/compute/neko"
	assets "github.com/kevinmartin/sidekicks/web"
)

const orchestrationToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type unitX struct {
	mu     sync.Mutex
	state  x11.State
	closed bool
	fail   bool
}

// Size implements the test fixture's Size method.
func (*unitX) Size() (int, int) {
	return 2, 2
}

// Mapping implements the test fixture's Mapping method.
func (*unitX) Mapping() map[uint32]x11.Stroke {
	return map[uint32]x11.Stroke{'a': {Code: 38}, 'A': {Code: 38, Shift: true}, 0xffe1: {Code: 50}, 0xffe3: {Code: 37}, 0xff0d: {Code: 36}}
}

// State implements the test fixture's State method.
func (x *unitX) State() (x11.State, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.fail || x.closed {
		return x11.State{}, errors.New("unit x unavailable")
	}
	return x.state, nil
}

// Fake implements the test fixture's Fake method.
func (x *unitX) Fake(kind, detail byte, px, py int16) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	switch kind {
	case xproto.KeyPress:
		x.state.Keys[detail/8] |= 1 << uint(detail%8)
	case xproto.KeyRelease:
		x.state.Keys[detail/8] &^= 1 << uint(detail%8)
	case xproto.ButtonPress:
		x.state.Buttons |= 1 << uint(detail+7)
	case xproto.ButtonRelease:
		x.state.Buttons &^= 1 << uint(detail+7)
	case xproto.MotionNotify:
		x.state.X = int(px)
		x.state.Y = int(py)
	}
	return nil
}

// Image implements the test fixture's Image method.
func (x *unitX) Image() (x11.RawImage, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.fail {
		return x11.RawImage{}, errors.New("unit capture failure")
	}
	return x11.RawImage{Width: 2, Height: 2, Depth: 24, BitsPerPixel: 32, ScanlinePad: 32, RedMask: 0xff0000, GreenMask: 0xff00, BlueMask: 0xff, Data: []byte{0, 0, 255, 0, 0, 255, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0}}, nil
}

// Sync implements the test fixture's Sync method.
func (x *unitX) Sync() error {
	return nil
}

// Close implements the test fixture's Close method.
func (x *unitX) Close() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.closed = true
	return nil
}
func (x *unitX) held() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, bits := range x.state.Keys {
		if bits != 0 {
			return true
		}
	}
	return x.state.Buttons != 0
}

type unitChild struct {
	pid  int
	done chan struct{}
	once sync.Once
}

// PID implements the test fixture's PID method.
func (p *unitChild) PID() int {
	return p.pid
}

// Done implements the test fixture's Done method.
func (p *unitChild) Done() <-chan struct{} {
	return p.done
}

// Stop implements the test fixture's Stop method.
func (p *unitChild) Stop() error {
	p.once.Do(func() {
		close(p.done)
	})
	return nil
}

type unitSessionRunner struct {
	mu      sync.Mutex
	missing bool
}

// Start implements the test fixture's Start method.
func (*unitSessionRunner) Start(_ context.Context, c session.Command) (session.Process, error) {
	return &unitChild{pid: len(c.Name) + 100, done: make(chan struct{})}, nil
}

// Probe implements the test fixture's Probe method.
func (*unitSessionRunner) Probe(context.Context, session.Command) error {
	return nil
}

// Components implements the test fixture's Components method.
func (r *unitSessionRunner) Components(context.Context, int, []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.missing {
		return errors.New("component exited")
	}
	return nil
}

type unitWindows struct {
	mu        sync.Mutex
	windows   []apps.Window
	activated int
}

// List implements the test fixture's List method.
func (w *unitWindows) List(context.Context) ([]apps.Window, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]apps.Window(nil), w.windows...), nil
}

// Activate implements the test fixture's Activate method.
func (w *unitWindows) Activate(context.Context, uint32) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.activated++
	return nil
}
func newUnitDesktop(t *testing.T) (*desktopWorkload, *unitX, *unitSessionRunner, *unitWindows) {
	t.Helper()
	base := t.TempDir()
	assetRoot := filepath.Join(base, "assets")
	for _, name := range []string{"wallpaper.png", "icons/browser.png", "icons/terminal.png", "icons/files.png", "polish/settings.ini", "polish/gtk.css", "polish/icons/index.theme", "polish/icons/scalable/places/folder.svg"} {
		target := filepath.Join(assetRoot, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("original-asset-placeholder"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runner := &unitSessionRunner{}
	manager, err := session.Start(context.Background(), session.Config{Home: filepath.Join(base, "home"), Assets: assetRoot, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	backend := &unitX{}
	driver := x11.New(backend)
	gate := control.New(driver)
	windows := &unitWindows{windows: []apps.Window{{ID: 42, Class: "SidekicksBrowser", Normal: true, Mapped: true, Width: 1120, Height: 548}, {ID: 43, Class: "Xfce4-terminal", Role: "SidekicksTerminal", Normal: true, Mapped: true, Width: 800, Height: 500}, {ID: 44, Class: "Thunar", Normal: true, Mapped: true, Width: 900, Height: 500}}}
	launcher, err := apps.New(apps.Config{Browser: apps.Chromium, HomeURL: "http://127.0.0.1:8081/home", HomeDir: filepath.Join(base, "home"), Backend: windows, Environment: manager.Env()})
	if err != nil {
		t.Fatal(err)
	}
	d := &desktopWorkload{session: manager, driver: driver, gate: gate, launcher: launcher, fixture: assets.NewFixtureSite()}
	d.viewer, err = viewer.New(viewer.Config{Gate: gate, Driver: driver, OnAcquire: d.onAcquire})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.close)
	return d, backend, runner, windows
}
func privateCall(d *desktopWorkload, method, path string, body any) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	d.private(response, request)
	return response
}
func acquireUnit(t *testing.T, d *desktopWorkload, owner control.Owner, id string) control.Lease {
	t.Helper()
	lease, err := d.gate.Acquire(context.Background(), control.Request{Owner: owner, Session: id, Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

// TestPrivateDesktopControlAndLaunchOwnership verifies private desktop control and launch ownership.
func TestPrivateDesktopControlAndLaunchOwnership(t *testing.T) {
	d, _, _, windows := newUnitDesktop(t)
	for _, body := range []any{map[string]any{"action": "acquire", "owner": "human", "session": "wrong-origin"}, map[string]any{"action": "stop"}, map[string]any{"action": "acquire", "owner": "agent", "session": "bad/session"}} {
		response := privateCall(d, "POST", "/desktop/control", body)
		if response.Code == 200 {
			t.Fatal("unauthorized or invalid acquisition accepted", response.Body)
		}
	}
	response := privateCall(d, "POST", "/desktop/control", map[string]any{"action": "acquire", "owner": "agent", "session": "qa-agent"})
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body)
	}
	var lease control.Lease
	if err := json.Unmarshal(response.Body.Bytes(), &lease); err != nil {
		t.Fatal(err)
	}
	if d.localLease.Token != lease.Token {
		t.Fatal("local ownership snapshot not recorded")
	}
	if response = privateCall(d, "POST", "/desktop/launch", map[string]any{"app": "browser", "session": lease.Session, "token": "stale"}); response.Code != 409 {
		t.Fatal(response.Code)
	}
	if response = privateCall(d, "POST", "/desktop/launch", map[string]any{"app": "browser", "session": lease.Session, "token": lease.Token}); response.Code != 200 {
		t.Fatal(response.Code, response.Body)
	}
	windows.mu.Lock()
	activated := windows.activated
	windows.mu.Unlock()
	if activated != 1 {
		t.Fatal("fixed app was not restored")
	}
	human := acquireUnit(t, d, control.Human, "qa-human")
	if response = privateCall(d, "POST", "/desktop/launch", map[string]any{"app": "terminal", "session": human.Session, "token": human.Token}); response.Code != 409 {
		t.Fatal("machine launch stole human control")
	}
	if response = privateCall(d, "POST", "/desktop/launch", map[string]any{"app": "terminal", "session": lease.Session, "token": lease.Token}); response.Code != 409 {
		t.Fatal("stale agent launch accepted")
	}
	if err := d.onAcquire(human.Session, human); err != nil {
		t.Fatal(err)
	}
	if err := d.localLaunch(context.Background(), apps.Terminal); err != nil {
		t.Fatal("human dock path could not restore", err)
	}
	d.remember(lease)
	if d.localLease.Token != human.Token {
		t.Fatal("old acquisition overwrote new ownership")
	}
	if response = privateCall(d, "GET", "/desktop/control", nil); response.Code != 200 || !strings.Contains(response.Body.String(), "qa-human") {
		t.Fatal(response.Code, response.Body)
	}
	if response = privateCall(d, "POST", "/desktop/control", map[string]any{"action": "release", "session": human.Session, "token": "stale"}); response.Code != 409 {
		t.Fatal(response.Code)
	}
	if response = privateCall(d, "POST", "/desktop/control", map[string]any{"action": "release", "session": human.Session, "token": human.Token}); response.Code != 200 {
		t.Fatal(response.Code, response.Body)
	}
	if err := d.localLaunch(context.Background(), apps.Browser); err == nil {
		t.Fatal("unowned dock path accepted")
	}
}

// TestPrivateInputMalformedBatchReleasesActualHeldState verifies private input malformed batch releases actual held state.
func TestPrivateInputMalformedBatchReleasesActualHeldState(t *testing.T) {
	d, backend, _, _ := newUnitDesktop(t)
	lease := acquireUnit(t, d, control.Agent, "input-agent")
	body := map[string]any{"session": lease.Session, "token": lease.Token, "events": []any{map[string]any{"type": "key", "key": "a", "action": "down"}}}
	if response := privateCall(d, "POST", "/desktop/input", body); response.Code != 200 {
		t.Fatal(response.Code, response.Body)
	}
	if !backend.held() {
		t.Fatal("test did not establish a real backend hold")
	}
	body["events"] = []any{map[string]any{"type": "key", "key": "unsupported", "action": "down"}}
	if response := privateCall(d, "POST", "/desktop/input", body); response.Code != 422 {
		t.Fatal(response.Code, response.Body)
	}
	if backend.held() || d.gate.View().Owner != "" {
		t.Fatal("invalid batch retained actual held input or ownership")
	}
	if response := privateCall(d, "POST", "/desktop/input", map[string]any{"session": lease.Session, "token": lease.Token, "events": []any{map[string]any{"type": "text", "text": "a"}}}); response.Code != 409 {
		t.Fatal("stale input accepted", response.Code)
	}
}

// TestPrivateStateHealthCaptureAndTicket verifies private state health capture and ticket.
func TestPrivateStateHealthCaptureAndTicket(t *testing.T) {
	d, backend, runner, _ := newUnitDesktop(t)
	if err := d.ready(); err != nil {
		t.Fatal(err)
	}
	state := privateCall(d, "GET", "/desktop/state", nil)
	if state.Code != 200 || !strings.Contains(state.Body.String(), `"ready":true`) {
		t.Fatal(state.Code, state.Body)
	}
	capture := privateCall(d, "GET", "/desktop/capture", nil)
	if capture.Code != 200 || capture.Header().Get("Content-Type") != "image/png" {
		t.Fatal(capture.Code, capture.Body)
	}
	image, err := png.Decode(bytes.NewReader(capture.Body.Bytes()))
	if err != nil || image.Bounds().Dx() != 2 {
		t.Fatal("not a fresh valid backend PNG", err)
	}
	ticket := privateCall(d, "POST", "/desktop/connect", map[string]any{})
	if ticket.Code != 200 || !strings.Contains(ticket.Body.String(), `"ticket"`) {
		t.Fatal(ticket.Code, ticket.Body)
	}
	done := make(chan struct{})
	d.childDone = []<-chan struct{}{done}
	close(done)
	if err = d.ready(); err == nil {
		t.Fatal("dead backend child passed health")
	}
	if state = privateCall(d, "GET", "/desktop/state", nil); state.Code != 200 || !strings.Contains(state.Body.String(), `"ready":false`) {
		t.Fatal("state disagrees with actual backend health", state.Code, state.Body)
	}
	d.childDone = nil
	runner.mu.Lock()
	runner.missing = true
	runner.mu.Unlock()
	if err = d.ready(); err == nil {
		t.Fatal("stale session marker passed health")
	}
	runner.mu.Lock()
	runner.missing = false
	runner.mu.Unlock()
	client, err := neko.New(neko.Config{Endpoint: "http://127.0.0.1:8082", AdminUsername: "controller", AdminPassword: "unit-secret", ViewerPassword: "unit-viewer"})
	if err != nil {
		t.Fatal(err)
	}
	d.nekoClient = client
	if err = d.ready(); err == nil {
		t.Fatal("disconnected controller passed health")
	}
	d.nekoClient = nil
	backend.mu.Lock()
	backend.fail = true
	backend.mu.Unlock()
	if capture = privateCall(d, "GET", "/desktop/capture", nil); capture.Code != 503 {
		t.Fatal(capture.Code)
	}
	if state = privateCall(d, "GET", "/desktop/state", nil); state.Code != 503 {
		t.Fatal(state.Code)
	}
}

// TestAuthenticatedPrivateHandlerMountAndStrictJSON verifies authenticated private handler mount and strict json.
func TestAuthenticatedPrivateHandlerMountAndStrictJSON(t *testing.T) {
	d, _, _, _ := newUnitDesktop(t)
	routes := map[string]http.Handler{}
	for _, path := range []string{"/desktop/state", "/desktop/control", "/desktop/launch", "/desktop/input", "/desktop/capture", "/desktop/connect"} {
		routes[path] = http.HandlerFunc(d.private)
	}
	handler, err := guest.New(guest.Config{Workspace: t.TempDir(), Token: orchestrationToken, Health: d.ready, ExtraRoutes: routes})
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	request := httptest.NewRequest("POST", "/desktop/control", strings.NewReader(`{"action":"acquire","owner":"agent","session":"not-authorized"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 401 || d.gate.View().Owner != "" {
		t.Fatal("public route acquired private control")
	}
	request.Header.Set("Authorization", "Bearer "+orchestrationToken)
	request.Header.Set("Origin", "http://evil.test")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 403 || d.gate.View().Owner != "" {
		t.Fatal("foreign Origin reached control")
	}
	for _, raw := range []string{`{"action":"acquire","unknown":true}`, `{} {}`, strings.Repeat("x", 65537)} {
		request = httptest.NewRequest("POST", "/desktop/control", strings.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		response = httptest.NewRecorder()
		d.private(response, request)
		if response.Code != 400 {
			t.Fatal(response.Code)
		}
	}
	request = httptest.NewRequest("POST", "/desktop/input", strings.NewReader(`{}`))
	response = httptest.NewRecorder()
	d.private(response, request)
	if response.Code != 415 {
		t.Fatal(response.Code)
	}
	if response = privateCall(d, "GET", "/private-host-shell", nil); response.Code != 404 {
		t.Fatal(response.Code)
	}
}

// TestExecGateCannotRunThroughHumanTakeover verifies exec gate cannot run through human takeover.
func TestExecGateCannotRunThroughHumanTakeover(t *testing.T) {
	d, _, _, _ := newUnitDesktop(t)
	human := acquireUnit(t, d, control.Human, "human")
	called := false
	if err := d.execGate(context.Background(), func(context.Context) error {
		called = true
		return nil
	}); err == nil || called {
		t.Fatal("exec bypassed human ownership")
	}
	if err := d.gate.Release(context.Background(), control.Credentials{Session: human.Session, Token: human.Token}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- d.execGate(context.Background(), func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	<-started
	next := acquireUnit(t, d, control.Human, "next-human")
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revoked exec succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("human takeover did not cancel command callback")
	}
	if d.gate.View().Session != next.Session {
		t.Fatal("old exec defer released the new owner")
	}
}

// TestDesktopCleanupClearsInputsAndOwnedResources verifies desktop cleanup clears inputs and owned resources.
func TestDesktopCleanupClearsInputsAndOwnedResources(t *testing.T) {
	d, backend, _, _ := newUnitDesktop(t)
	lease := acquireUnit(t, d, control.Agent, "cleanup")
	if err := d.gate.DispatchGated(context.Background(), control.Credentials{Session: lease.Session, Token: lease.Token}, func(ctx context.Context) error {
		return d.driver.Input(ctx, desktopKeyDown())
	}); err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	d.stopSites = []func(){func() {
		calls = append(calls, "site1")
	}, func() {
		calls = append(calls, "site2")
	}}
	d.stopChildren = []func(){func() {
		calls = append(calls, "child1")
	}, func() {
		calls = append(calls, "child2")
	}}
	d.close()
	d.stopSites = nil
	d.stopChildren = nil
	if backend.held() || strings.Join(calls, ",") != "site2,site1,child2,child1" || d.ready() == nil {
		t.Fatal("cleanup incomplete", calls)
	}
}
func desktopKeyDown() desktop.Event {
	return desktop.Event{Type: "key", Key: "a", Action: "down"}
}

// TestAssetRoutesAreBounded verifies asset routes are bounded.
func TestAssetRoutesAreBounded(t *testing.T) {
	for _, kind := range []string{"desktop-novnc", "desktop-neko"} {
		handler := desktopAssets(kind)
		for _, path := range []string{"/", "/viewer.css", "/private-host-exec", "/../../etc/passwd"} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
			if (path == "/" || path == "/viewer.css") && response.Code != 200 {
				t.Fatal(kind, path, response.Code)
			}
			if strings.Contains(path, "exec") && response.Code != 404 {
				t.Fatal("machine API exposed by assets")
			}
		}
	}
}

// TestLaunchRequestRejectsArbitraryCommandBeforeIO verifies launch request rejects arbitrary command before io.
func TestLaunchRequestRejectsArbitraryCommandBeforeIO(t *testing.T) {
	for _, args := range [][]string{nil, {"browser", "--url", "http://evil"}, {"sh"}, {"terminal;rm"}} {
		if err := launchRequest(args); err == nil {
			t.Fatal("arbitrary launcher accepted", fmt.Sprint(args))
		}
	}
}

// TestDesktopBootstrapCanceledBeforeAnyGuestResource verifies desktop bootstrap canceled before any guest resource.
func TestDesktopBootstrapCanceledBeforeAnyGuestResource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if desktop, err := prepareDesktop(ctx, "desktop-novnc", orchestrationToken); err == nil || desktop != nil {
		t.Fatal("canceled bootstrap accepted")
	}
}

// TestNekoConnectRequiresCompleteUniqueMediaAllocation verifies neko connect requires complete unique media allocation.
func TestNekoConnectRequiresCompleteUniqueMediaAllocation(t *testing.T) {
	d, _, _, _ := newUnitDesktop(t)
	client, err := neko.New(neko.Config{Endpoint: "http://127.0.0.1:8082", AdminUsername: "controller", AdminPassword: "private-admin", ViewerPassword: "private-viewer"})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := neko.NewHandler(client, d.gate, neko.HandlerOptions{Token: orchestrationToken})
	if err != nil {
		t.Fatal(err)
	}
	d.nekoClient = client
	d.nekoHandler = handler
	if response := privateCall(d, "POST", "/desktop/connect", map[string]any{"ports": []map[string]uint16{{"guest": 60020, "host": 50020}, {"guest": 60020, "host": 50021}}}); response.Code != 400 {
		t.Fatal("duplicate media port accepted", response.Code)
	}
	if response := privateCall(d, "POST", "/desktop/connect", map[string]any{"ports": []map[string]uint16{{"guest": 60020, "host": 50020}}}); response.Code != 409 {
		t.Fatal("partial media allocation accepted", response.Code)
	}
	ports := []map[string]uint16{}
	for port := uint16(60020); port < 60040; port++ {
		ports = append(ports, map[string]uint16{"guest": port, "host": port - 10000})
	}
	if response := privateCall(d, "POST", "/desktop/connect", map[string]any{"ports": ports}); response.Code != 200 {
		t.Fatal("complete fixed allocation rejected", response.Code, response.Body)
	}
	lease := acquireUnit(t, d, control.Human, "disconnected-sdk")
	if err := d.onAcquire(lease.Session, lease); err == nil {
		t.Fatal("disconnected Neko viewer was bound to ownership")
	}
	if d.localLease.Token != "" {
		t.Fatal("failed native binding published local ownership")
	}
}

// TestPrivateViewerTicketCapacityIsBounded verifies private viewer ticket capacity is bounded.
func TestPrivateViewerTicketCapacityIsBounded(t *testing.T) {
	d, _, _, _ := newUnitDesktop(t)
	for range 8 {
		if response := privateCall(d, "POST", "/desktop/connect", nil); response.Code != 200 {
			t.Fatal(response.Code)
		}
	}
	if response := privateCall(d, "POST", "/desktop/connect", nil); response.Code != 503 {
		t.Fatal("unbounded ticket creation", response.Code)
	}
}
