// Package browserviewer serves the local UI for a named browser tab.
package browserviewer

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
)

//go:embed web
var assets embed.FS

// Server serves the UI and relays the guest's authenticated viewer stream.
type Server struct {
	host  string
	token string
	files http.Handler
	dial  func(context.Context) (*websocket.Conn, error)
}

// New creates a viewer for a bound host-loopback address.
func New(host string, dial func(context.Context) (*websocket.Conn, error)) (*Server, error) {
	files, err := fs.Sub(assets, "web")
	if err != nil {
		return nil, fmt.Errorf("open viewer assets: %w", err)
	}
	return &Server{
		host:  host,
		token: rand.Text(),
		files: http.FileServerFS(files),
		dial:  dial,
	}, nil
}

// URL is the local URL for this viewer session.
func (s *Server) URL() string {
	return "http://" + s.host + "/#" + s.token
}

// Assets serves the embedded viewer page and source files.
func (s *Server) Assets(w http.ResponseWriter, r *http.Request) {
	if r.Host != s.host {
		http.Error(w, "invalid viewer host", http.StatusForbidden)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'self'; style-src 'self'; img-src data:; connect-src ws://"+s.host+"; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	s.files.ServeHTTP(w, r)
}

// Stream relays frames to and input from an authorized local viewer.
func (s *Server) Stream(w http.ResponseWriter, r *http.Request) {
	if r.Host != s.host || r.Header.Get("Origin") != "http://"+s.host || !s.authorized(r) {
		http.Error(w, "invalid viewer session", http.StatusForbidden)
		return
	}
	local, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{browserapi.ViewerProtocol},
	})
	if err != nil {
		return
	}
	defer func() {
		_ = local.CloseNow()
	}()
	local.SetReadLimit(browserapi.ViewerInputLimit)
	guest, err := s.dial(r.Context())
	if err != nil {
		reason := "browser guest is unavailable"
		switch {
		case strings.Contains(err.Error(), "tab unavailable"):
			reason = "selected browser tab is closed"
		case strings.Contains(err.Error(), "does not support the viewer"):
			reason = "start an instance with a viewer-enabled browser image"
		}
		_ = local.Close(websocket.StatusPolicyViolation, reason)
		return
	}
	defer func() {
		_ = guest.CloseNow()
	}()
	relay(r.Context(), local, guest)
}

func (s *Server) authorized(r *http.Request) bool {
	var protocol, token bool
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for value := range strings.SplitSeq(header, ",") {
			value = strings.TrimSpace(value)
			protocol = protocol || value == browserapi.ViewerProtocol
			token = token || subtle.ConstantTimeCompare([]byte(value), []byte(s.token)) == 1
		}
	}
	return token && protocol
}

type relayResult struct {
	fromGuest bool
	err       error
}

func relay(ctx context.Context, local, guest *websocket.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan relayResult, 2)
	go func() {
		done <- relayResult{
			fromGuest: true,
			err:       forward(ctx, guest, local),
		}
	}()
	go func() {
		done <- relayResult{
			err: forward(ctx, local, guest),
		}
	}()
	first := <-done
	if first.fromGuest {
		var closed websocket.CloseError
		if errors.As(first.err, &closed) && closed.Code == websocket.StatusPolicyViolation {
			_ = local.Close(closed.Code, closed.Reason)
		} else if ctx.Err() == nil {
			_ = local.Close(websocket.StatusInternalError, "browser viewer disconnected")
		}
	}
	cancel()
	<-done
}

func forward(ctx context.Context, from, to *websocket.Conn) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			log.Printf("browser viewer relay panicked: %v", failure)
			err = errors.New("browser viewer relay failed")
		}
	}()
	for {
		kind, message, err := from.Read(ctx)
		if err != nil {
			return err
		}
		write, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = to.Write(write, kind, message)
		cancel()
		if err != nil {
			return err
		}
	}
}
