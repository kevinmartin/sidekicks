package neko

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/compute/control"
)

const instanceTestToken = "test-instance-token-with-at-least-thirty-two-bytes"

func handlerSetup(t *testing.T, native bool) (*Handler, *fakeNeko) {
	t.Helper()
	c, f := connected(t)
	g := control.New(c)
	t.Cleanup(func() {
		_ = g.Close(context.Background())
	})
	h, err := NewHandler(c, g, HandlerOptions{Token: instanceTestToken, EnableNative: native})
	if err != nil {
		t.Fatal(err)
	}
	return h, f
}
func handlerCall(t *testing.T, h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+instanceTestToken)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TestHandlerAuthenticationMethodsAndBodyLimits verifies handler authentication methods and body limits.
func TestHandlerAuthenticationMethodsAndBodyLimits(t *testing.T) {
	h, _ := handlerSetup(t, false)
	if _, err := NewHandler(nil, h.gate, HandlerOptions{Token: instanceTestToken}); err == nil {
		t.Fatal("nil client accepted")
	}
	if _, err := NewHandler(h.client, h.gate, HandlerOptions{Token: "weak"}); err == nil {
		t.Fatal("weak token")
	}
	for _, tc := range []struct {
		method, path, body string
		headers            map[string]string
		status             int
	}{
		{"GET", "/desktop/health", "", nil, 200}, {"GET", "/desktop/capabilities", "", nil, 200}, {"GET", "/desktop/health", "", map[string]string{"Authorization": "Bearer wrong"}, 401}, {"GET", "/desktop/health", "", map[string]string{"Origin": "http://evil.example"}, 403}, {"POST", "/desktop/health", "{}", nil, 405}, {"GET", "/desktop/unknown", "", nil, 404},
		{"POST", "/desktop/lease", "{}", map[string]string{"Content-Type": "text/plain"}, 415}, {"POST", "/desktop/lease", "{bad", nil, 400}, {"POST", "/desktop/lease", `{"action":"stop","unknown":1}`, nil, 400}, {"POST", "/desktop/lease", "{} {}", nil, 400}, {"POST", "/desktop/lease", strings.Repeat("x", 65537), nil, 400}, {"POST", "/desktop/lease", `{"action":"wrong"}`, nil, 400}, {"POST", "/desktop/lease", `{"action":"acquire","leaseMs":1}`, nil, 400}, {"POST", "/desktop/lease", `{"action":"acquire","owner":"human","session":"native","mode":"native"}`, nil, 422},
		{"GET", "/desktop/capture?maxWidth=notint", "", nil, 400}, {"GET", "/desktop/capture?maxHeight=bad", "", nil, 400}, {"POST", "/desktop/input", `{"session":"s","token":"x","events":[]}`, nil, 501},
		{"GET", "/desktop/stream/api/room/control/take", "", nil, 403}, {"POST", "/desktop/stream/api/whoami", "{}", nil, 405}, {"GET", "/desktop/stream/api/whoami", "", nil, 401},
	} {
		w := handlerCall(t, h, tc.method, tc.path, tc.body, tc.headers)
		if w.Code != tc.status {
			t.Fatalf("%s %s => %d want%d: %s", tc.method, tc.path, w.Code, tc.status, w.Body.String())
		}
	}
	w := handlerCall(t, h, "GET", "/desktop/capabilities", "", nil)
	var capabilities struct {
		NativeControl     bool   `json:"nativeControl"`
		UpstreamInput     bool   `json:"upstreamInput"`
		InjectionBoundary string `json:"injectionBoundary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &capabilities); err != nil || capabilities.NativeControl || capabilities.UpstreamInput || capabilities.InjectionBoundary != "synchronized Go X11; no application queue flush" {
		t.Fatalf("misleading input capabilities: %s (%v)", w.Body.String(), err)
	}
}

// TestHandlerLeaseCaptureAndDisabledUpstreamInput verifies handler lease capture and disabled upstream input.
func TestHandlerLeaseCaptureAndDisabledUpstreamInput(t *testing.T) {
	h, f := handlerSetup(t, false)
	w := handlerCall(t, h, "POST", "/desktop/lease", `{"action":"acquire","owner":"agent","session":"agent","leaseMs":60000}`, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var lease control.Lease
	if json.Unmarshal(w.Body.Bytes(), &lease) != nil || lease.Token == "" {
		t.Fatal(w.Body.String())
	}
	b, _ := json.Marshal(map[string]any{"session": "agent", "token": lease.Token, "events": []Event{{Type: "text", Text: "Hi!"}}})
	w = handlerCall(t, h, "POST", "/desktop/input", string(b), nil)
	if w.Code != http.StatusNotImplemented {
		t.Fatal(w.Code, w.Body.String())
	}
	if f.count() != 0 {
		t.Fatal("disabled input emitted a frame")
	}
	w = handlerCall(t, h, "GET", "/desktop/lease", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"agent"`) {
		t.Fatal(w.Body.String())
	}
	var jpegBytes bytes.Buffer
	_ = jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 64, 32)), nil)
	f.mu.Lock()
	f.shot = jpegBytes.Bytes()
	f.mu.Unlock()
	w = handlerCall(t, h, "GET", "/desktop/capture?maxWidth=16&maxHeight=16", "", nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("X-Image-Width") != "16" {
		t.Fatal(w.Code, w.Header())
	}
	b, _ = json.Marshal(map[string]string{"action": "release", "session": "agent", "token": lease.Token})
	w = handlerCall(t, h, "POST", "/desktop/lease", string(b), nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = handlerCall(t, h, "POST", "/desktop/input", `{"session":"agent","token":"stale","events":[{"type":"text","text":"x"}]}`, nil)
	if w.Code != http.StatusNotImplemented {
		t.Fatal(w.Code, w.Body.String())
	}
	w = handlerCall(t, h, "POST", "/desktop/lease", `{"action":"stop"}`, nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}

// TestHandlerOpaqueSDKTicketAndGatedClose verifies handler opaque sdk ticket and gated close.
func TestHandlerOpaqueSDKTicketAndGatedClose(t *testing.T) {
	h, _ := handlerSetup(t, false)
	w := handlerCall(t, h, "POST", "/desktop/viewer", `{"session":"human"}`, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var ticket struct{ Token, ID, Session string }
	if json.Unmarshal(w.Body.Bytes(), &ticket) != nil || ticket.Token == "viewer-token" || len(ticket.Token) != 43 {
		t.Fatal("upstream token exposed", w.Body.String())
	}
	w = handlerCall(t, h, "GET", "/desktop/stream/api/whoami", "", map[string]string{"Authorization": "Bearer " + ticket.Token})
	if w.Code != 200 || strings.Contains(w.Body.String(), "viewer-token") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = handlerCall(t, h, "POST", "/desktop/lease", `{"action":"acquire","owner":"human","session":"human","mode":"native","viewerId":"forged"}`, nil)
	if w.Code != 400 {
		t.Fatal("caller upstream identity accepted", w.Code)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	ws, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http")+"/desktop/stream/api/ws?token="+ticket.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err = ws.Read(ctx); err != nil {
		t.Fatal(err)
	}
	w = handlerCall(t, h, "POST", "/desktop/lease", `{"action":"acquire","owner":"human","session":"human","mode":"gated","leaseMs":60000}`, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var lease control.Lease
	_ = json.Unmarshal(w.Body.Bytes(), &lease)
	b, _ := json.Marshal(map[string]any{"session": "human", "token": lease.Token, "events": []Event{{Type: "text", Text: "blocked"}}})
	w = handlerCall(t, h, "POST", "/desktop/input", string(b), nil)
	if w.Code != http.StatusNotImplemented {
		t.Fatal(w.Code, w.Body.String())
	}
	if h.gate.View().Session != "human" {
		t.Fatal("native lease revoked")
	}
	_ = ws.Close(websocket.StatusNormalClosure, "")
	wait(t, func() bool {
		return h.gate.View().Session == ""
	})
	w = handlerCall(t, h, "GET", "/desktop/stream/api/whoami", "", map[string]string{"Authorization": "Bearer " + ticket.Token})
	if w.Code != 401 {
		t.Fatal("closed ticket survived", w.Code)
	}
}

// TestHandlerExpiredTicketAndDisconnectedHealth verifies handler expired ticket and disconnected health.
func TestHandlerExpiredTicketAndDisconnectedHealth(t *testing.T) {
	h, f := handlerSetup(t, false)
	w := handlerCall(t, h, "POST", "/desktop/viewer", `{"session":"stale"}`, nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var ticket struct{ Token string }
	_ = json.Unmarshal(w.Body.Bytes(), &ticket)
	h.mu.Lock()
	v := h.tickets[ticket.Token]
	v.Expires = time.Now().Add(-time.Minute)
	h.tickets[ticket.Token] = v
	h.mu.Unlock()
	w = handlerCall(t, h, "GET", "/desktop/stream/api/whoami", "", map[string]string{"Authorization": "Bearer " + ticket.Token})
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	h.expireTickets()
	h.client.mu.Lock()
	_, exists := h.client.viewers["stale"]
	h.client.mu.Unlock()
	if exists {
		t.Fatal("expired reservation not removed")
	}
	f.mu.Lock()
	admin := f.admin
	f.mu.Unlock()
	admin.CloseNow()
	wait(t, func() bool {
		return !h.client.Ready()
	})
	w = handlerCall(t, h, "GET", "/desktop/health", "", nil)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
