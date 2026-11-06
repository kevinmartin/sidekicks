package desktop

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/compute/control"
)

func gatewayConfig(gate HumanGate) Config {
	return Config{Target: "127.0.0.1:5900", Gate: gate, Authenticate: func(*http.Request) (control.Credentials, error) {
		return control.Credentials{}, nil
	}, AllowedHosts: []string{"127.0.0.1:7901"}, AllowedOrigins: []string{"http://127.0.0.1:7901"}}
}

// TestGatewayRejectsUnsafeConfiguration verifies gateway rejects unsafe configuration.
func TestGatewayRejectsUnsafeConfiguration(t *testing.T) {
	gate, _, _ := acquiredGate(t, control.Human)
	cases := []func(*Config){
		func(c *Config) {
			c.Target = "0.0.0.0:5900"
		}, func(c *Config) {
			c.Target = "localhost:5900"
		},
		func(c *Config) {
			c.Target = "192.168.0.10:5900"
		}, func(c *Config) {
			c.Target = "[::1%lo0]:5900"
		},
		func(c *Config) {
			c.Target = "127.0.0.1:0"
		}, func(c *Config) {
			c.Target = "127.0.0.1:65536"
		},
		func(c *Config) {
			c.Target = "127.0.0.1"
		}, func(c *Config) {
			c.Gate = nil
		}, func(c *Config) {
			c.Authenticate = nil
		},
		func(c *Config) {
			c.AllowedHosts = nil
		}, func(c *Config) {
			c.AllowedOrigins = nil
		},
		func(c *Config) {
			c.AllowedHosts = []string{"*.example.com"}
		}, func(c *Config) {
			c.AllowedHosts = []string{"user@host"}
		},
		func(c *Config) {
			c.AllowedOrigins = []string{"null"}
		}, func(c *Config) {
			c.AllowedOrigins = []string{"http://127.0.0.1:7901/"}
		},
		func(c *Config) {
			c.AllowedOrigins = []string{"http://evil.example"}
		},
		func(c *Config) {
			c.AllowedOrigins = []string{"http://user@127.0.0.1:7901"}
		},
		func(c *Config) {
			c.Path = "/websockify?token=secret"
		},
	}
	for i, mutate := range cases {
		c := gatewayConfig(gate)
		mutate(&c)
		if _, err := NewGateway(c); err == nil {
			t.Errorf("unsafe config %d accepted", i)
		}
	}
	c := gatewayConfig(gate)
	c.Target = "[::1]:5900"
	if _, err := NewGateway(c); err != nil {
		t.Fatalf("IPv6 loopback rejected: %v", err)
	}
}

// TestGatewayBoundariesBeforeAnyDial verifies gateway boundaries before any dial.
func TestGatewayBoundariesBeforeAnyDial(t *testing.T) {
	gate, _, _ := acquiredGate(t, control.Human)
	c := gatewayConfig(gate)
	var dials atomic.Int64
	c.DialContext = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("unavailable")
	}
	c.Authenticate = func(r *http.Request) (control.Credentials, error) {
		if r.Header.Get("Authorization") != "Bearer private-instance" {
			return control.Credentials{}, errors.New("bad credential")
		}
		return control.Credentials{}, nil
	}
	gateway, err := NewGateway(c)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, method, path, host, origin, auth string
		status                                 int
	}{
		{"host", "GET", "/websockify", "evil.example", "http://127.0.0.1:7901", "Bearer private-instance", 403},
		{"origin", "GET", "/websockify", "127.0.0.1:7901", "http://evil.example", "Bearer private-instance", 403},
		{"no-origin", "GET", "/websockify", "127.0.0.1:7901", "", "Bearer private-instance", 403},
		{"cross-variant", "GET", "/websockify", "127.0.0.1:7901", "http://127.0.0.1:7902", "Bearer private-instance", 403},
		{"auth", "GET", "/websockify", "127.0.0.1:7901", "http://127.0.0.1:7901", "", 401},
		{"query-token", "GET", "/websockify?token=secret", "127.0.0.1:7901", "http://127.0.0.1:7901", "Bearer private-instance", 404},
		{"wrong-path", "GET", "/private/exec", "127.0.0.1:7901", "http://127.0.0.1:7901", "Bearer private-instance", 404},
		{"encoded-path", "GET", "/web%73ockify", "127.0.0.1:7901", "http://127.0.0.1:7901", "Bearer private-instance", 404},
		{"method", "POST", "/websockify", "127.0.0.1:7901", "http://127.0.0.1:7901", "Bearer private-instance", 405},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(test.method, test.path, nil)
			r.Host = test.host
			if test.origin != "" {
				r.Header.Set("Origin", test.origin)
			}
			r.Header.Set("Authorization", test.auth)
			w := httptest.NewRecorder()
			gateway.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status %d want %d", w.Code, test.status)
			}
		})
	}
	if dials.Load() != 0 {
		t.Fatal("rejected request dialed private RFB")
	}
	r := httptest.NewRequest("GET", "/websockify", nil)
	r.Host = "127.0.0.1:7901"
	r.Header.Set("Authorization", "Bearer private-instance")
	r.Header.Add("Origin", "http://127.0.0.1:7901")
	r.Header.Add("Origin", "http://127.0.0.1:7901")
	w := httptest.NewRecorder()
	gateway.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("duplicate Origin accepted")
	}
	r.Header.Set("Origin", "http://127.0.0.1:7901")
	w = httptest.NewRecorder()
	gateway.ServeHTTP(w, r)
	if w.Code != 503 || dials.Load() != 1 {
		t.Fatalf("upstream failure: %d dials %d", w.Code, dials.Load())
	}
}

