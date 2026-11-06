package neko

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/compute/control"
)

// TestPinnedUpstreamInputPathsAreUnsupportedWithoutSending verifies pinned upstream input paths are unsupported without sending.
func TestPinnedUpstreamInputPathsAreUnsupportedWithoutSending(t *testing.T) {
	c, f := connected(t)
	g, creds := gateAgent(t, c)
	for _, events := range [][]Event{nil, {{Type: "text", Text: "must not dispatch"}}, {{Type: "key", Key: "Control", Action: "down"}}, {{Type: "unsupported"}}} {
		if err := c.Execute(context.Background(), g, creds, events); !errors.Is(err, ErrUpstreamInputUnsupported) {
			t.Fatal("controller input was not rejected", err)
		}
	}
	if err := c.send(context.Background(), "control/keydown", map[string]int{"keysym": 65507}); !errors.Is(err, ErrUpstreamInputUnsupported) {
		t.Fatal("control send escaped guard", err)
	}
	if c.SentFrames() != 0 || f.count() != 0 {
		t.Fatal("disabled controller input emitted frames")
	}
	if _, err := g.Validate(creds); err != nil {
		t.Fatal("unsupported compatibility API changed real ownership", err)
	}
	if _, err := NewHandler(c, g, HandlerOptions{Token: instanceTestToken, EnableNative: true}); !errors.Is(err, ErrNativeInputUnsupported) {
		t.Fatal("experimental native opt-in accepted", err)
	}
}

// TestConnectedMediaViewerCannotBeGrantedNativeHost verifies connected media viewer cannot be granted native host.
func TestConnectedMediaViewerCannotBeGrantedNativeHost(t *testing.T) {
	c, f := connected(t)
	if err := c.SetMediaPorts(allocation(45000)); err != nil {
		t.Fatal(err)
	}
	viewer, err := c.CreateViewer(context.Background(), "media-only")
	if err != nil {
		t.Fatal(err)
	}
	ws, result := viewerConnection(t, c, "media-only", nil)
	if err = c.Give(context.Background(), viewer.ID); !errors.Is(err, ErrNativeInputUnsupported) {
		t.Fatal("connected native grant accepted", err)
	}
	f.mu.Lock()
	calls := append([]string(nil), f.calls...)
	upstream := f.viewer
	f.mu.Unlock()
	for _, call := range calls {
		if call == "POST /api/room/control/give/"+viewer.ID {
			t.Fatal("native ownership reached upstream")
		}
	}
	// Rejecting ownership does not disable the WebRTC signaling/media route.
	signal := []byte(`{"event":"signal/provide","payload":{"sdp":"v=0\r\n"}}`)
	if err = upstream.Write(context.Background(), websocket.MessageText, signal); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, received, err := ws.Read(ctx)
	if err != nil || string(received) != string(signal) {
		t.Fatal("media signaling was lost", string(received), err)
	}
	_ = ws.Close(websocket.StatusNormalClosure, "")
	select {
	case <-result:
	case <-ctx.Done():
		t.Fatal("media viewer did not clean up")
	}
}

// TestDisabledPrivateUpstreamInputDoesNotExposeOrCancelAnotherOwner verifies disabled private upstream input does not expose or cancel another owner.
func TestDisabledPrivateUpstreamInputDoesNotExposeOrCancelAnotherOwner(t *testing.T) {
	h, f := handlerSetup(t, false)
	lease, err := h.gate.Acquire(context.Background(), control.Request{Owner: control.Human, Session: "gated", Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	w := handlerCall(t, h, http.MethodPost, "/desktop/input", `{"session":"foreign","token":"foreign","events":[{"type":"key","key":"Control","action":"down"}]}`, nil)
	if w.Code != http.StatusNotImplemented || f.count() != 0 {
		t.Fatal("unsafe private input enabled", w.Code)
	}
	if _, err = h.gate.Validate(control.Credentials{Session: "gated", Token: lease.Token}); err != nil {
		t.Fatal("unsupported route canceled real owner")
	}
	w = handlerCall(t, h, http.MethodPost, "/desktop/lease", `{"action":"acquire","owner":"human","session":"native","mode":"native"}`, nil)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatal("native acquisition enabled", w.Code)
	}
	if _, err = h.gate.Validate(control.Credentials{Session: "gated", Token: lease.Token}); err != nil {
		t.Fatal("rejected native request preempted real owner")
	}
}
