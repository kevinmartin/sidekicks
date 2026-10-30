package neko

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func viewerConnection(t *testing.T, c *Client, session string, onClose func(string)) (*websocket.Conn, <-chan error) {
	t.Helper()
	result := make(chan error, 1)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			result <- err
			return
		}
		defer ws.CloseNow()
		result <- c.RelaySignaling(r.Context(), session, ws, onClose)
	}))
	t.Cleanup(front.Close)
	ws, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(front.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ws.CloseNow()
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err = ws.Read(ctx); err != nil {
		t.Fatal("viewer init", err)
	}
	return ws, result
}

// TestViewerIdentityAndSignalingBoundary verifies viewer identity and signaling boundary.
func TestViewerIdentityAndSignalingBoundary(t *testing.T) {
	c, f := connected(t)
	for _, session := range []string{"", "a/b", "a\n", strings.Repeat("a", 129)} {
		if _, err := c.CreateViewer(context.Background(), session); err == nil {
			t.Fatal("invalid viewer", session)
		}
	}
	v, err := c.CreateViewer(context.Background(), "human")
	if err != nil {
		t.Fatal(err)
	}
	if v.ID != "viewer-id" || v.Token != "viewer-token" {
		t.Fatal("bad identity")
	}
	if _, err = c.CreateViewer(context.Background(), "human"); err == nil {
		t.Fatal("duplicate viewer")
	}
	if _, err = c.ViewerID("human"); err == nil {
		t.Fatal("unconnected viewer accepted")
	}
	if err = c.Give(context.Background(), v.ID); err == nil {
		t.Fatal("unconnected native grant")
	}
	if _, err = c.ViewerREST(context.Background(), "human", "/api/room/control/take"); err == nil {
		t.Fatal("unsafe REST")
	}
	if _, err = c.ViewerREST(context.Background(), "missing", "/api/whoami"); err == nil {
		t.Fatal("missing viewer")
	}
	closed := make(chan string, 1)
	ws, result := viewerConnection(t, c, "human", func(s string) {
		closed <- s
	})
	id, err := c.ViewerID("human")
	if err != nil || id != "viewer-id" {
		t.Fatal("connected identity", id, err)
	}
	if err = c.Give(context.Background(), id); err != ErrNativeInputUnsupported {
		t.Fatal("native grant was not rejected", err)
	}
	if err = ws.Write(context.Background(), websocket.MessageText, []byte(`{"event":"signal/answer","payload":{"sdp":"synthetic"}}`)); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool {
		return f.count() == 1
	})
	if f.events()[0].Event != "signal/answer" {
		t.Fatal("signal not relayed")
	}
	if err = ws.Write(context.Background(), websocket.MessageText, []byte(`{"event":"control/keydown","payload":{"keysym":97}}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-result:
		if err == nil {
			t.Fatal("malformed signaling falsely successful")
		}
	case <-time.After(time.Second):
		t.Fatal("denied control did not close")
	}
	if f.count() != 1 {
		t.Fatal("raw control leaked upstream")
	}
	select {
	case session := <-closed:
		if session != "human" {
			t.Fatal(session)
		}
	case <-time.After(time.Second):
		t.Fatal("close callback missing")
	}
	if _, err = c.ViewerID("human"); err == nil {
		t.Fatal("closed viewer still available")
	}
}

// TestControllerDisconnectTerminatesViewerAndRevokesCallback verifies controller disconnect terminates viewer and revokes callback.
func TestControllerDisconnectTerminatesViewerAndRevokesCallback(t *testing.T) {
	c, f := connected(t)
	if _, err := c.CreateViewer(context.Background(), "human"); err != nil {
		t.Fatal(err)
	}
	_, result := viewerConnection(t, c, "human", nil)
	f.mu.Lock()
	admin := f.admin
	f.mu.Unlock()
	admin.CloseNow()
	wait(t, func() bool {
		return !c.Ready()
	})
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("viewer survived controller disconnect")
	}
	if _, err := c.ViewerID("human"); err == nil {
		t.Fatal("viewer identity survived controller disconnect")
	}
}

// TestViewerLimitAndAllowedEvents verifies viewer limit and allowed events.
func TestViewerLimitAndAllowedEvents(t *testing.T) {
	c, _ := connected(t)
	c.mu.Lock()
	for i := 0; i < 64; i++ {
		c.viewers[string(rune(33+i))] = Viewer{Session: "reserved"}
	}
	c.mu.Unlock()
	if _, err := c.CreateViewer(context.Background(), "over"); err == nil {
		t.Fatal("viewer limit ignored")
	}
	for _, event := range []string{"signal/offer", "signal/answer", "client/heartbeat", "system/heartbeat"} {
		if !allowedViewerEvent(event) {
			t.Fatal(event)
		}
	}
	for _, event := range []string{"", "signal/", "control/move", "room/control/give", strings.Repeat("x", 129)} {
		if allowedViewerEvent(event) {
			t.Fatal(event)
		}
	}
	if _, err := c.ViewerID("none"); err == nil {
		t.Fatal("unknown viewer")
	}
}

// TestDelayedViewerDialCannotResurrectAcrossReconnect verifies delayed viewer dial cannot resurrect across reconnect.
// A delayed old viewer dial must not publish into a reconnected controller's
// state, even when the upstream reuses the same textual session ID and token.
func TestDelayedViewerDialCannotResurrectAcrossReconnect(t *testing.T) {
	c, f := connected(t)
	old, err := c.CreateViewer(context.Background(), "reuse")
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	f.mu.Lock()
	f.viewerDialStarted = started
	f.viewerDialRelease = release
	oldAdmin := f.admin
	f.mu.Unlock()
	result := make(chan error, 1)
	callback := make(chan struct{}, 1)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, e := websocket.Accept(w, r, nil)
		if e != nil {
			result <- e
			return
		}
		defer ws.CloseNow()
		result <- c.RelaySignaling(r.Context(), "reuse", ws, func(string) {
			callback <- struct{}{}
		})
	}))
	defer front.Close()
	downstream, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(front.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer downstream.CloseNow()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("dial barrier not reached")
	}
	oldAdmin.CloseNow()
	wait(t, func() bool {
		c.mu.Lock()
		done := c.done
		ready := c.ready
		c.mu.Unlock()
		if ready {
			return false
		}
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	if err = c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	fresh, err := c.CreateViewer(context.Background(), "reuse")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.reservation == old.reservation {
		t.Fatal("reservation reused")
	}
	close(release)
	select {
	case err = <-result:
		if err == nil {
			t.Fatal("stale dial accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("stale dial did not stop")
	}
	c.mu.Lock()
	current, exists := c.viewers["reuse"]
	c.mu.Unlock()
	if !exists || current.reservation != fresh.reservation || current.connected || current.connecting {
		t.Fatal("new viewer state changed by old dial", current, exists)
	}
	select {
	case <-callback:
		t.Fatal("stale callback could revoke reused session")
	default:
	}
}
