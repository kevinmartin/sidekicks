package viewer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute/control"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop"
)

const authority = "127.0.0.1:7901"

type fakeDriver struct {
	inputs         atomic.Int64
	resets         atomic.Int64
	native         string
	capture        []byte
	captureErr     error
	blockCapture   chan struct{}
	captureEntered chan struct{}
	once           sync.Once
}

// Reset records a simulated input reset.
func (d *fakeDriver) Reset(context.Context) error {
	d.resets.Add(1)
	return nil
}

// Give records the selected native viewer.
func (d *fakeDriver) Give(_ context.Context, id string) error {
	d.native = id
	return nil
}

// Input records a simulated input event.
func (d *fakeDriver) Input(context.Context, desktop.Event) error {
	d.inputs.Add(1)
	return nil
}

// CapturePNG returns or waits for the configured fake capture.
func (d *fakeDriver) CapturePNG(ctx context.Context) ([]byte, error) {
	if d.captureEntered != nil {
		d.once.Do(func() {
			close(d.captureEntered)
		})
		select {
		case <-d.blockCapture:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return d.capture, d.captureErr
}

// Size returns the current display dimensions.
func (*fakeDriver) Size() (int, int) {
	return 1280, 720
}
func setup(t *testing.T, mutate func(*Config)) (*Service, *control.Manager, *fakeDriver) {
	t.Helper()
	driver := &fakeDriver{capture: []byte("\x89PNG\r\n\x1a\nsynthetic")}
	gate := control.New(driver)
	config := Config{Gate: gate, Driver: driver, RFBTarget: "127.0.0.1:5900", Assets: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("static " + r.URL.Path))
	})}
	if mutate != nil {
		mutate(&config)
	}
	service, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = service.Close(context.Background())
		_ = gate.Close(context.Background())
	})
	return service, gate, driver
}
func request(service *Service, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+authority+path, strings.NewReader(body))
	r.Host = authority
	if method != "GET" {
		r.Header.Set("Origin", "http://"+authority)
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
		// Existing tests represent a current viewer frontend; explicit epoch
		// regression tests construct requests without this automatic helper.
		if method == http.MethodPost {
			if viewer := service.lookup(r); viewer != nil {
				if lease, err := service.config.Gate.Validate(service.credentials(viewer)); err == nil && lease.Owner == control.Human && lease.Session == viewer.id {
					r.Header.Set(EpochHeader, strconv.FormatUint(lease.Generation, 10))
				}
			}
		}
	}
	w := httptest.NewRecorder()
	service.ServeHTTP(w, r)
	return w
}
func authenticate(t *testing.T, service *Service) *http.Cookie {
	t.Helper()
	ticket, err := service.NewTicket()
	if err != nil {
		t.Fatal(err)
	}
	w := request(service, "POST", "/auth", fmt.Sprintf(`{"ticket":%q}`, ticket), nil)
	if w.Code != 200 {
		t.Fatalf("auth: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing auth cookie")
	}
	return cookies[0]
}
func state(t *testing.T, w *httptest.ResponseRecorder) Response {
	t.Helper()
	var body Response
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

// TestTicketCookieOneTimeExpiryAndPortIndependentNames verifies ticket cookie one time expiry and port independent names.
func TestTicketCookieOneTimeExpiryAndPortIndependentNames(t *testing.T) {
	now := time.Now()
	service, _, _ := setup(t, func(c *Config) {
		c.Now = func() time.Time {
			return now
		}
	})
	ticket, err := service.NewTicket()
	if err != nil || len(ticket) != 43 {
		t.Fatal("bad ticket")
	}
	auth := request(service, "POST", "/auth", fmt.Sprintf(`{"ticket":%q}`, ticket), nil)
	if auth.Code != 200 {
		t.Fatal(auth.Body.String())
	}
	cookie := auth.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge != 1800 || cookie.Domain != "" {
		t.Fatalf("cookie protection: %+v", cookie)
	}
	if w := request(service, "POST", "/auth", fmt.Sprintf(`{"ticket":%q}`, ticket), nil); w.Code != 401 {
		t.Fatal("ticket replay accepted")
	}
	if w := request(service, "GET", "/state", "", cookie); w.Code != 200 || state(t, w).Owned {
		t.Fatal("read-only authenticated session missing")
	}
	next, err := service.NewTicket()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if w := request(service, "POST", "/auth", fmt.Sprintf(`{"ticket":%q}`, next), nil); w.Code != 401 {
		t.Fatal("expired ticket accepted")
	}
	now = now.Add(30 * time.Minute)
	if w := request(service, "GET", "/state", "", cookie); w.Code != 401 {
		t.Fatal("expired cookie accepted")
	}
	second, _, _ := setup(t, nil)
	secondCookie := authenticate(t, second)
	if secondCookie.Name == cookie.Name {
		t.Fatal("parallel port services would overwrite each other's cookie")
	}
}

// TestPublicBoundariesAndNoPrivateAPIExtension verifies public boundaries and no private api extension.
func TestPublicBoundariesAndNoPrivateAPIExtension(t *testing.T) {
	var extensionCalls atomic.Int64
	service, _, _ := setup(t, func(c *Config) {
		c.Additional = func(http.ResponseWriter, *http.Request, string) bool {
			extensionCalls.Add(1)
			return false
		}
	})
	cookie := authenticate(t, service)
	cases := []struct {
		method, path, host, origin, body string
		cookie                           *http.Cookie
		status                           int
	}{
		{"GET", "/state", "localhost:7901", "", "", cookie, 403},
		{"GET", "/state", "0.0.0.0:7901", "", "", cookie, 403},
		{"GET", "/state", "127.0.0.1:0", "", "", cookie, 403},
		{"GET", "/state", "127.0.0.1:07901", "", "", cookie, 403},
		{"GET", "/state", authority, "", "", nil, 401},
		{"POST", "/control", authority, "http://evil.example", `{"action":"acquire"}`, cookie, 403},
		{"POST", "/auth", authority, "", `{"ticket":"bad"}`, nil, 403},
		{"GET", "/state?token=secret", authority, "", "", cookie, 400},
		{"GET", "/state?ticket=secret", authority, "", "", cookie, 400},
		{"GET", "/exec", authority, "http://" + authority, "", cookie, 404},
		{"GET", "/files/workspace/x", authority, "http://" + authority, "", cookie, 404},
		{"GET", "/desktop/connect", authority, "http://" + authority, "", cookie, 404},
		{"GET", "/vendor/novnc/../exec", authority, "", "", cookie, 404},
	}
	for _, test := range cases {
		r := httptest.NewRequest(test.method, "http://"+authority+test.path, strings.NewReader(test.body))
		r.Host = test.host
		if test.origin != "" {
			r.Header.Set("Origin", test.origin)
		}
		r.Header.Set("Content-Type", "application/json")
		if test.cookie != nil {
			r.AddCookie(test.cookie)
		}
		w := httptest.NewRecorder()
		service.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Errorf("%s %s host=%s: %d want %d", test.method, test.path, test.host, w.Code, test.status)
		}
	}
	if extensionCalls.Load() != 0 {
		t.Fatal("private/forbidden request reached extension")
	}
	for _, path := range []string{"/", "/viewer.js", "/viewer.css", "/input.mjs", "/vendor/novnc/core/rfb.js"} {
		if w := request(service, "GET", path, "", nil); w.Code != 200 || !strings.HasPrefix(w.Body.String(), "static ") {
			t.Fatalf("public static asset %s unavailable", path)
		}
	}
}

