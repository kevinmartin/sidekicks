package viewer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/compute/control"
)

// TestAdditionalWebSocketFollowsCookieLifecycleAndCannotReleaseNewOwner verifies additional web socket follows cookie lifecycle and cannot release new owner.
func TestAdditionalWebSocketFollowsCookieLifecycleAndCannotReleaseNewOwner(t *testing.T) {
	for _, action := range []string{"rotation", "expiry", "close"} {
		t.Run(action, func(t *testing.T) {
			var clock atomic.Int64
			clock.Store(time.Now().UnixNano())
			entered := make(chan struct{}, 1)
			disconnected := make(chan struct{}, 1)
			allowCleanup := make(chan struct{})
			finished := make(chan struct{}, 1)
			var leasesMu sync.Mutex
			leases := map[string]control.Credentials{}
			service, gate, _ := setup(t, func(c *Config) {
				c.Now = func() time.Time {
					return time.Unix(0, clock.Load())
				}
				c.OnAcquire = func(session string, lease control.Lease) error {
					leasesMu.Lock()
					leases[session] = control.Credentials{Session: lease.Session, Token: lease.Token}
					leasesMu.Unlock()
					return nil
				}
				c.Additional = func(w http.ResponseWriter, r *http.Request, session string) bool {
					if r.URL.Path != "/sdk/ws" {
						return false
					}
					ws, err := websocket.Accept(w, r, nil)
					if err != nil {
						return true
					}
					defer ws.CloseNow()
					entered <- struct{}{}
					_, _, _ = ws.Read(r.Context())
					disconnected <- struct{}{}
					// Simulate a delayed backend disconnect callback. The service must
					// already close its authenticated socket and release only the old lease.
					<-allowCleanup
					leasesMu.Lock()
					credentials := leases[session]
					leasesMu.Unlock()
					if credentials.Token != "" {
						_ = c.Gate.Release(context.Background(), credentials)
					}
					finished <- struct{}{}
					return true
				}
			})
			cookie := authenticate(t, service)
			take(t, service, cookie)
			server := httptest.NewServer(service)
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			ws, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/sdk/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{server.URL}, "Cookie": []string{cookie.String()}}})
			if err != nil {
				t.Fatalf("SDK connection: %+v %v", response, err)
			}
			defer ws.CloseNow()
			<-entered
			if second, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/sdk/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{server.URL}, "Cookie": []string{cookie.String()}}}); err == nil {
				second.CloseNow()
				t.Fatal("parallel SDK socket accepted")
			} else if response == nil || response.StatusCode != http.StatusConflict {
				t.Fatalf("parallel SDK status: %+v %v", response, err)
			}
			var replacement *http.Cookie
			switch action {
			case "rotation":
				ticket, err := service.NewTicket()
				if err != nil {
					t.Fatal(err)
				}
				response := request(service, http.MethodPost, "/auth", fmt.Sprintf(`{"ticket":%q}`, ticket), cookie)
				if response.Code != http.StatusOK {
					t.Fatal(response.Body.String())
				}
				replacement = response.Result().Cookies()[0]
			case "expiry":
				clock.Add(int64(31 * time.Minute))
				if _, err := service.NewTicket(); err != nil {
					t.Fatal(err)
				}
				replacement = authenticate(t, service)
			case "close":
				if err := service.Close(ctx); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-disconnected:
			case <-ctx.Done():
				t.Fatal("cookie lifecycle did not cancel SDK request")
			}
			// The peer must close before the delayed backend callback is permitted.
			if _, _, err := ws.Read(ctx); err == nil {
				t.Fatal("retired SDK socket remained open")
			}
			if gate.View().Owner != "" {
				t.Fatal("retired cookie retained ownership")
			}
			var fresh uint64
			if replacement != nil {
				fresh = take(t, service, replacement)
			}
			close(allowCleanup)
			select {
			case <-finished:
			case <-ctx.Done():
				t.Fatal("backend cleanup did not finish")
			}
			if replacement != nil {
				current := state(t, request(service, http.MethodGet, "/state", "", replacement))
				if !current.Owned || current.Epoch != fresh {
					t.Fatal("late old SDK disconnect revoked replacement owner")
				}
			}
		})
	}
}
