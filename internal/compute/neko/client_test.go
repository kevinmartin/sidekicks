package neko

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/compute/control"
)

type fakeNeko struct {
	server            *httptest.Server
	mu                sync.Mutex
	calls             []string
	frames            []message
	init              []byte
	failPath          string
	loginBody         string
	shot              []byte
	admin             *websocket.Conn
	viewer            *websocket.Conn
	viewerDialStarted chan struct{}
	viewerDialRelease chan struct{}
	blockPath         string
	pathStarted       chan struct{}
	pathRelease       chan struct{}
}

func newFake(t *testing.T) *fakeNeko {
	t.Helper()
	f := &fakeNeko{init: []byte(`{"event":"system/init","payload":{"screen_size":{"width":1280,"height":720},"settings":{"locked_controls":true,"implicit_hosting":false}}}`)}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}
func (f *fakeNeko) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	fail := f.failPath == r.URL.Path
	loginBody := f.loginBody
	shot := f.shot
	block := f.blockPath == r.URL.Path
	started, release := f.pathStarted, f.pathRelease
	f.mu.Unlock()
	if block {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
	}
	if fail {
		http.Error(w, "secret never exposed", http.StatusServiceUnavailable)
		return
	}
	if r.URL.Path == "/api/login" {
		if loginBody != "" {
			_, _ = w.Write([]byte(loginBody))
			return
		}
		var b map[string]string
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			w.WriteHeader(400)
			return
		}
		if b["username"] == "controller" && b["password"] == "private-admin" {
			_, _ = w.Write([]byte(`{"id":"admin-id","token":"admin-token","profile":{"is_admin":true}}`))
			return
		}
		if strings.HasPrefix(b["username"], "viewer-") && b["password"] == "private-view" {
			_, _ = w.Write([]byte(`{"id":"viewer-id","token":"viewer-token","profile":{"can_login":true,"can_connect":true,"can_watch":true,"can_host":false}}`))
			return
		}
		w.WriteHeader(401)
		return
	}
	if r.URL.Path == "/api/ws" {
		if r.URL.Query().Get("token") == "viewer-token" {
			f.mu.Lock()
			started, release := f.viewerDialStarted, f.viewerDialRelease
			f.viewerDialStarted = nil
			f.viewerDialRelease = nil
			f.mu.Unlock()
			if started != nil {
				close(started)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
		}
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			return
		}
		defer ws.CloseNow()
		f.mu.Lock()
		if r.URL.Query().Get("token") == "admin-token" {
			f.admin = ws
		} else {
			f.viewer = ws
		}
		init := f.init
		f.mu.Unlock()
		if init == nil {
			return
		}
		_ = ws.Write(r.Context(), websocket.MessageText, init)
		for {
			_, b, e := ws.Read(r.Context())
			if e != nil {
				return
			}
			var m message
			if json.Unmarshal(b, &m) != nil {
				return
			}
			f.mu.Lock()
			f.frames = append(f.frames, m)
			f.mu.Unlock()
		}
	}
	if r.Header.Get("Authorization") == "Bearer viewer-token" && (r.URL.Path == "/api/whoami" || r.URL.Path == "/api/room/screen" || r.URL.Path == "/api/room/screen/configurations") {
		_, _ = w.Write([]byte(`{"id":"viewer-id","profile":{"is_admin":false}}`))
		return
	}
	if r.Header.Get("Authorization") != "Bearer admin-token" {
		w.WriteHeader(401)
		return
	}
	if r.URL.Path == "/api/room/screen/shot.jpg" {
		_, _ = w.Write(shot)
		return
	}
	w.WriteHeader(204)
}

// TestConnectDoesNotPublishReadinessBeforeReset verifies initialization remains private.
func TestConnectDoesNotPublishReadinessBeforeReset(t *testing.T) {
	f := newFake(t)
	f.blockPath = "/api/room/control/reset"
	f.pathStarted = make(chan struct{}, 1)
	f.pathRelease = make(chan struct{})
	c, err := New(Config{Endpoint: f.server.URL, AdminUsername: "controller", AdminPassword: "private-admin", ViewerPassword: "private-view"})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		result <- c.Connect(ctx)
	}()
	select {
	case <-f.pathStarted:
	case <-time.After(time.Second):
		t.Fatal("reset did not start")
	}
	if c.Ready() {
		t.Fatal("readiness published before reset completed")
	}
	if _, err := c.CreateViewer(context.Background(), "viewer"); err == nil {
		t.Fatal("viewer admitted before reset completed")
	}
	close(f.pathRelease)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if !c.Ready() {
		t.Fatal("readiness not published after reset")
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestCloseReportsDisconnectResetFailure verifies cleanup failures remain observable.
func TestCloseReportsDisconnectResetFailure(t *testing.T) {
	c, f := connected(t)
	f.mu.Lock()
	f.failPath = "/api/room/control/reset"
	ws := f.admin
	f.mu.Unlock()
	_ = ws.CloseNow()
	wait(t, func() bool {
		return !c.Ready()
	})
	if err := c.Close(context.Background()); err == nil {
		t.Fatal("unconfirmed disconnect reset reported success")
	}
}
func (f *fakeNeko) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.frames)
}
func (f *fakeNeko) events() []message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]message(nil), f.frames...)
}
func connected(t *testing.T) (*Client, *fakeNeko) {
	t.Helper()
	f := newFake(t)
	c, err := New(Config{Endpoint: f.server.URL, AdminUsername: "controller", AdminPassword: "private-admin", ViewerPassword: "private-view"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err = c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = c.Close(ctx)
	})
	return c, f
}
func wait(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not observed")
}
func gateAgent(t *testing.T, c *Client) (*control.Manager, control.Credentials) {
	t.Helper()
	g := control.New(c)
	l, err := g.Acquire(context.Background(), control.Request{Owner: control.Agent, Session: "test", Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = g.Close(context.Background())
	})
	return g, control.Credentials{Session: "test", Token: l.Token}
}

