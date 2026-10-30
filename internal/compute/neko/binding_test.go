package neko

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/compute/control"
)

func boundSDKConnection(t *testing.T, h *Handler, session string) (SDKViewerTicket, *websocket.Conn, <-chan struct{}) {
	t.Helper()
	ticket, err := h.CreateSDKViewer(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		if r.URL.Path == "/desktop/stream/api/ws" {
			finished <- struct{}{}
		}
	}))
	t.Cleanup(server.Close)
	ws, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http")+"/desktop/stream/api/ws?token="+ticket.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ws.CloseNow()
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err = ws.Read(ctx); err != nil {
		t.Fatal(err)
	}
	return ticket, ws, finished
}
func finishSDK(t *testing.T, ws *websocket.Conn, finished <-chan struct{}) {
	t.Helper()
	_ = ws.Close(websocket.StatusNormalClosure, "")
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("SDK handler did not finish cleanup")
	}
}
func countReset(f *fakeNeko) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, p := range f.calls {
		if p == "POST /api/room/control/reset" {
			count++
		}
	}
	return count
}

// TestDelegatedGatedLeaseBindingReleasesOnSDKClose verifies delegated gated lease binding releases on sdk close.
func TestDelegatedGatedLeaseBindingReleasesOnSDKClose(t *testing.T) {
	h, f := handlerSetup(t, false)
	_, ws, finished := boundSDKConnection(t, h, "public")
	lease, err := h.gate.Acquire(context.Background(), control.Request{Owner: control.Human, Session: "public", Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.BindLease("public", lease); err != nil {
		t.Fatal(err)
	}
	before := countReset(f)
	finishSDK(t, ws, finished)
	if h.gate.View().Session != "" || !h.gate.View().Ready {
		t.Fatal("SDK close left human ownership", h.gate.View())
	}
	if countReset(f) <= before {
		t.Fatal("SDK close did not reset backend input")
	}
}

// TestBindingRejectsForeignStaleOrDisconnectedLeasesWithoutSideEffects verifies binding rejects foreign stale or disconnected leases without side effects.
func TestBindingRejectsForeignStaleOrDisconnectedLeasesWithoutSideEffects(t *testing.T) {
	h, _ := handlerSetup(t, false)
	_, ws, finished := boundSDKConnection(t, h, "public")
	lease, err := h.gate.Acquire(context.Background(), control.Request{Owner: control.Human, Session: "public", Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*control.Lease){func(l *control.Lease) {
		l.Owner = control.Agent
	}, func(l *control.Lease) {
		l.Generation++
	}, func(l *control.Lease) {
		l.Token = "wrong"
	}, func(l *control.Lease) {
		l.Mode = control.Native
		l.ViewerID = "forged"
	}, func(l *control.Lease) {
		l.Mode = "unknown"
	}} {
		bad := lease
		alter(&bad)
		if h.BindLease("public", bad) == nil {
			t.Fatal("invalid bind accepted")
		}
		if _, err = h.gate.Validate(control.Credentials{Session: "public", Token: lease.Token}); err != nil {
			t.Fatal("failed bind changed real lease")
		}
	}
	if h.BindLease("other", lease) == nil {
		t.Fatal("foreign session accepted")
	}
	finishSDK(t, ws, finished)
	if h.BindLease("public", lease) == nil {
		t.Fatal("disconnected viewer accepted")
	}
	if err = h.gate.Release(context.Background(), control.Credentials{Session: "public", Token: lease.Token}); err != nil {
		t.Fatal(err)
	}
}

// TestLateOldSDKCloseCannotRevokeNewlyBoundHuman verifies late old sdk close cannot revoke newly bound human.
func TestLateOldSDKCloseCannotRevokeNewlyBoundHuman(t *testing.T) {
	h, f := handlerSetup(t, false)
	_, oldWS, oldFinished := boundSDKConnection(t, h, "old")
	old, err := h.gate.Acquire(context.Background(), control.Request{Owner: control.Human, Session: "old", Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.BindLease("old", old); err != nil {
		t.Fatal(err)
	}
	if err = h.gate.Release(context.Background(), control.Credentials{Session: "old", Token: old.Token}); err != nil {
		t.Fatal(err)
	}
	_, newWS, newFinished := boundSDKConnection(t, h, "new")
	fresh, err := h.gate.Acquire(context.Background(), control.Request{Owner: control.Human, Session: "new", Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.BindLease("new", fresh); err != nil {
		t.Fatal(err)
	}
	before := countReset(f)
	finishSDK(t, oldWS, oldFinished)
	if _, err = h.gate.Validate(control.Credentials{Session: "new", Token: fresh.Token}); err != nil {
		t.Fatal("old SDK close revoked newer human", err)
	}
	h.mu.Lock()
	issued := h.issued
	h.mu.Unlock()
	if countReset(f) != before || issued.Token != fresh.Token {
		t.Fatal("old cleanup changed new binding")
	}
	finishSDK(t, newWS, newFinished)
	if h.gate.View().Session != "" {
		t.Fatal("new SDK did not release its own lease")
	}
}

// TestBindMissingTicketRejectsWithoutRevokingHuman verifies bind missing ticket rejects without revoking human.
func TestBindMissingTicketRejectsWithoutRevokingHuman(t *testing.T) {
	h, _ := handlerSetup(t, false)
	ticket, ws, finished := boundSDKConnection(t, h, "public")
	lease, err := h.gate.Acquire(context.Background(), control.Request{Owner: control.Human, Session: "public", Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	delete(h.tickets, ticket.Token)
	h.mu.Unlock()
	if h.BindLease("public", lease) == nil {
		t.Fatal("missing ticket accepted")
	}
	if err = h.gate.Release(context.Background(), control.Credentials{Session: "public", Token: lease.Token}); err != nil {
		t.Fatal(err)
	}
	finishSDK(t, ws, finished)
}

// TestControllerDisconnectImmediatelyFencesUnissuedAgentDispatch verifies controller disconnect immediately fences unissued agent dispatch.
func TestControllerDisconnectImmediatelyFencesUnissuedAgentDispatch(t *testing.T) {
	h, f := handlerSetup(t, false)
	lease, err := h.gate.Acquire(context.Background(), control.Request{Owner: control.Agent, Session: "private-exec", Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	creds := control.Credentials{Session: lease.Session, Token: lease.Token}
	h.mu.Lock()
	issued := h.issued
	h.mu.Unlock()
	if issued.Token != "" {
		t.Fatal("test must bypass handler-issued lease tracking")
	}
	started, canceled, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- h.gate.Dispatch(context.Background(), creds, func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(canceled)
			return ctx.Err()
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("dispatch not established")
	}
	before := countReset(f)
	f.mu.Lock()
	admin := f.admin
	f.mu.Unlock()
	admin.CloseNow()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("controller disconnect did not immediately cancel unissued dispatch")
	}
	if _, err = h.gate.Validate(creds); err == nil {
		t.Fatal("disconnected controller left old agent credentials valid")
	}
	if view := h.gate.View(); view.Session != "" || view.Ready {
		t.Fatal("disconnect was falsely acknowledged as ready", view)
	}
	select {
	case err = <-finished:
		if err == nil {
			t.Fatal("canceled dispatch reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled dispatch did not return")
	}
	wait(t, func() bool {
		h.client.mu.Lock()
		done := h.client.done
		h.client.mu.Unlock()
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	if countReset(f) <= before {
		t.Fatal("saved-token backend reset was not attempted")
	}
	// The old deferred cleanup is generation fenced. A fresh controller/lease
	// remains possible after old cleanup; no delayed global Stop is scheduled.
	if err = h.client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	fresh, err := h.gate.Acquire(context.Background(), control.Request{Owner: control.Human, Session: "fresh", Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.gate.Validate(control.Credentials{Session: "fresh", Token: fresh.Token}); err != nil {
		t.Fatal("new owner invalidated by old cleanup", err)
	}
}
