package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute/control"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/viewer"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/x11"
	"github.com/kevinmartin/sidekicks/internal/compute/neko"
)

type nekoReset struct {
	client *neko.Client
	x      *x11.Driver
}

// Reset clears the Neko controller and held desktop input.
func (n nekoReset) Reset(ctx context.Context) error {
	return errors.Join(n.client.Reset(ctx), n.x.Reset(ctx))
}
func (d *desktopWorkload) prepareNeko(ctx context.Context, token string) error {
	admin := sha256.Sum256([]byte(token + ":neko-admin"))
	guest := sha256.Sum256([]byte(token + ":neko-viewer"))
	adminPassword := hex.EncodeToString(admin[:])
	viewerPassword := hex.EncodeToString(guest[:])
	config := nekoServerConfig(adminPassword, viewerPassword)
	b, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if err = os.WriteFile("/run/sidekicks-neko.yaml", b, 0600); err != nil {
		return err
	}
	pulse := []byte("load-module module-null-sink sink_name=audio_output\nload-module module-native-protocol-unix socket=/tmp/pulseaudio.socket auth-anonymous=1\nload-module module-always-sink\nset-default-sink audio_output\n")
	if err = os.WriteFile("/run/sidekicks-pulse.pa", pulse, 0600); err != nil {
		return err
	}
	env := append(d.session.Env(), "PULSE_SERVER=unix:/tmp/pulseaudio.socket", "NEKO_CONFIG=/run/sidekicks-neko.yaml")
	stop, done, err := startChild(ctx, env, "/usr/bin/pulseaudio", "-nF", "/run/sidekicks-pulse.pa", "--exit-idle-time=-1", "--disallow-exit")
	if err != nil {
		return err
	}
	d.stopChildren = append(d.stopChildren, stop)
	d.childDone = append(d.childDone, done)
	stop, done, err = startChild(ctx, env, "/usr/bin/neko", "serve")
	if err != nil {
		return err
	}
	d.stopChildren = append(d.stopChildren, stop)
	d.childDone = append(d.childDone, done)
	if err = waitTCP(ctx, "127.0.0.1:8082"); err != nil {
		return err
	}
	client, err := neko.New(neko.Config{Endpoint: "http://127.0.0.1:8082", AdminUsername: "sidekicks-controller", AdminPassword: adminPassword, ViewerPassword: viewerPassword})
	if err != nil {
		return err
	}
	d.nekoClient = client
	if err = client.Connect(ctx); err != nil {
		return err
	}
	old := d.gate
	d.gate = control.New(nekoReset{client, d.driver})
	if old != nil {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = old.Close(c)
		cancel()
	}
	handler, err := neko.NewHandler(client, d.gate, neko.HandlerOptions{Token: token})
	if err != nil {
		return err
	}
	d.nekoHandler = handler
	d.viewer, err = viewer.New(d.nekoViewerConfig(handler))
	return err
}

func nekoServerConfig(adminPassword, viewerPassword string) map[string]any {
	return map[string]any{
		"legacy": false, "server": map[string]any{"bind": "127.0.0.1:8082", "proxy": false},
		"desktop": map[string]any{"screen": "1280x720@30", "input": map[string]any{"enabled": false}},
		"session": map[string]any{"implicit_hosting": false, "locked_controls": true, "cookie": map[string]any{"enabled": false}},
		"member":  map[string]any{"provider": "multiuser", "multiuser": map[string]any{"admin_password": adminPassword, "user_password": viewerPassword, "user_profile": map[string]any{"can_login": true, "can_connect": true, "can_watch": true, "can_host": false, "can_share_media": false, "can_access_clipboard": false, "sends_inactive_cursor": false, "can_see_inactive_cursors": false}}},
		"webrtc":  map[string]any{"icelite": true, "epr": "60020-60039", "nat1to1": []string{"127.0.0.1"}, "ip_retrieval_url": "", "iceservers": map[string]any{"frontend": []any{map[string]any{"urls": []string{"stun:127.0.0.1:9"}}}, "backend": []any{}}},
		"capture": map[string]any{"video": map[string]any{"codec": "vp8", "show_pointer": false}, "microphone": map[string]any{"enabled": false}, "webcam": map[string]any{"enabled": false}}, "plugins": map[string]any{"enabled": false},
	}
}

func (d *desktopWorkload) nekoViewerConfig(handler *neko.Handler) viewer.Config {
	return viewer.Config{
		Gate: d.gate, Driver: d.driver, OnAcquire: d.onAcquire, Assets: desktopAssets("desktop-neko"),
		ReadOnlyAdditionalPaths: []string{"/desktop/stream/api/whoami", "/desktop/stream/api/room/screen", "/desktop/stream/api/room/screen/configurations"},
		OpaqueTicketQueryPaths:  []string{"/desktop/stream/api/ws"},
		Additional: func(w http.ResponseWriter, r *http.Request, session string) bool {
			if r.URL.Path == "/viewer" {
				if r.Method != "POST" {
					http.Error(w, "post required", http.StatusMethodNotAllowed)
					return true
				}
				var body struct{}
				if !strictJSON(w, r, &body) {
					return true
				}
				ticket, e := handler.CreateSDKViewer(r.Context(), session)
				if e != nil {
					http.Error(w, "viewer unavailable", http.StatusServiceUnavailable)
					return true
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(ticket)
				return true
			}
			if strings.HasPrefix(r.URL.Path, "/desktop/stream/api/") {
				ticket := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				if r.URL.Path == "/desktop/stream/api/ws" {
					ticket = r.URL.Query().Get("token")
				}
				if !handler.TicketBelongs(ticket, session) {
					http.Error(w, "viewer ticket mismatch", http.StatusForbidden)
					return true
				}
				clone := r.Clone(r.Context())
				clone.Header = r.Header.Clone()
				clone.Header.Del("Origin")
				clone.Header.Del("Cookie")
				handler.ServeHTTP(w, clone)
				return true
			}
			return false
		},
	}
}
