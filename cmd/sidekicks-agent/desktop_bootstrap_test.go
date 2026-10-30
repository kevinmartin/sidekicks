package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute/control"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/apps"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/session"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/viewer"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/x11"
)

type bootstrapWindows struct {
	*unitWindows
	fail bool
}

// List implements the test fixture's List method.
func (w *bootstrapWindows) List(ctx context.Context) ([]apps.Window, error) {
	if w.fail {
		return nil, errors.New("enumeration failed")
	}
	return w.unitWindows.List(ctx)
}

type bootstrapFixture struct {
	base            *desktopWorkload
	backend         *unitX
	windows         *bootstrapWindows
	deps            desktopDependencies
	fail            string
	sites           map[string]http.Handler
	homeToken       string
	stoppedSites    []string
	stoppedChildren []string
	kind            apps.BrowserKind
	tokenWrites     int
}

func bootstrapSetup(t *testing.T) *bootstrapFixture {
	t.Helper()
	base, backend, _, windows := newUnitDesktop(t)
	h := &bootstrapFixture{base: base, backend: backend, windows: &bootstrapWindows{unitWindows: windows}, sites: map[string]http.Handler{}}
	h.deps = desktopDependencies{
		startSession: func(ctx context.Context, cfg session.Config) (*session.Manager, error) {
			if h.fail == "session" {
				return nil, errors.New("session failed")
			}
			if cfg.Home != "" || cfg.Assets != "" {
				t.Fatal("production session defaults changed")
			}
			return base.session, nil
		},
		openX11: func(ctx context.Context, display, cookie string) (*x11.Driver, error) {
			if h.fail == "x11" {
				return nil, errors.New("x11 failed")
			}
			if ctx.Err() != nil || display != ":99" || cookie != "" {
				t.Fatal("non-private X11 bootstrap")
			}
			return base.driver, nil
		},
		newWindows: func(display, cookie string) (apps.Windows, error) {
			if h.fail == "windows" {
				return nil, errors.New("windows failed")
			}
			if display != ":99" || cookie != "" {
				t.Fatal("window backend selected external display")
			}
			return h.windows, nil
		},
		newLauncher: func(cfg apps.Config) (*apps.Launcher, error) {
			if h.fail == "launcher" {
				return nil, errors.New("launcher failed")
			}
			h.kind = cfg.Browser
			if cfg.HomeURL != "http://127.0.0.1:8081/home" || cfg.HomeDir != session.DefaultHome {
				t.Fatal("managed app paths changed")
			}
			for _, value := range cfg.Environment {
				if strings.Contains(value, orchestrationToken) {
					t.Fatal("private token reached app environment")
				}
				if strings.HasPrefix(value, "HOME=") {
					cfg.HomeDir = strings.TrimPrefix(value, "HOME=")
				}
			}
			cfg.LaunchTimeout = 5 * time.Millisecond
			cfg.PollInterval = time.Millisecond
			return apps.New(cfg)
		},
		writeFile: func(path string, data []byte, mode os.FileMode) error {
			if path != "/run/sidekicks-home.token" || mode != 0600 || len(data) != 64 || string(data) == orchestrationToken {
				t.Fatal("desktop capability was not independently scoped")
			}
			h.tokenWrites++
			h.homeToken = string(data)
			if h.fail == "token" {
				return errors.New("token storage failed")
			}
			return nil
		},
		startSite: func(_ context.Context, address string, handler http.Handler) (func(), error) {
			if (address == "127.0.0.1:8081" && h.fail == "home-site") || (address == "0.0.0.0:6080" && h.fail == "viewer-site") {
				return nil, errors.New("site failed")
			}
			h.sites[address] = handler
			if address == "0.0.0.0:6080" {
				if h.fail == "browser-launch" {
					h.windows.fail = true
				}
				if h.fail == "browser-map" {
					h.windows.mu.Lock()
					for i := range h.windows.windows {
						if h.windows.windows[i].Class == "SidekicksBrowser" {
							h.windows.windows[i].Mapped = false
						}
					}
					h.windows.mu.Unlock()
				}
			}
			return func() {
				h.stoppedSites = append(h.stoppedSites, address)
			}, nil
		},
		startChild: func(_ context.Context, env []string, path string, args ...string) (func(), <-chan struct{}, error) {
			if h.fail == "child" {
				return nil, nil, errors.New("backend child failed")
			}
			if path != "/usr/local/bin/x11vnc" || !strings.Contains(strings.Join(args, " "), "-nowireframe") {
				t.Fatal("wrong guest video backend")
			}
			for _, value := range env {
				if strings.Contains(value, orchestrationToken) {
					t.Fatal("backend inherited private machine token")
				}
			}
			done := make(chan struct{})
			var once sync.Once
			return func() {
				once.Do(func() {
					h.stoppedChildren = append(h.stoppedChildren, path)
					close(done)
				})
			}, done, nil
		},
		waitTCP: func(_ context.Context, address string) error {
			if address != "127.0.0.1:5900" {
				t.Fatal("backend readiness used non-private target")
			}
			if h.fail == "backend-ready" {
				return errors.New("backend not ready")
			}
			return nil
		},
		prepareNeko: func(d *desktopWorkload, _ context.Context, token string) error {
			if token != orchestrationToken {
				t.Fatal("instance credential changed")
			}
			if h.fail == "neko" {
				d.stopChildren = append(d.stopChildren, func() {
					h.stoppedChildren = append(h.stoppedChildren, "partial-neko")
				})
				return errors.New("neko startup failed")
			}
			var err error
			d.viewer, err = viewer.New(viewer.Config{Gate: d.gate, Driver: d.driver, OnAcquire: d.onAcquire})
			return err
		},
	}
	return h
}
func homeRequest(h http.Handler, method, path, body, capability string, remote, host, origin string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = remote
	request.Host = host
	request.Header.Set("Content-Type", "application/json")
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if capability != "" {
		request.Header.Set("X-Sidekicks-Desktop", capability)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}

// TestDesktopBootstrapCleansEveryPartiallyOwnedStage verifies desktop bootstrap cleans every partially owned stage.
func TestDesktopBootstrapCleansEveryPartiallyOwnedStage(t *testing.T) {
	for _, point := range []string{"session", "x11", "windows", "launcher", "token", "home-site", "child", "backend-ready", "viewer-site", "browser-launch", "browser-map", "unsupported", "neko"} {
		t.Run(point, func(t *testing.T) {
			h := bootstrapSetup(t)
			h.fail = point
			kind := "desktop-novnc"
			if point == "unsupported" {
				kind = "unsupported"
			}
			if point == "neko" {
				kind = "desktop-neko"
			}
			desktop, err := prepareDesktopWith(context.Background(), kind, orchestrationToken, h.deps)
			if err == nil || desktop != nil {
				t.Fatal("failed stage was accepted", point)
			}
			if point == "session" {
				if h.base.session.Ready() != nil {
					t.Fatal("failed factory closed a session it did not return")
				}
			} else if h.base.session.Ready() == nil {
				t.Fatal("partially owned session leaked", point)
			}
			h.backend.mu.Lock()
			closed := h.backend.closed
			h.backend.mu.Unlock()
			if point != "session" && point != "x11" && !closed {
				t.Fatal("owned X11 connection leaked", point)
			}
			if len(h.stoppedSites) != len(h.sites) {
				t.Fatal("owned site leaked", point, h.stoppedSites)
			}
			if point == "backend-ready" || point == "viewer-site" || point == "browser-launch" || point == "browser-map" {
				if len(h.stoppedChildren) != 1 {
					t.Fatal("owned backend child leaked", point, h.stoppedChildren)
				}
			}
			if point == "neko" && (len(h.stoppedChildren) != 1 || h.stoppedChildren[0] != "partial-neko") {
				t.Fatal("partially started Neko backend leaked")
			}
		})
	}
}

// TestDesktopBootstrapPrivateHomeAndHumanLaunchContract verifies desktop bootstrap private home and human launch contract.
func TestDesktopBootstrapPrivateHomeAndHumanLaunchContract(t *testing.T) {
	h := bootstrapSetup(t)
	desktop, err := prepareDesktopWith(context.Background(), "desktop-novnc", orchestrationToken, h.deps)
	if err != nil {
		t.Fatal(err)
	}
	defer desktop.close()
	if h.kind != apps.Chromium || h.tokenWrites != 1 || len(h.sites) != 2 || desktop.ready() != nil {
		t.Fatal("desktop startup contract incomplete")
	}
	site := h.sites["127.0.0.1:8081"]
	response := homeRequest(site, "GET", "/home", "", "", "127.0.0.1:5", "127.0.0.1:8081", "")
	if response.Code != 200 || strings.Contains(response.Body.String(), "__SIDEKICKS_DESKTOP_TOKEN__") || !strings.Contains(response.Body.String(), h.homeToken) || strings.Contains(response.Body.String(), orchestrationToken) {
		t.Fatal("Home capability substitution leaked machine authority")
	}
	for _, test := range []struct{ method, remote, host string }{{"POST", "127.0.0.1:5", "127.0.0.1:8081"}, {"GET", "127.0.0.2:5", "127.0.0.1:8081"}, {"GET", "127.0.0.1:5", "evil.test"}} {
		if response = homeRequest(site, test.method, "/home", "", "", test.remote, test.host, ""); response.Code != 403 {
			t.Fatal("Home crossed guest-local authority", test, response.Code)
		}
	}
	body := `{"app":"browser"}`
	if response = homeRequest(site, "POST", "/desktop/launch", body, h.homeToken, "127.0.0.1:5", "127.0.0.1:8081", "http://127.0.0.1:8081"); response.Code != 403 {
		t.Fatal("unowned Home launched an app")
	}
	human := acquireUnit(t, desktop, control.Human, "viewer-home")
	if err = desktop.onAcquire(human.Session, human); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ token, remote, host, origin string }{{"wrong", "127.0.0.1:5", "127.0.0.1:8081", "http://127.0.0.1:8081"}, {h.homeToken, "127.0.0.2:5", "127.0.0.1:8081", "http://127.0.0.1:8081"}, {h.homeToken, "127.0.0.1:5", "127.0.0.1:8081", "http://evil.test"}} {
		if response = homeRequest(site, "POST", "/desktop/launch", body, test.token, test.remote, test.host, test.origin); response.Code != 403 {
			t.Fatal("forged launch authority accepted", response.Code)
		}
	}
	if response = homeRequest(site, "POST", "/desktop/launch", `{"app":"browser","unknown":true}`, h.homeToken, "127.0.0.1:5", "127.0.0.1:8081", "http://127.0.0.1:8081"); response.Code != 400 {
		t.Fatal("unbounded Home launch body accepted")
	}
	if response = homeRequest(site, "POST", "/desktop/launch", body, h.homeToken, "127.0.0.1:5", "127.0.0.1:8081", "http://127.0.0.1:8081"); response.Code != 200 {
		t.Fatal("authorized human Home launch unavailable", response.Code, response.Body)
	}
	if err = desktop.gate.Release(context.Background(), control.Credentials{Session: human.Session, Token: human.Token}); err != nil {
		t.Fatal(err)
	}
	// A real browser close removes the app client, not the desktop session. The
	// session/backend health callback must remain ready in that intentional state.
	h.windows.mu.Lock()
	h.windows.windows = h.windows.windows[1:]
	h.windows.mu.Unlock()
	if err = desktop.ready(); err != nil {
		t.Fatal("browser close was mistaken for desktop failure", err)
	}
}

// TestDesktopBootstrapRespectsBackendChoiceAndNoBrowserOverride verifies desktop bootstrap respects backend choice and no browser override.
func TestDesktopBootstrapRespectsBackendChoiceAndNoBrowserOverride(t *testing.T) {
	t.Setenv("SIDEKICKS_DESKTOP_START_BROWSER", "0")
	h := bootstrapSetup(t)
	h.windows.fail = true
	desktop, err := prepareDesktopWith(context.Background(), "desktop-neko", orchestrationToken, h.deps)
	if err != nil {
		t.Fatal(err)
	}
	defer desktop.close()
	if h.kind != apps.Firefox || len(h.sites) != 2 {
		t.Fatal("Neko desktop selected a different browser contract")
	}
	if len(desktop.childDone) != 0 {
		t.Fatal("Neko path started a noVNC backend")
	}
}