// TestEndpointAndCredentials verifies endpoint and credentials.
func TestEndpointAndCredentials(t *testing.T) {
	for _, endpoint := range []string{"https://127.0.0.1:1", "http://localhost:1", "http://example.com", "http://127.0.0.1:1/x", "http://u:p@127.0.0.1", "http://127.0.0.1?token=x", "http://127.0.0.1/#x", "http://[::1]:0", "http://["} {
		t.Run(endpoint, func(t *testing.T) {
			_, err := New(Config{endpoint, "controller", "secret", "view"})
			if err == nil {
				t.Fatal("accepted endpoint")
			}
		})
	}
	for _, cfg := range []Config{{Endpoint: "http://127.0.0.1"}, {Endpoint: "http://127.0.0.1", AdminUsername: strings.Repeat("a", 129), AdminPassword: "s", ViewerPassword: "v"}} {
		if _, err := New(cfg); err == nil {
			t.Fatal("accepted credentials")
		}
	}
	if _, err := New(Config{"http://[::1]:8080", "controller", "s", "v"}, WithHTTPClient(nil)); err == nil {
		t.Fatal("accepted nil HTTP")
	}
}

// TestConnectResetHeartbeatAndDisconnect verifies connect reset heartbeat and disconnect.
func TestConnectResetHeartbeatAndDisconnect(t *testing.T) {
	c, f := connected(t)
	if !c.Ready() {
		t.Fatal("not ready")
	}
	if w, h := c.Size(); w != 1280 || h != 720 {
		t.Fatal(w, h)
	}
	if err := c.Connect(context.Background()); err == nil {
		t.Fatal("double connect")
	}
	f.mu.Lock()
	ws := f.admin
	f.mu.Unlock()
	_ = ws.Write(context.Background(), websocket.MessageText, []byte(`{"event":"system/heartbeat"}`))
	wait(t, func() bool {
		return f.count() == 1
	})
	if f.events()[0].Event != "client/heartbeat" {
		t.Fatal(f.events())
	}
	_ = ws.Write(context.Background(), websocket.MessageText, []byte(`{"event":"screen/updated","payload":{"width":1024,"height":576}}`))
	wait(t, func() bool {
		w, h := c.Size()
		return w == 1024 && h == 576
	})
	_ = ws.Write(context.Background(), websocket.MessageText, []byte(`{"event":"screen/updated","payload":{"width":99999,"height":576}}`))
	wait(t, func() bool {
		return !c.Ready()
	})
	if err := c.Reset(context.Background()); err == nil {
		t.Fatal("reset after disconnect")
	}
}

// TestLoginAndInitializationFailure verifies login and initialization failure.
func TestLoginAndInitializationFailure(t *testing.T) {
	for _, tc := range []struct{ name, login, init, fail string }{{"status", "", "", "/api/login"}, {"malformed", "{bad", "", ""}, {"missingtoken", "{}", "", ""}, {"wronginit", "", `{"event":"control/take"}`, ""}, {"badpayload", "", `{"event":"system/init","payload":"wrong"}`, ""}, {"badsize", "", `{"event":"system/init","payload":{"screen_size":{"width":99999,"height":720}}}`, ""}, {"resetfailure", "", "", "/api/room/control/reset"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			f.loginBody = tc.login
			f.failPath = tc.fail
			if tc.init != "" {
				f.init = []byte(tc.init)
			}
			c, err := New(Config{f.server.URL, "controller", "private-admin", "private-view"})
			if err != nil {
				t.Fatal(err)
			}
			err = c.Connect(context.Background())
			if err == nil {
				_ = c.Close(context.Background())
				t.Fatal("expected initialization rejection")
			}
			if strings.Contains(err.Error(), "private-admin") || strings.Contains(err.Error(), "admin-token") {
				t.Fatal("credential leaked")
			}
			if c.Ready() {
				t.Fatal("false ready")
			}
		})
	}
}

