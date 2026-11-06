package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kevinmartin/sidekicks/internal/compute/control"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop"
	"github.com/kevinmartin/sidekicks/internal/compute/neko"
)

// TestNekoDefaultServerCannotGrantViewerNativeHost verifies neko default server cannot grant viewer native host.
func TestNekoDefaultServerCannotGrantViewerNativeHost(t *testing.T) {
	raw, err := json.Marshal(nekoServerConfig("unit-admin", "unit-viewer"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Server struct {
			Bind  string `json:"bind"`
			Proxy *bool  `json:"proxy"`
		}
		Session struct {
			LockedControls  bool `json:"locked_controls"`
			ImplicitHosting bool `json:"implicit_hosting"`
		}
		Member struct {
			Multiuser struct {
				Profile struct {
					CanHost            bool `json:"can_host"`
					CanWatch           bool `json:"can_watch"`
					IsAdmin            bool `json:"is_admin"`
					CanAccessClipboard bool `json:"can_access_clipboard"`
				} `json:"user_profile"`
			} `json:"multiuser"`
		}
	}
	if json.Unmarshal(raw, &config) != nil {
		t.Fatal("invalid server configuration")
	}
	p := config.Member.Multiuser.Profile
	if p.CanHost || p.IsAdmin || p.CanAccessClipboard || !p.CanWatch || !config.Session.LockedControls || config.Session.ImplicitHosting || config.Server.Bind != "127.0.0.1:8082" || config.Server.Proxy == nil || *config.Server.Proxy {
		t.Fatalf("unsafe default server configuration: %s", raw)
	}
	if _, native := any(nekoReset{}).(control.NativeDriver); native {
		t.Fatal("default Neko reset wrapper must not implement native ownership transfer")
	}
}

// TestNekoPublicViewerUsesSynchronizedGoX11Default verifies neko public viewer uses synchronized go x11 default.
func TestNekoPublicViewerUsesSynchronizedGoX11Default(t *testing.T) {
	d, backend, _, _ := newUnitDesktop(t)
	client, err := neko.New(neko.Config{Endpoint: "http://127.0.0.1:8082", AdminUsername: "controller", AdminPassword: "unit-admin", ViewerPassword: "unit-viewer"})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := neko.NewHandler(client, d.gate, neko.HandlerOptions{Token: orchestrationToken})
	if err != nil {
		t.Fatal(err)
	}
	config := d.nekoViewerConfig(handler)
	if config.NativeViewerID != nil || config.Input != nil || config.Gate != d.gate || config.Driver != d.driver || config.OnAcquire == nil {
		t.Fatal("Neko viewer bypasses synchronized default X11 dispatch")
	}
	lease := acquireUnit(t, d, control.Human, "synchronized-viewer")
	err = d.gate.DispatchHumanGated(context.Background(), control.Credentials{Session: lease.Session, Token: lease.Token}, func(ctx context.Context) error {
		return config.Driver.Input(ctx, desktop.Event{Type: "key", Key: "Control", Action: "down"})
	})
	if err != nil || !backend.held() {
		t.Fatal("default Go X11 path did not apply input", err)
	}
	if err = d.gate.Release(context.Background(), control.Credentials{Session: lease.Session, Token: lease.Token}); err != nil || backend.held() {
		t.Fatal("Go X11 reset did not clear input", err)
	}
}

// TestNekoDisabledUpstreamPathsDoNotExposeInputOrOptIn verifies neko disabled upstream paths do not expose input or opt in.
func TestNekoDisabledUpstreamPathsDoNotExposeInputOrOptIn(t *testing.T) {
	d, _, _, _ := newUnitDesktop(t)
	client, err := neko.New(neko.Config{Endpoint: "http://127.0.0.1:8082", AdminUsername: "controller", AdminPassword: "unit-admin", ViewerPassword: "unit-viewer"})
	if err != nil {
		t.Fatal(err)
	}
	//lint:ignore SA1019 Verify the deprecated opt-in fails closed.
	if _, err = neko.NewHandler(client, d.gate, neko.HandlerOptions{Token: orchestrationToken, EnableNative: true}); !errors.Is(err, neko.ErrNativeInputUnsupported) {
		t.Fatal("native opt-in was accepted", err)
	}
	handler, err := neko.NewHandler(client, d.gate, neko.HandlerOptions{Token: orchestrationToken})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/desktop/input", strings.NewReader(`{"events":[{"type":"key","key":"Control","action":"down"}]}`))
	r.Header.Set("Authorization", "Bearer "+orchestrationToken)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNotImplemented {
		t.Fatal("upstream input route was not disabled", w.Code)
	}
	if err = client.Give(context.Background(), "guessed-viewer"); !errors.Is(err, neko.ErrNativeInputUnsupported) {
		t.Fatal("native Give enabled", err)
	}
	if err = client.Execute(context.Background(), d.gate, control.Credentials{}, []neko.Event{{Type: "text", Text: "disabled"}}); !errors.Is(err, neko.ErrUpstreamInputUnsupported) {
		t.Fatal("upstream controller input enabled", err)
	}
	if client.SentFrames() != 0 {
		t.Fatal("disabled APIs emitted upstream input frames")
	}
}