// TestHumanOwnershipPrivateAgentPreemptionAndForeignStopDenial verifies human ownership private agent preemption and foreign stop denial.
func TestHumanOwnershipPrivateAgentPreemptionAndForeignStopDenial(t *testing.T) {
	service, gate, driver := setup(t, nil)
	one, two := authenticate(t, service), authenticate(t, service)
	agent, err := gate.Acquire(context.Background(), control.Request{Owner: control.Agent, Session: "private-agent", Mode: control.Gated, Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"stop", "release", "renew"} {
		if w := request(service, "POST", "/control", fmt.Sprintf(`{"action":%q}`, action), one); w.Code != 409 {
			t.Fatalf("foreign %s: %d", action, w.Code)
		}
	}
	if view := gate.View(); view.Owner != control.Agent || view.Session != agent.Session {
		t.Fatal("public stop changed foreign owner")
	}
	acquire := request(service, "POST", "/control", `{"action":"acquire"}`, one)
	if acquire.Code != 200 || !state(t, acquire).Owned || gate.View().Owner != control.Human {
		t.Fatalf("human acquisition: %d %s", acquire.Code, acquire.Body.String())
	}
	lease := gate.View()
	if strings.Contains(acquire.Body.String(), "token") {
		t.Fatal("public control leaked actual lease token")
	}
	for _, action := range []string{"stop", "release", "acquire"} {
		if w := request(service, "POST", "/control", fmt.Sprintf(`{"action":%q}`, action), two); w.Code != 409 {
			t.Fatalf("second viewer %s bypass: %d", action, w.Code)
		}
	}
	if gate.View().Session != lease.Session {
		t.Fatal("foreign viewer changed current human")
	}
	if w := request(service, "POST", "/control", `{"action":"renew","leaseMs":60000}`, one); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(service, "POST", "/input", `{"events":[{"type":"text","text":"Hello"}]}`, one); w.Code != 200 || driver.inputs.Load() != 1 {
		t.Fatalf("authorized input: %d", w.Code)
	}
	if w := request(service, "POST", "/input", `{"events":[{"type":"text","text":"foreign"}]}`, two); w.Code != 409 || driver.inputs.Load() != 1 {
		t.Fatal("foreign input reached driver")
	}
	if w := request(service, "POST", "/input", `{"events":[{"type":"text","text":"safe"},{"type":"eval","text":"unsafe"}]}`, one); w.Code != 400 || driver.inputs.Load() != 1 {
		t.Fatal("invalid late event emitted partial input")
	}
	if w := request(service, "POST", "/control", `{"action":"acquire"}`, one); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(service, "POST", "/control", `{"action":"stop"}`, one); w.Code != 200 || gate.View().Owner != "" {
		t.Fatal("owner stop did not release")
	}
}

// TestStrictJSONAndLeaseBounds verifies strict json and lease bounds.
func TestStrictJSONAndLeaseBounds(t *testing.T) {
	service, _, _ := setup(t, nil)
	cookie := authenticate(t, service)
	for _, body := range []string{`null`, `{"action":"acquire","token":"forged"}`, `{"action":"acquire"} {}`, `{"action":"acquire","leaseMs":99}`, `{"action":"acquire","leaseMs":60001}`, `{"action":"acquire","mode":"administrator"}`, `{"action":"destroy"}`, `{"action":"acquire","leaseMs":1.5}`} {
		if w := request(service, "POST", "/control", body, cookie); w.Code != 400 {
			t.Errorf("bad JSON %s accepted: %d", body, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/control", strings.NewReader(`{"action":"acquire"}`))
	r.Host = authority
	r.Header.Set("Origin", "http://"+authority)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	service.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("missing JSON type accepted")
	}
	if w := request(service, "POST", "/control", `{"action":"acquire"}`, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(service, "POST", "/input", strings.Repeat("x", 65<<10), cookie); w.Code != 400 {
		t.Fatal("oversized body accepted")
	}
}

// TestNativeModeResolverAndInputOverride verifies native mode resolver and input override.
func TestNativeModeResolverAndInputOverride(t *testing.T) {
	var extensionSession string
	var inputCalls atomic.Int64
	service, gate, driver := setup(t, func(c *Config) {
		c.NativeViewerID = func(session string) (string, error) {
			extensionSession = session
			return "observed-native-id", nil
		}
		c.Input = func(ctx context.Context, credentials control.Credentials, events []desktop.Event) error {
			inputCalls.Add(1)
			for _, event := range events {
				if err := c.Gate.DispatchHumanGated(ctx, credentials, func(ctx context.Context) error {
					return c.Driver.Input(ctx, event)
				}); err != nil {
					return err
				}
			}
			return nil
		}
	})
	cookie := authenticate(t, service)
	if w := request(service, "POST", "/control", `{"action":"acquire","mode":"native"}`, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if !strings.HasPrefix(extensionSession, "viewer-") || driver.native != "observed-native-id" {
		t.Fatal("native ID was not derived from server session")
	}
	if w := request(service, "POST", "/input", `{"events":[{"type":"text","text":"denied"}]}`, cookie); w.Code != 409 || inputCalls.Load() != 0 {
		t.Fatal("native lease accepted gated HTTP input")
	}
	if gate.View().Mode != control.Native {
		t.Fatal("input denial changed native lease")
	}
	if w := request(service, "POST", "/control", `{"action":"release"}`, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(service, "POST", "/control", `{"action":"acquire"}`, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(service, "POST", "/input", `{"events":[{"type":"text","text":"allowed"}]}`, cookie); w.Code != 200 || inputCalls.Load() != 1 {
		t.Fatal("normalized backend override failed")
	}
	plain, _, _ := setup(t, nil)
	plainCookie := authenticate(t, plain)
	if w := request(plain, "POST", "/control", `{"action":"acquire","mode":"native"}`, plainCookie); w.Code != 422 {
		t.Fatal("unsupported native mode accepted")
	}
}

// TestAdditionalReadOnlyAllowlistAndAuthenticatedScope verifies additional read only allowlist and authenticated scope.
func TestAdditionalReadOnlyAllowlistAndAuthenticatedScope(t *testing.T) {
	var calls atomic.Int64
	service, _, _ := setup(t, func(c *Config) {
		c.ReadOnlyAdditionalPaths = []string{"/api/whoami"}
		c.Additional = func(w http.ResponseWriter, r *http.Request, session string) bool {
			if !strings.HasPrefix(session, "viewer-") {
				t.Error("untrusted extension session")
			}
			if r.URL.Path != "/api/whoami" && r.URL.Path != "/neko/session" {
				return false
			}
			calls.Add(1)
			_, _ = w.Write([]byte("desktop metadata"))
			return true
		}
	})
	cookie := authenticate(t, service)
	if w := request(service, "GET", "/api/whoami", "", cookie); w.Code != 200 {
		t.Fatal("same-origin cookie GET metadata blocked")
	}
	if w := request(service, "GET", "/api/whoami", "", nil); w.Code != 401 {
		t.Fatal("unauthenticated extension allowed")
	}
	if w := request(service, "GET", "/neko/session", "", cookie); w.Code != 404 {
		t.Fatal("unlisted absent-Origin extension allowed")
	}
	if w := request(service, "POST", "/neko/session", `{}`, cookie); w.Code != 200 {
		t.Fatal("exact-Origin extension denied")
	}
	r := httptest.NewRequest("GET", "/api/whoami", nil)
	r.Host = authority
	r.AddCookie(cookie)
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	service.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("foreign-Origin extension allowed")
	}
	r.Header.Del("Origin")
	r.Header.Set("Upgrade", "websocket")
	w = httptest.NewRecorder()
	service.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("Origin-less WebSocket extension allowed")
	}
	if calls.Load() != 2 {
		t.Fatal("extension boundary called unexpectedly")
	}
}

// TestTicketAndSessionCapacityRotationAndServiceClose verifies ticket and session capacity rotation and service close.
func TestTicketAndSessionCapacityRotationAndServiceClose(t *testing.T) {
	service, gate, _ := setup(t, nil)
	var cookies []*http.Cookie
	for i := 0; i < 8; i++ {
		cookies = append(cookies, authenticate(t, service))
	}
	ticket, err := service.NewTicket()
	if err != nil {
		t.Fatal(err)
	}
	if w := request(service, "POST", "/auth", fmt.Sprintf(`{"ticket":%q}`, ticket), nil); w.Code != 429 {
		t.Fatal("session limit bypassed")
	}
	// Rotating an existing browser cookie is explicit and frees exactly that
	// session, releasing its own human lease rather than any other owner.
	if w := request(service, "POST", "/control", `{"action":"acquire"}`, cookies[0]); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	rotated := request(service, "POST", "/auth", fmt.Sprintf(`{"ticket":%q}`, ticket), cookies[0])
	if rotated.Code != 200 || gate.View().Owner != "" {
		t.Fatal("cookie rotation did not retire its own lease")
	}
	if w := request(service, "GET", "/state", "", cookies[0]); w.Code != 401 {
		t.Fatal("retired cookie still valid")
	}
	for i := 0; i < 8; i++ {
		if _, err := service.NewTicket(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.NewTicket(); !errors.Is(err, ErrLimit) {
		t.Fatal("unbounded tickets")
	}
	if err := service.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.NewTicket(); !errors.Is(err, ErrClosed) {
		t.Fatal("closed service minted ticket")
	}
	if w := request(service, "GET", "/state", "", rotated.Result().Cookies()[0]); w.Code != 503 {
		t.Fatal("closed service accepted request")
	}
}

// TestScreenshotReadOnlyAndConcurrencyBound verifies screenshot read only and concurrency bound.
func TestScreenshotReadOnlyAndConcurrencyBound(t *testing.T) {
	service, _, driver := setup(t, nil)
	cookie := authenticate(t, service)
	w := request(service, "GET", "/screenshot", "", cookie)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), driver.capture) {
		t.Fatal("authenticated read-only capture failed")
	}
	driver.blockCapture = make(chan struct{})
	driver.captureEntered = make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(service, "GET", "/screenshot", "", cookie)
	}()
	<-driver.captureEntered
	if w := request(service, "GET", "/screenshot", "", cookie); w.Code != 503 {
		t.Fatal("concurrent capture limit bypassed")
	}
	close(driver.blockCapture)
	if w := <-done; w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	driver.captureErr = errors.New("capture failed")
	if w := request(service, "GET", "/screenshot", "", cookie); w.Code != 503 {
		t.Fatal("capture error hidden")
	}
}

// TestOpaqueSDKTicketQueryIsPathCookieAndOriginScoped verifies opaque sdk ticket query is path cookie and origin scoped.
func TestOpaqueSDKTicketQueryIsPathCookieAndOriginScoped(t *testing.T) {
	var calls atomic.Int64
	service, _, _ := setup(t, func(c *Config) {
		c.OpaqueTicketQueryPaths = []string{"/desktop/stream/api/ws"}
		c.Additional = func(w http.ResponseWriter, r *http.Request, _ string) bool {
			if r.URL.Path != "/desktop/stream/api/ws" {
				return false
			}
			calls.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return true
		}
	})
	cookie := authenticate(t, service)
	opaque, err := secret()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path, origin string
		cookie       *http.Cookie
		status       int
	}{
		{"/desktop/stream/api/ws?token=" + opaque, "http://" + authority, cookie, 204},
		{"/desktop/stream/api/ws?token=" + opaque, "http://" + authority, nil, 401},
		{"/desktop/stream/api/ws?token=" + opaque, "http://evil.example", cookie, 404},
		{"/desktop/stream/api/ws?token=" + opaque, "", cookie, 404},
		{"/desktop/stream/api/ws?token=short", "http://" + authority, cookie, 400},
		{"/desktop/stream/api/ws?token=" + opaque + "&session=forged", "http://" + authority, cookie, 400},
		{"/desktop/stream/api/ws?token=" + opaque + "&token=" + opaque, "http://" + authority, cookie, 400},
		{"/websockify?token=" + opaque, "http://" + authority, cookie, 400},
		{"/desktop/stream/api/ws?token=%QQ", "http://" + authority, cookie, 400},
	}
	for _, test := range cases {
		r := httptest.NewRequest("GET", test.path, nil)
		r.Host = authority
		r.Header.Set("Upgrade", "websocket")
		if test.origin != "" {
			r.Header.Set("Origin", test.origin)
		}
		if test.cookie != nil {
			r.AddCookie(test.cookie)
		}
		w := httptest.NewRecorder()
		service.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Errorf("opaque path %s origin %q: %d want %d", test.path, test.origin, w.Code, test.status)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("SDK token exception escaped authenticated exact path")
	}
}

// TestInvalidOwnedBatchReleasesOnlyItsOwnGatedLease verifies invalid owned batch releases only its own gated lease.
func TestInvalidOwnedBatchReleasesOnlyItsOwnGatedLease(t *testing.T) {
	service, gate, driver := setup(t, nil)
	one, two := authenticate(t, service), authenticate(t, service)
	if w := request(service, "POST", "/control", `{"action":"acquire"}`, one); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	before := driver.resets.Load()
	if w := request(service, "POST", "/input", `{"events":[{"type":"eval"}]}`, two); w.Code != 409 || gate.View().Owner != control.Human || driver.resets.Load() != before {
		t.Fatal("invalid foreign input disturbed owner")
	}
	if w := request(service, "POST", "/input", `{"events":[{"type":"eval"}]}`, one); w.Code != 400 || gate.View().Owner != "" || driver.resets.Load() <= before {
		t.Fatal("invalid own batch left input held under a live lease")
	}
}

// TestAcquireBindingHookFailureReleasesExactLeaseBeforeAcknowledgement verifies acquire binding hook failure releases exact lease before acknowledgement.
func TestAcquireBindingHookFailureReleasesExactLeaseBeforeAcknowledgement(t *testing.T) {
	for _, failure := range []string{"rejected", "panic"} {
		t.Run(failure, func(t *testing.T) {
			var calls int
			var bound control.Lease
			service, gate, driver := setup(t, func(c *Config) {
				c.OnAcquire = func(session string, lease control.Lease) error {
					calls++
					bound = lease
					if lease.Owner != control.Human || lease.Session != session || lease.Token == "" {
						t.Fatal("binding hook received wrong identity")
					}
					if failure == "panic" {
						panic("synthetic binding failure")
					}
					return errors.New("backend viewer reservation vanished")
				}
			})
			cookie := authenticate(t, service)
			before := driver.resets.Load()
			result := request(service, "POST", "/control", `{"action":"acquire"}`, cookie)
			if result.Code != 503 || calls != 1 {
				t.Fatalf("failed binding acknowledged: status=%d calls=%d body=%s", result.Code, calls, result.Body.String())
			}
			if gate.View().Owner != "" || driver.resets.Load() <= before+1 {
				t.Fatal("failed binding did not release/reset exact acquired owner")
			}
			if strings.Contains(result.Body.String(), bound.Token) {
				t.Fatal("binding failure exposed actual token")
			}
			if response := state(t, request(service, "GET", "/state", "", cookie)); response.Owned {
				t.Fatal("cookie retained rejected lease credentials")
			}
		})
	}
}

// TestAcquireBindingHookRenewUsesSameGenerationAndToken verifies acquire binding hook renew uses same generation and token.
func TestAcquireBindingHookRenewUsesSameGenerationAndToken(t *testing.T) {
	var bound []control.Lease
	service, _, _ := setup(t, func(c *Config) {
		c.OnAcquire = func(_ string, lease control.Lease) error {
			bound = append(bound, lease)
			return nil
		}
	})
	cookie := authenticate(t, service)
	for _, action := range []string{"acquire", "renew"} {
		if response := request(service, "POST", "/control", fmt.Sprintf(`{"action":%q}`, action), cookie); response.Code != 200 || !state(t, response).Owned {
			t.Fatalf("%s binding failed: %s", action, response.Body.String())
		}
	}
	if len(bound) != 2 || bound[0].Generation != bound[1].Generation || bound[0].Token != bound[1].Token {
		t.Fatal("renew binding did not retain exact generation/token")
	}
}

// TestAcquireBindingDisconnectCannotRetainOrAcknowledgeNativeLease verifies acquire binding disconnect cannot retain or acknowledge native lease.
func TestAcquireBindingDisconnectCannotRetainOrAcknowledgeNativeLease(t *testing.T) {
	service, gate, _ := setup(t, func(c *Config) {
		c.NativeViewerID = func(string) (string, error) {
			return "connected-native-viewer", nil
		}
		c.OnAcquire = func(_ string, lease control.Lease) error {
			// Simulate SDK disconnect after host transfer but before bind/ACK. The
			// backend releases that exact native lease while the hook is executing.
			return c.Gate.Release(context.Background(), control.Credentials{Session: lease.Session, Token: lease.Token})
		}
	})
	cookie := authenticate(t, service)
	result := request(service, "POST", "/control", `{"action":"acquire","mode":"native"}`, cookie)
	if result.Code != 409 || gate.View().Owner != "" {
		t.Fatalf("disconnected native lease acknowledged/retained: %d %+v", result.Code, gate.View())
	}
	if state(t, request(service, "GET", "/state", "", cookie)).Owned {
		t.Fatal("disconnected native token stored in public session")
	}
}

// TestAcquireBindingCleanupCannotReleaseReplacementOwner verifies acquire binding cleanup cannot release replacement owner.
func TestAcquireBindingCleanupCannotReleaseReplacementOwner(t *testing.T) {
	service, gate, _ := setup(t, func(c *Config) {
		c.OnAcquire = func(_ string, lease control.Lease) error {
			if err := c.Gate.Release(context.Background(), control.Credentials{Session: lease.Session, Token: lease.Token}); err != nil {
				return err
			}
			if _, err := c.Gate.Acquire(context.Background(), control.Request{Owner: control.Human, Session: "replacement-viewer", Mode: control.Gated, Duration: time.Minute}); err != nil {
				return err
			}
			return errors.New("original reservation rejected")
		}
	})
	cookie := authenticate(t, service)
	result := request(service, "POST", "/control", `{"action":"acquire"}`, cookie)
	if result.Code != 503 {
		t.Fatalf("failed original binding acknowledged: %d", result.Code)
	}
	if view := gate.View(); view.Owner != control.Human || view.Session != "replacement-viewer" {
		t.Fatal("hook failure cleanup globally stopped a newer owner")
	}
}

type unavailablePublicDriver struct{ *fakeDriver }

// Ready reports whether the client is ready.
func (*unavailablePublicDriver) Ready() bool {
	return false
}

// TestPublicStateIncludesDriverReadiness verifies public state includes driver readiness.
func TestPublicStateIncludesDriverReadiness(t *testing.T) {
	service, gate, _ := setup(t, func(c *Config) {
		c.Driver = &unavailablePublicDriver{c.Driver.(*fakeDriver)}
	})
	cookie := authenticate(t, service)
	if !gate.View().Ready {
		t.Fatal("test requires otherwise ready ownership manager")
	}
	if state(t, request(service, http.MethodGet, "/state", "", cookie)).Control.Ready {
		t.Fatal("public state ignored unavailable desktop driver")
	}
}
