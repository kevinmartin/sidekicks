package neko

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

// Viewer is an upstream read-only Neko identity. Token is a sensitive bearer
// credential for signaling; return it only over the authenticated viewer path.
type Viewer struct {
	Session     string `json:"session"`
	ID          string `json:"id"`
	Token       string `json:"token"`
	connected   bool
	connecting  bool
	reservation uint64
}

// CreateViewer authenticates an upstream viewer role; the upstream configuration
// must lock controls and must not grant this role independent take-control rights.
// Those server settings are a required deployment precondition, not enforced by
// hiding a button in the browser.
func (c *Client) CreateViewer(ctx context.Context, session string) (Viewer, error) {
	if !validSession(session) {
		return Viewer{}, errors.New("neko: invalid viewer session")
	}
	c.mu.Lock()
	if !c.ready {
		c.mu.Unlock()
		return Viewer{}, errors.New("neko: controller disconnected")
	}
	if _, ok := c.viewers[session]; ok {
		c.mu.Unlock()
		return Viewer{}, errors.New("neko: viewer session already exists")
	}
	if len(c.viewers) >= 64 {
		c.mu.Unlock()
		return Viewer{}, errors.New("neko: viewer limit reached")
	}
	connection := c.ws
	c.nextViewer++
	reservation := c.nextViewer
	// Reserve this name across concurrent login calls.
	c.viewers[session] = Viewer{Session: session, reservation: reservation}
	c.mu.Unlock()
	l, err := c.login(ctx, "viewer-"+session, c.cfg.ViewerPassword)
	if err != nil || !safeID(l.ID) || l.Profile.IsAdmin || !l.Profile.CanLogin || !l.Profile.CanConnect || !l.Profile.CanWatch || l.Profile.CanHost || l.Profile.CanShareMedia || l.Profile.CanAccessClipboard {
		c.mu.Lock()
		if current, ok := c.viewers[session]; ok && current.reservation == reservation {
			delete(c.viewers, session)
		}
		c.mu.Unlock()
		if err == nil {
			err = errors.New("neko: invalid viewer identity")
		}
		return Viewer{}, err
	}
	v := Viewer{Session: session, ID: l.ID, Token: l.Token, reservation: reservation}
	c.mu.Lock()
	defer c.mu.Unlock()
	current, exists := c.viewers[session]
	if !c.ready || c.ws != connection || !exists || current.reservation != reservation {
		if exists && current.reservation == reservation {
			delete(c.viewers, session)
		}
		return Viewer{}, errors.New("neko: controller disconnected")
	}
	c.viewers[session] = v
	return v, nil
}

// RelaySignaling forwards only WebRTC signaling and heartbeat messages from the
// SDK. It never forwards control/*, host-transfer, or arbitrary REST commands.
// An authenticated HTTP layer must choose session; untrusted input must not
// supply an upstream URL or administrator token. onClose should revoke the
// matching session's lease before reporting viewer cleanup complete.
func (c *Client) RelaySignaling(ctx context.Context, session string, downstream *websocket.Conn, onClose func(string)) error {
	c.mu.Lock()
	v, ok := c.viewers[session]
	if !ok || v.ID == "" || (v.connected || v.connecting) || !c.ready {
		c.mu.Unlock()
		return errors.New("neko: unavailable viewer session")
	}
	connection := c.ws
	v.connecting = true
	c.viewers[session] = v
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		owned := false
		if current, exists := c.viewers[session]; exists && current.reservation == v.reservation {
			owned = true
			delete(c.viewers, session)
			delete(c.viewerCancels, session)
		}
		c.mu.Unlock()
		if owned && onClose != nil {
			onClose(session)
		}
	}()
	u := *c.base
	u.Scheme = "ws"
	u.Path = "/api/ws"
	u.RawQuery = url.Values{"token": {v.Token}}.Encode()
	upstream, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: c.http, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return errors.New("neko: viewer signaling unavailable")
	}
	defer upstream.CloseNow()
	upstream.SetReadLimit(maxMessage)
	downstream.SetReadLimit(maxMessage)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.mu.Lock()
	current, exists := c.viewers[session]
	if !c.ready || c.ws != connection || !exists || current.reservation != v.reservation || !current.connecting {
		c.mu.Unlock()
		return errors.New("neko: controller or viewer reservation changed")
	}
	v.connected = true
	v.connecting = false
	c.viewers[session] = v
	c.viewerCancels[session] = cancel
	c.mu.Unlock()
	results := make(chan error, 2)
	var once sync.Once
	end := func() {
		once.Do(func() {
			cancel()
			upstream.CloseNow()
			downstream.CloseNow()
		})
	}
	go func() {
		defer end()
		for {
			typ, b, e := downstream.Read(runCtx)
			if e != nil {
				results <- e
				return
			}
			if typ != websocket.MessageText {
				results <- errors.New("neko: signaling text required")
				return
			}
			var m message
			if json.Unmarshal(b, &m) != nil || !allowedViewerEvent(m.Event) {
				results <- errors.New("neko: viewer event denied")
				return
			}
			if e = upstream.Write(runCtx, websocket.MessageText, b); e != nil {
				results <- e
				return
			}
		}
	}()
	go func() {
		defer end()
		for {
			typ, b, e := upstream.Read(runCtx)
			if e != nil {
				results <- e
				return
			}
			if typ != websocket.MessageText {
				results <- errors.New("neko: invalid upstream signaling")
				return
			}
			b, e = c.rewriteSignal(b)
			if e != nil {
				results <- e
				return
			}
			if e = downstream.Write(runCtx, websocket.MessageText, b); e != nil {
				results <- e
				return
			}
		}
	}()
	first := <-results
	end()
	<-results
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if status := websocket.CloseStatus(first); status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway {
		return nil
	}
	return errors.New("neko: signaling connection ended or event denied")
}
func allowedViewerEvent(event string) bool {
	return (strings.HasPrefix(event, "signal/") && len(event) > len("signal/") && len(event) <= 128) || event == "client/heartbeat" || event == "system/heartbeat"
}

// ViewerID resolves only an actually connected viewer for native lease transfer.
func (c *Client) ViewerID(session string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	viewer, ok := c.viewers[session]
	if !ok || !viewer.connected || !c.ready {
		return "", errors.New("neko: connected viewer required")
	}
	return viewer.ID, nil
}

// ViewerREST provides the SDK's narrowly allowlisted read-only metadata routes.
// It cannot proxy host-control, file, clipboard, or administrator endpoints.
func (c *Client) ViewerREST(ctx context.Context, session, path string) ([]byte, error) {
	switch path {
	case "/api/whoami", "/api/room/screen", "/api/room/screen/configurations":
	default:
		return nil, errors.New("neko: viewer rest route denied")
	}
	c.mu.Lock()
	viewer, ok := c.viewers[session]
	ready := c.ready
	c.mu.Unlock()
	if !ok || viewer.Token == "" || !ready {
		return nil, errors.New("neko: viewer identity required")
	}
	return c.request(ctx, http.MethodGet, path, viewer.Token, nil)
}