// TestGatewayActualWebSocketBinaryRFBAndDisconnectCleanup verifies gateway actual web socket binary rfb and disconnect cleanup.
func TestGatewayActualWebSocketBinaryRFBAndDisconnectCleanup(t *testing.T) {
	gate, credentials, _ := acquiredGate(t, control.Human)
	server := httptest.NewUnstartedServer(nil)
	host := server.Listener.Addr().String()
	c := gatewayConfig(gate)
	c.AllowedHosts = []string{host}
	c.AllowedOrigins = []string{"http://" + host}
	c.Authenticate = func(r *http.Request) (control.Credentials, error) {
		if r.Header.Get("Authorization") != "Bearer private-instance" {
			return control.Credentials{}, errors.New("missing")
		}
		return credentials, nil
	}
	upstream, rfb := net.Pipe()
	defer rfb.Close()
	var dials atomic.Int64
	c.DialContext = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return upstream, nil
	}
	gateway, err := NewGateway(c)
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = gateway
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/websockify", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{server.URL}, "Authorization": []string{"Bearer private-instance"}}, Subprotocols: []string{"binary"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	expected := append([]byte("RFB 003.008\n\x01\x01"), []byte{3, 1, 0, 0, 0, 0, 5, 0, 2, 208}...)
	serverDone := make(chan error, 1)
	go func() {
		got := make([]byte, len(expected))
		_, err := io.ReadFull(rfb, got)
		if err == nil && string(got) != string(expected) {
			err = errors.New("bad rfb bytes")
		}
		if err == nil {
			_, err = rfb.Write([]byte("actual upstream bytes"))
		}
		serverDone <- err
	}()
	// RFB fragmentation is independent from WebSocket message fragmentation.
	for _, packet := range [][]byte{expected[:5], expected[5:15], expected[15:]} {
		if err := conn.Write(ctx, websocket.MessageBinary, packet); err != nil {
			t.Fatal(err)
		}
	}
	kind, data, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageBinary || string(data) != "actual upstream bytes" {
		t.Fatalf("stream: %d %q %v", kind, data, err)
	}
	if err := await(t, serverDone); err != nil {
		t.Fatal(err)
	}
	_ = conn.CloseNow()
	deadline := time.Now().Add(time.Second)
	for gate.View().Owner != "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if gate.View().Owner != "" || dials.Load() != 1 {
		t.Fatal("WebSocket disconnect did not release lease")
	}
}

// TestGatewayTextMessageRejectedWithoutRFBForward verifies gateway text message rejected without rfb forward.
func TestGatewayTextMessageRejectedWithoutRFBForward(t *testing.T) {
	gate, credentials, _ := acquiredGate(t, control.Human)
	server := httptest.NewUnstartedServer(nil)
	host := server.Listener.Addr().String()
	c := gatewayConfig(gate)
	c.AllowedHosts = []string{host}
	c.AllowedOrigins = []string{"http://" + host}
	c.Authenticate = func(*http.Request) (control.Credentials, error) {
		return credentials, nil
	}
	upstream, rfb := net.Pipe()
	defer rfb.Close()
	c.DialContext = func(context.Context, string, string) (net.Conn, error) {
		return upstream, nil
	}
	gateway, err := NewGateway(c)
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = gateway
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/websockify", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{server.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageText, []byte("RFB 003.008\n")); err != nil {
		t.Fatal(err)
	}
	_, _, err = conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusUnsupportedData {
		t.Fatalf("text close: %v", err)
	}
	_ = rfb.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1)
	if n, _ := rfb.Read(buf); n != 0 {
		t.Fatal("text RFB was forwarded")
	}
}

// TestGatewayConnectionLimitAndNilDial verifies gateway connection limit and nil dial.
func TestGatewayConnectionLimitAndNilDial(t *testing.T) {
	gate, _, _ := acquiredGate(t, control.Human)
	c := gatewayConfig(gate)
	c.MaxConnections = 1
	var calls atomic.Int64
	c.DialContext = func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, nil
	}
	g, err := NewGateway(c)
	if err != nil {
		t.Fatal(err)
	}
	request := func() *http.Request {
		r := httptest.NewRequest("GET", "/websockify", nil)
		r.Host = "127.0.0.1:7901"
		r.Header.Set("Origin", "http://127.0.0.1:7901")
		return r
	}
	g.connections <- struct{}{}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, request())
	if w.Code != 503 || calls.Load() != 0 {
		t.Fatal("connection limit bypassed")
	}
	<-g.connections
	w = httptest.NewRecorder()
	g.ServeHTTP(w, request())
	if w.Code != 503 || calls.Load() != 1 {
		t.Fatal("nil upstream accepted")
	}
	if len(g.connections) != 0 {
		t.Fatal("failed dial leaked viewer slot")
	}
	for _, bad := range []int{-1, 33} {
		c.MaxConnections = bad
		if _, err := NewGateway(c); err == nil {
			t.Fatal("bad connection bound accepted")
		}
	}
}
