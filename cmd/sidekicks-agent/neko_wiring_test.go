package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/compute/control"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop/viewer"
	"github.com/kevinmartin/sidekicks/internal/compute/neko"
)

// A real net/http server and WebSocket codec over net.Pipe lets this command
// wiring test exercise serialization and disconnects without binding host ports
// or attaching to an installed browser, X server or running POC.
func commandPipeServer(t *testing.T, port string, handler http.Handler) *http.Client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	listener := &pipeListener{connections: make(chan net.Conn, 32), done: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, listener, handler)
	}()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
		if address != "127.0.0.1:"+port {
			return nil, errors.New("test refused non-private http target")
		}
		server, client := net.Pipe()
		select {
		case listener.connections <- server:
			return client, nil
		case <-ctx.Done():
			server.Close()
			client.Close()
			return nil, ctx.Err()
		case <-listener.done:
			server.Close()
			client.Close()
			return nil, net.ErrClosed
		}
	}}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	t.Cleanup(func() {
		cancel()
		transport.CloseIdleConnections()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("serialized HTTP test server did not stop")
		}
	})
	return client
}
func commandNekoClient(t *testing.T) (*neko.Client, *sync.Mutex, *[]string) {
	t.Helper()
	var mu sync.Mutex
	calls := []string{}
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/api/login":
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				w.WriteHeader(400)
				return
			}
			if body["username"] == "controller" {
				_, _ = w.Write([]byte(`{"id":"admin-id","token":"private-upstream-admin-token","profile":{"is_admin":true}}`))
				return
			}
			if strings.HasPrefix(body["username"], "viewer-") {
				_, _ = w.Write([]byte(`{"id":"viewer-id","token":"private-upstream-viewer-token","profile":{"can_login":true,"can_connect":true,"can_watch":true,"can_host":false}}`))
				return
			}
			w.WriteHeader(401)
		case "/api/ws":
			conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
			if err != nil {
				return
			}
			defer conn.CloseNow()
			if conn.Write(r.Context(), websocket.MessageText, []byte(`{"event":"system/init","payload":{"screen_size":{"width":1280,"height":720},"settings":{"locked_controls":true,"implicit_hosting":false}}}`)) != nil {
				return
			}
			for {
				if _, _, err = conn.Read(r.Context()); err != nil {
					return
				}
			}
		case "/api/whoami", "/api/room/screen", "/api/room/screen/configurations":
			if r.Header.Get("Authorization") != "Bearer private-upstream-viewer-token" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"id":"viewer-id","profile":{"is_admin":false}}`))
		default:
			if r.Header.Get("Authorization") != "Bearer private-upstream-admin-token" {
				w.WriteHeader(401)
				return
			}
			w.WriteHeader(204)
		}
	})
	client, err := neko.New(neko.Config{Endpoint: "http://127.0.0.1:17778", AdminUsername: "controller", AdminPassword: "unit-private-admin", ViewerPassword: "unit-private-view"}, neko.WithHTTPClient(commandPipeServer(t, "17778", upstream)))
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = client.Close(ctx)
	})
	return client, &mu, &calls
}
func wiringViewerCall(service http.Handler, method, path, body string, cookie *http.Cookie, opaque string, epoch ...uint64) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://127.0.0.1:17777"+path, strings.NewReader(body))
	request.Header.Set("Origin", "http://127.0.0.1:17777")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if opaque != "" {
		request.Header.Set("Authorization", "Bearer "+opaque)
	}
	if len(epoch) > 0 {
		request.Header.Set(viewer.EpochHeader, strconv.FormatUint(epoch[0], 10))
	}
	response := httptest.NewRecorder()
	service.ServeHTTP(response, request)
	return response
}
func authorizeWiringViewer(t *testing.T, service *viewer.Service) *http.Cookie {
	t.Helper()
	ticket, err := service.NewTicket()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"ticket": ticket})
	response := wiringViewerCall(service, "POST", "/auth", string(body), nil, "")
	cookies := response.Result().Cookies()
	if response.Code != 200 || len(cookies) != 1 {
		t.Fatal("viewer authentication failed", response.Code, response.Body)
	}
	return cookies[0]
}

// TestNekoRootViewerWiringBindsSDKDisconnectToIssuedHumanLease verifies neko root viewer wiring binds sdk disconnect to issued human lease.
func TestNekoRootViewerWiringBindsSDKDisconnectToIssuedHumanLease(t *testing.T) {
	d, backend, _, _ := newUnitDesktop(t)
	client, _, _ := commandNekoClient(t)
	oldGate := d.gate
	_ = oldGate.Close(context.Background())
	d.gate = control.New(nekoReset{client: client, x: d.driver})
	d.nekoClient = client
	handler, err := neko.NewHandler(client, d.gate, neko.HandlerOptions{Token: orchestrationToken})
	if err != nil {
		t.Fatal(err)
	}
	d.nekoHandler = handler
	service, err := viewer.New(d.nekoViewerConfig(handler))
	if err != nil {
		t.Fatal(err)
	}
	d.viewer = service
	defer d.close()
	cookie := authorizeWiringViewer(t, service)
	response := wiringViewerCall(service, "POST", "/viewer", `{}`, cookie, "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body)
	}
	var ticket neko.SDKViewerTicket
	if json.Unmarshal(response.Body.Bytes(), &ticket) != nil || len(ticket.Token) != 43 || strings.Contains(response.Body.String(), "private-upstream") {
		t.Fatal("upstream credential crossed viewer boundary")
	}
	// A ticket minted for this cookie must not work with a second cookie session.
	other := authorizeWiringViewer(t, service)
	response = wiringViewerCall(service, "GET", "/desktop/stream/api/whoami", "", other, ticket.Token)
	if response.Code != 403 {
		t.Fatal("SDK capability crossed cookie session", response.Code)
	}
	response = wiringViewerCall(service, "GET", "/desktop/stream/api/whoami", "", cookie, ticket.Token)
	if response.Code != 200 || strings.Contains(response.Body.String(), "private-upstream") {
		t.Fatal("trusted metadata did not proxy safely", response.Code, response.Body)
	}
	gatewayClient := commandPipeServer(t, "17777", service)
	header := http.Header{"Origin": {"http://127.0.0.1:17777"}, "Cookie": {cookie.Name + "=" + cookie.Value}}
	stream, _, err := websocket.Dial(context.Background(), "ws://127.0.0.1:17777/desktop/stream/api/ws?token="+ticket.Token, &websocket.DialOptions{HTTPClient: gatewayClient, HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.CloseNow()
	read, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err = stream.Read(read); err != nil {
		t.Fatal("upstream session was not observed connected", err)
	}
	response = wiringViewerCall(service, "POST", "/control", `{"action":"acquire","mode":"gated"}`, cookie, "")
	if response.Code != 200 || d.gate.View().Session != ticket.Session {
		t.Fatal("public acquisition was not bound to SDK session", response.Code, response.Body)
	}
	var acquired viewer.Response
	if json.Unmarshal(response.Body.Bytes(), &acquired) != nil || acquired.Epoch == 0 {
		t.Fatal("viewer did not return its current generation")
	}
	response = wiringViewerCall(service, "POST", "/input", `{"events":[{"type":"key","key":"Control","action":"down"}]}`, cookie, "", acquired.Epoch)
	if response.Code != 200 || !backend.held() {
		t.Fatal("actual simulated backend hold was not established", response.Code, response.Body)
	}
	stream.CloseNow()
	deadline := time.Now().Add(time.Second)
	for d.gate.View().Owner != "" || backend.held() {
		if time.Now().After(deadline) {
			t.Fatal("SDK disconnect failed to release the root-wired issued lease/input")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestNekoRootAdditionalRoutesRejectMalformedAndForeignCapabilities verifies neko root additional routes reject malformed and foreign capabilities.
func TestNekoRootAdditionalRoutesRejectMalformedAndForeignCapabilities(t *testing.T) {
	d, _, _, _ := newUnitDesktop(t)
	client, err := neko.New(neko.Config{Endpoint: "http://127.0.0.1:8082", AdminUsername: "controller", AdminPassword: "unit-admin", ViewerPassword: "unit-view"})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := neko.NewHandler(client, d.gate, neko.HandlerOptions{Token: orchestrationToken})
	if err != nil {
		t.Fatal(err)
	}
	additional := d.nekoViewerConfig(handler).Additional
	for _, test := range []struct {
		method, path, body string
		status             int
	}{{"GET", "/viewer", "", 405}, {"POST", "/viewer", `{"unknown":true}`, 400}, {"POST", "/viewer", `{}`, 503}, {"GET", "/desktop/stream/api/whoami", "", 403}, {"GET", "/desktop/stream/api/ws?token=forged", "", 403}} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		if !additional(response, request, "viewer-local") || response.Code != test.status {
			t.Fatal(test.method, test.path, response.Code, response.Body)
		}
	}
	if additional(httptest.NewRecorder(), httptest.NewRequest("POST", "/exec", nil), "viewer-local") {
		t.Fatal("broad machine route was delegated")
	}
}

// TestNekoResetStillClearsX11WhenControllerIsAlreadyUnavailable verifies neko reset still clears x11 when controller is already unavailable.
func TestNekoResetStillClearsX11WhenControllerIsAlreadyUnavailable(t *testing.T) {
	d, backend, _, _ := newUnitDesktop(t)
	lease := acquireUnit(t, d, control.Agent, "before-disconnect")
	if err := d.gate.DispatchGated(context.Background(), control.Credentials{Session: lease.Session, Token: lease.Token}, func(ctx context.Context) error {
		return d.driver.Input(ctx, desktopKeyDown())
	}); err != nil {
		t.Fatal(err)
	}
	client, err := neko.New(neko.Config{Endpoint: "http://127.0.0.1:8082", AdminUsername: "controller", AdminPassword: "unit-admin", ViewerPassword: "unit-view"})
	if err != nil {
		t.Fatal(err)
	}
	if err = (nekoReset{client: client, x: d.driver}).Reset(context.Background()); err == nil || backend.held() {
		t.Fatal("upstream failure hid cleanup failure or skipped actual X11 release", err)
	}
}