// TestCaptureBoundsAndFreshJPEG verifies capture bounds and fresh jpeg.
func TestCaptureBoundsAndFreshJPEG(t *testing.T) {
	c, f := connected(t)
	img := image.NewRGBA(image.Rect(0, 0, 80, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 20, 255})
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, nil)
	f.mu.Lock()
	f.shot = b.Bytes()
	f.mu.Unlock()
	shot, err := c.Capture(context.Background(), 20, 20)
	if err != nil {
		t.Fatal(err)
	}
	if shot.Width != 20 || shot.Height != 10 || shot.SourceWidth != 80 || len(shot.JPEG) == 0 {
		t.Fatal(shot)
	}
	shot, err = c.Capture(context.Background(), 1920, 5)
	if err != nil || shot.Width != 10 || shot.Height != 5 {
		t.Fatal(shot, err)
	}
	if _, err = c.Capture(context.Background(), 0, 10); err == nil {
		t.Fatal("invalid bounds")
	}
	f.mu.Lock()
	f.shot = []byte("notjpeg")
	f.mu.Unlock()
	if _, err = c.Capture(context.Background(), 20, 20); err == nil {
		t.Fatal("invalid JPEG")
	}
	f.mu.Lock()
	f.shot = bytes.Repeat([]byte{'x'}, 4*1024*1024+1)
	f.mu.Unlock()
	if _, err = c.Capture(context.Background(), 20, 20); err == nil {
		t.Fatal("oversized JPEG")
	}
}

// TestNoCredentialRedirect verifies no credential redirect.
func TestNoCredentialRedirect(t *testing.T) {
	c, _ := connected(t)
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(204)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	c.base, _ = urlParse(source.URL)
	_, err := c.authenticated(context.Background(), http.MethodPost, "/api/room/control/take", nil)
	if err == nil || called {
		t.Fatal("redirect followed", err, called)
	}
}
func urlParse(s string) (*url.URL, error) {
	return url.Parse(s)
}

// TestUnsafeUpstreamRolesAndSettingsFailClosed verifies unsafe upstream roles and settings fail closed.
func TestUnsafeUpstreamRolesAndSettingsFailClosed(t *testing.T) {
	t.Run("nonadministrator controller", func(t *testing.T) {
		f := newFake(t)
		f.loginBody = `{"id":"wrong","token":"not-admin","profile":{"is_admin":false}}`
		c, err := New(Config{f.server.URL, "controller", "private-admin", "private-view"})
		if err != nil {
			t.Fatal(err)
		}
		if err = c.Connect(context.Background()); err == nil || c.Ready() {
			t.Fatal("nonadmin controller accepted")
		}
	})
	for _, settings := range []string{`{"locked_controls":false,"implicit_hosting":false}`, `{"locked_controls":true,"implicit_hosting":true}`, `{}`} {
		t.Run(settings, func(t *testing.T) {
			f := newFake(t)
			f.init = []byte(`{"event":"system/init","payload":{"screen_size":{"width":1280,"height":720},"settings":` + settings + `}}`)
			c, err := New(Config{f.server.URL, "controller", "private-admin", "private-view"})
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Connect(context.Background()); err == nil || c.Ready() {
				t.Fatal("unsafe controls accepted")
			}
		})
	}
	t.Run("viewer privilege", func(t *testing.T) {
		c, f := connected(t)
		for _, profile := range []string{`{"is_admin":true,"can_login":true,"can_connect":true,"can_watch":true}`, `{"can_login":true,"can_connect":true,"can_watch":true,"can_share_media":true}`, `{"can_login":true,"can_connect":true,"can_watch":true,"can_access_clipboard":true}`, `{"can_login":true,"can_connect":true,"can_watch":true,"can_host":true}`, `{"can_login":true,"can_connect":false,"can_watch":true}`} {
			f.mu.Lock()
			f.loginBody = `{"id":"viewer-id","token":"viewer-token","profile":` + profile + `}`
			f.mu.Unlock()
			if _, err := c.CreateViewer(context.Background(), "unsafe"); err == nil {
				t.Fatal("unsafe viewer accepted", profile)
			}
		}
	})
	t.Run("unlock after connect", func(t *testing.T) {
		c, f := connected(t)
		f.mu.Lock()
		ws := f.admin
		f.mu.Unlock()
		_ = ws.Write(context.Background(), websocket.MessageText, []byte(`{"event":"system/settings","payload":{"settings":{"locked_controls":false,"implicit_hosting":false}}}`))
		wait(t, func() bool {
			return !c.Ready()
		})
	})
}
