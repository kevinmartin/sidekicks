package apps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop"
	desktopx11 "github.com/kevinmartin/sidekicks/internal/compute/desktop/x11"
)

// TestRealGuestApps is opt-in and destructive only to its own managed app
// windows in a newly provisioned disposable Linux test guest. It never discovers
// a display or falls back to the user's desktop. The coordinator owns lifecycle.
func TestRealGuestApps(t *testing.T) {
	display := os.Getenv("SIDEKICKS_APPS_TEST_DISPLAY")
	if display == "" {
		t.Skip("requires explicit disposable guest display and home")
	}
	if runtime.GOOS != "linux" || os.Getuid() != 1000 {
		t.Fatal("real apps test requires disposable Linux guest UID1000")
	}
	home := os.Getenv("SIDEKICKS_APPS_TEST_HOME")
	if home == "" || !filepath.IsAbs(home) || filepath.Clean(home) != home || home == "/" {
		t.Fatal("explicit disposable guest home required")
	}
	browser, err := realGuestBrowser(os.Getenv("SIDEKICKS_APPS_TEST_BROWSER"))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewX11(display, os.Getenv("SIDEKICKS_APPS_TEST_COOKIE"))
	if err != nil {
		t.Fatal(err)
	}
	homeURL := os.Getenv("SIDEKICKS_APPS_TEST_HOME_URL")
	if homeURL == "" {
		homeURL = "http://127.0.0.1:8080/home"
	}
	environment := append([]string(nil), os.Environ()...)
	environment = append(environment, "DISPLAY="+display)
	launcher, err := New(Config{Browser: browser, HomeURL: homeURL, HomeDir: home, Backend: backend, Environment: environment, LaunchTimeout: 10 * time.Second, PollInterval: 25 * time.Millisecond, ChromiumNoSandbox: false})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	input, err := desktopx11.Open(ctx, display, os.Getenv("SIDEKICKS_APPS_TEST_COOKIE"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := input.Reset(cleanup); err != nil {
			t.Errorf("release test input: %v", err)
		}
		if err := input.Close(); err != nil {
			t.Errorf("close test input: %v", err)
		}
	}()
	// This is a fresh test guest contract, not permission to close existing work.
	initial, err := backend.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range initial {
		for _, app := range []App{Browser, Terminal, Files} {
			if matches(app, w) {
				t.Fatalf("refusing occupied test display: existing %s client %#x", app, w.ID)
			}
		}
	}
	opened := map[App]uint32{}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		clients, e := backend.List(cleanup)
		if e != nil {
			t.Errorf("final window observation: %v", e)
			return
		}
		for _, w := range clients {
			for app := range opened {
				if matches(app, w) {
					if e = realClose(cleanup, backend, w.ID); e != nil {
						t.Errorf("close owned %s: %v", app, e)
					}
				}
			}
		}
	}()
	for _, app := range []App{Browser, Terminal, Files} {
		opened[app] = 0 // any later matching window belongs to this fresh test launch.
		result, e := launcher.Launch(ctx, app)
		if e != nil {
			t.Fatalf("launch %s: %v", app, e)
		}
		mapped, e := launcher.WaitFor(ctx, app)
		if e != nil {
			t.Fatalf("mapped %s: %v", app, e)
		}
		w := realWaitWindow(t, ctx, backend, app)
		if mapped.ID != w.ID {
			t.Fatalf("%s mapped client changed %#x -> %#x", app, mapped.ID, w.ID)
		}
		if (result.State != "opened" || result.Window != w.ID) && result.State != "launching" {
			t.Fatalf("%s launch result %+v vs actual %#x", app, result, w.ID)
		}
		opened[app] = w.ID
		pid, e := realPID(ctx, backend, w.ID)
		if e != nil || pid == 0 {
			t.Fatalf("%s actual PID: %d %v", app, pid, e)
		}
		t.Logf("opened app=%s client=%#x pid=%d class=%q role=%q", app, w.ID, pid, w.Class, w.Role)
	}
	// An actual browser title provides loaded-page evidence separate from the
	// launch argv. This does not assert New Tab behavior or physical input.
	titleWant := os.Getenv("SIDEKICKS_APPS_TEST_TITLE")
	if titleWant == "" {
		titleWant = "Sidekicks"
	}
	realWait(t, ctx, string(browser)+" loaded managed Home title", func() bool {
		title, e := realTitle(ctx, backend, opened[Browser])
		return e == nil && strings.Contains(title, titleWant)
	})
	title, _ := realTitle(ctx, backend, opened[Browser])
	t.Logf("actual managed %s title=%q", browser, title)
	for _, app := range []App{Browser, Terminal, Files} {
		w := realWaitWindow(t, ctx, backend, app)
		original := w.ID
		pid, _ := realPID(ctx, backend, original)
		before, e := realGeometry(ctx, backend, original)
		if e != nil {
			t.Fatal(e)
		}
		for restore := 0; restore < 2; restore++ {
			if e = backend.Activate(ctx, original); e != nil {
				t.Fatalf("focus %s: %v", app, e)
			}
			time.Sleep(250 * time.Millisecond)
			active, activeErr := realActiveWindow(ctx, backend)
			t.Logf("focus observation app=%s wanted=%#x actual=%#x error=%v", app, original, active, activeErr)
			focusDeadline := time.Now().Add(3 * time.Second)
			focused := false
			for time.Now().Before(focusDeadline) && ctx.Err() == nil {
				active, e := realActiveWindow(ctx, backend)
				if e == nil && active == original {
					focused = true
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !focused {
				t.Logf("focus request target=%#x source=2 timestamp=0", original)
				realFocusDiagnostics(t, ctx, backend, original, opened[Browser])
				if png, captureErr := input.CapturePNG(ctx); captureErr == nil {
					if writeErr := os.WriteFile("/workspace/apps-focus-failure.png", png, 0600); writeErr != nil {
						t.Logf("focus capture write: %v", writeErr)
					}
				} else {
					t.Logf("focus capture: %v", captureErr)
				}
				// This diagnostic is a control, not a substitute for the failed
				// launcher activation. Inputs affect only this disposable guest.
				for attempt := 0; attempt < 2; attempt++ {
					for _, event := range []desktop.Event{{Type: "key", Key: "Alt", Action: "down"}, {Type: "key", Key: "Tab", Action: "down"}, {Type: "key", Key: "Tab", Action: "up"}, {Type: "key", Key: "Alt", Action: "up"}} {
						if e = input.Input(ctx, event); e != nil {
							t.Logf("Alt+Tab diagnostic: %v", e)
							break
						}
					}
					if e = input.Reset(ctx); e != nil {
						t.Logf("Alt+Tab release: %v", e)
						break
					}
					time.Sleep(250 * time.Millisecond)
					actual, activeErr := realActiveWindow(ctx, backend)
					t.Logf("Alt+Tab diagnostic round=%d active=%#x error=%v", attempt+1, actual, activeErr)
					if activeErr == nil && actual == original {
						break
					}
				}
				t.Fatalf("requested %s client was not focused by launcher activation", app)
			}
			for _, event := range []desktop.Event{{Type: "key", Key: "Alt", Action: "down"}, {Type: "key", Key: "F9", Action: "down"}, {Type: "key", Key: "F9", Action: "up"}, {Type: "key", Key: "Alt", Action: "up"}} {
				if e = input.Input(ctx, event); e != nil {
					t.Fatalf("actual minimize shortcut %s: %v", app, e)
				}
			}
			if e = input.Reset(ctx); e != nil {
				t.Fatalf("release minimize shortcut %s: %v", app, e)
			}
			time.Sleep(250 * time.Millisecond)
			observed, observationErr := realIsIconic(ctx, backend, original)
			t.Logf("iconify observation app=%s iconic=%t error=%v", app, observed, observationErr)
			realWait(t, ctx, "actual iconic state", func() bool {
				iconic, e := realIsIconic(ctx, backend, original)
				return e == nil && iconic
			})
			result, e := launcher.Launch(ctx, app)
			if e != nil || result.State != "restored" || result.Window != original {
				t.Fatalf("restore %s: %+v %v", app, result, e)
			}
			realWait(t, ctx, "actual normal mapped state", func() bool {
				iconic, e := realIsIconic(ctx, backend, original)
				return e == nil && !iconic
			})
			after, e := realGeometry(ctx, backend, original)
			if e != nil || after != before {
				t.Fatalf("%s geometry changed %+v -> %+v (%v)", app, before, after, e)
			}
			afterPID, e := realPID(ctx, backend, original)
			if e != nil || afterPID != pid {
				t.Fatalf("%s restore PID changed %d -> %d (%v)", app, pid, afterPID, e)
			}
			realAssertOne(t, ctx, backend, app, original)
			t.Logf("restore app=%s round=%d sameclient=%#x samepid=%d geometry=%+v", app, restore+1, original, pid, after)
		}
		if e = realClose(ctx, backend, original); e != nil {
			t.Fatalf("close %s: %v", app, e)
		}
		realWait(t, ctx, "closed app absent from managed clients", func() bool {
			return realCount(ctx, backend, app) == 0
		})
		// Continue observing absence; a screenshot or one missing frame alone is
		// insufficient to show that a supervisor did not immediately reopen it.
		end := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(end) {
			if realCount(ctx, backend, app) != 0 {
				t.Fatalf("%s reopened without explicit launch", app)
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(25 * time.Millisecond):
			}
		}
		result, e := launcher.Launch(ctx, app)
		if e != nil {
			t.Fatalf("explicit reopen %s: %v", app, e)
		}
		reopened := realWaitWindow(t, ctx, backend, app)
		if (result.State != "opened" || result.Window != reopened.ID) && result.State != "launching" {
			t.Fatalf("%s reopen %+v vs %#x", app, result, reopened.ID)
		}
		realAssertOne(t, ctx, backend, app, reopened.ID)
		opened[app] = reopened.ID
		newPID, e := realPID(ctx, backend, reopened.ID)
		if e != nil || newPID == 0 {
			t.Fatalf("%s reopened PID: %d %v", app, newPID, e)
		}
		t.Logf("close/reopen app=%s absent>=500ms oldclient=%#x newclient=%#x oldpid=%d newpid=%d (numeric XID/PID reuse allowed)", app, original, reopened.ID, pid, newPID)
		if app == Browser {
			// Mapping precedes Firefox startup. Wait for the actual Home title
			// and stable active client before testing a different app's focus.
			var stableSince time.Time
			realWait(t, ctx, "reopened browser Home and stable focus", func() bool {
				actualTitle, titleErr := realTitle(ctx, backend, reopened.ID)
				active, activeErr := realActiveWindow(ctx, backend)
				if titleErr != nil || activeErr != nil || !strings.Contains(actualTitle, titleWant) || active != reopened.ID {
					stableSince = time.Time{}
					return false
				}
				if stableSince.IsZero() {
					stableSince = time.Now()
				}
				return time.Since(stableSince) >= 500*time.Millisecond
			})
			t.Logf("reopened browser Home title and focus stable >=500ms client=%#x", reopened.ID)
		}
	}
	state, e := launcher.State(ctx)
	if e != nil || !state.Ready || len(state.Windows) != 3 {
		t.Fatalf("actual final managed app state %+v %v", state, e)
	}
}

type actualGeometry struct {
	X, Y          int16
	Width, Height uint16
}

func realGeometry(ctx context.Context, x *X11, id uint32) (actualGeometry, error) {
	c, e := x.connect(ctx)
	if e != nil {
		return actualGeometry{}, e
	}
	defer c.close()
	geometry, e := xproto.GetGeometry(c.conn, xproto.Drawable(id)).Reply()
	if e != nil || geometry == nil {
		return actualGeometry{}, ErrUnavailable
	}
	origin, e := xproto.TranslateCoordinates(c.conn, xproto.Window(id), c.root, 0, 0).Reply()
	if e != nil || origin == nil || !origin.SameScreen {
		return actualGeometry{}, ErrUnavailable
	}
	return actualGeometry{origin.DstX, origin.DstY, geometry.Width, geometry.Height}, nil
}
func realPID(ctx context.Context, x *X11, id uint32) (uint32, error) {
	c, e := x.connect(ctx)
	if e != nil {
		return 0, e
	}
	defer c.close()
	a, e := c.atom("_NET_WM_PID")
	if e != nil {
		return 0, e
	}
	p, e := c.property(xproto.Window(id), a, 1)
	if e != nil || p.Format != 32 || p.Type != xproto.AtomCardinal || len(p.Value) != 4 {
		return 0, ErrUnavailable
	}
	return xgb.Get32(p.Value), nil
}
func realTitle(ctx context.Context, x *X11, id uint32) (string, error) {
	c, e := x.connect(ctx)
	if e != nil {
		return "", e
	}
	defer c.close()
	a, e := c.atom("_NET_WM_NAME")
	if e != nil {
		return "", e
	}
	p, e := c.property(xproto.Window(id), a, 4096)
	if e != nil || p.Format != 8 {
		return "", ErrUnavailable
	}
	return string(p.Value), nil
}
func realActiveWindow(ctx context.Context, x *X11) (uint32, error) {
	c, e := x.connect(ctx)
	if e != nil {
		return 0, e
	}
	defer c.close()
	a, e := c.atom("_NET_ACTIVE_WINDOW")
	if e != nil {
		return 0, e
	}
	p, e := xproto.GetProperty(c.conn, false, c.root, a, xproto.GetPropertyTypeAny, 0, 16).Reply()
	if e != nil {
		return 0, fmt.Errorf("active-window property: %w", e)
	}
	if p == nil {
		return 0, fmt.Errorf("active-window property missing")
	}
	return realDecodeActiveWindow(p)
}

func realFocusDiagnostics(t *testing.T, ctx context.Context, x *X11, target, browser uint32) {
	t.Helper()
	c, err := x.connect(ctx)
	if err != nil {
		t.Logf("focus diagnostics connection: %v", err)
		return
	}
	defer c.close()
	if focus, e := xproto.GetInputFocus(c.conn).Reply(); e == nil && focus != nil {
		t.Logf("X input focus=%#x", focus.Focus)
	}
	for _, item := range []struct {
		id   xproto.Window
		name string
	}{
		{c.root, "_NET_ACTIVE_WINDOW"}, {c.root, "_NET_CURRENT_DESKTOP"}, {c.root, "_NET_CLIENT_LIST_STACKING"},
		{xproto.Window(target), "_NET_WM_DESKTOP"}, {xproto.Window(target), "WM_HINTS"}, {xproto.Window(target), "WM_TRANSIENT_FOR"}, {xproto.Window(target), "_NET_WM_STATE"},
		{xproto.Window(browser), "WM_TRANSIENT_FOR"}, {xproto.Window(browser), "_NET_WM_STATE"},
	} {
		a, e := c.atom(item.name)
		if e != nil {
			t.Logf("property %s window=%#x atom error=%v", item.name, item.id, e)
			continue
		}
		p, e := xproto.GetProperty(c.conn, false, item.id, a, xproto.GetPropertyTypeAny, 0, 64).Reply()
		if e != nil || p == nil {
			t.Logf("property %s window=%#x error=%v", item.name, item.id, e)
			continue
		}
		t.Logf("property %s window=%#x format=%d type=%d remaining=%d value=%x", item.name, item.id, p.Format, p.Type, p.BytesAfter, p.Value)
	}
}

// Xfwm writes the active WINDOW followed by its timestamp; other WMs may
// expose only the EWMH WINDOW. More than two elements remains invalid.
func realDecodeActiveWindow(p *xproto.GetPropertyReply) (uint32, error) {
	return activeWindow(p)
}
func realIsIconic(ctx context.Context, x *X11, id uint32) (bool, error) {
	c, e := x.connect(ctx)
	if e != nil {
		return false, e
	}
	defer c.close()
	a, e := c.atom("WM_STATE")
	if e != nil {
		return false, e
	}
	p, e := c.property(xproto.Window(id), a, 2)
	if e != nil || p.Format != 32 || p.Type != a || len(p.Value) < 4 {
		return false, ErrUnavailable
	}
	attrs, e := xproto.GetWindowAttributes(c.conn, xproto.Window(id)).Reply()
	if e != nil || attrs == nil {
		return false, ErrUnavailable
	}
	state := xgb.Get32(p.Value)
	if state == 3 && attrs.MapState == xproto.MapStateUnmapped {
		return true, nil
	}
	if state == 1 && attrs.MapState == xproto.MapStateViewable {
		return false, nil
	}
	return false, fmt.Errorf("intermediate wm state=%d map=%d", state, attrs.MapState)
}
func realClose(ctx context.Context, x *X11, id uint32) error {
	c, e := x.connect(ctx)
	if e != nil {
		return e
	}
	defer c.close()
	protocols, e := c.atom("WM_PROTOCOLS")
	if e != nil {
		return e
	}
	deleteWindow, e := c.atom("WM_DELETE_WINDOW")
	if e != nil {
		return e
	}
	p, e := c.property(xproto.Window(id), protocols, 128)
	if e != nil || p.Format != 32 || p.Type != xproto.AtomAtom {
		return ErrUnavailable
	}
	supported := false
	for i := 0; i+4 <= len(p.Value); i += 4 {
		if xproto.Atom(xgb.Get32(p.Value[i:])) == deleteWindow {
			supported = true
			break
		}
	}
	if !supported {
		return fmt.Errorf("window %#x lacks wm_delete_window", id)
	}
	event := xproto.ClientMessageEvent{Format: 32, Window: xproto.Window(id), Type: protocols, Data: xproto.ClientMessageDataUnionData32New([]uint32{uint32(deleteWindow), 0, 0, 0, 0})}
	return xproto.SendEventChecked(c.conn, false, xproto.Window(id), 0, string(event.Bytes())).Check()
}
func realWait(t *testing.T, ctx context.Context, description string, fn func() bool) {
	t.Helper()
	end := time.Now().Add(15 * time.Second)
	for !fn() {
		if e := ctx.Err(); e != nil {
			t.Fatalf("%s: %v", description, e)
		}
		if time.Now().After(end) {
			t.Fatalf("timeout: %s", description)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
func realCount(ctx context.Context, x *X11, app App) int {
	windows, e := x.List(ctx)
	if e != nil {
		return -1
	}
	count := 0
	for _, w := range windows {
		if matches(app, w) {
			count++
		}
	}
	return count
}
func realWaitWindow(t *testing.T, ctx context.Context, x *X11, app App) Window {
	t.Helper()
	var found Window
	realWait(t, ctx, "one managed "+string(app)+" window", func() bool {
		windows, e := x.List(ctx)
		if e != nil {
			return false
		}
		n := 0
		for _, w := range windows {
			if matches(app, w) {
				found = w
				n++
			}
		}
		return n == 1
	})
	return found
}
func realAssertOne(t *testing.T, ctx context.Context, x *X11, app App, id uint32) {
	t.Helper()
	w := realWaitWindow(t, ctx, x, app)
	if w.ID != id {
		t.Fatalf("%s unexpected client %#x vs %#x", app, w.ID, id)
	}
}

// Empty retains the original Chromium contract. Unknown spellings never select
// a fallback browser in an explicitly requested integration run.
func realGuestBrowser(value string) (BrowserKind, error) {
	switch value {
	case "", "chromium":
		return Chromium, nil
	case "firefox":
		return Firefox, nil
	default:
		return "", fmt.Errorf("unsupported real guest browser %q", value)
	}
}
