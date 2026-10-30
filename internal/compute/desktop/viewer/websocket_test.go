package viewer

import (
	"context"
	"io"
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
)

func openRFBForTest(t *testing.T, ctx context.Context, server *httptest.Server, cookie *http.Cookie, listener net.Listener) *websocket.Conn {
	t.Helper()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		accepted <- conn
	}()
	conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/websockify", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{server.URL}, "Cookie": []string{cookie.String()}}})
	if err != nil {
		t.Fatalf("open RFB: %+v %v", response, err)
	}
	select {
	case upstream := <-accepted:
		if upstream == nil {
			t.Fatal("RFB upstream not connected")
		}
		t.Cleanup(func() {
			_ = upstream.Close()
		})
	case <-ctx.Done():
		t.Fatal("RFB upstream accept timed out")
	}
	t.Cleanup(func() {
		_ = conn.CloseNow()
	})
	return conn
}

func waitViewerStreamClosed(t *testing.T, service *Service, cookie *http.Cookie) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		service.mu.Lock()
		viewer := service.sessions[cookie.Value]
		closed := viewer != nil && !viewer.websocket
		service.mu.Unlock()
		if closed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("RFB stream cleanup did not complete")
}

// TestObserverStreamDisconnectReleasesLaterAcquiredLease verifies observer stream disconnect releases later acquired lease.
func TestObserverStreamDisconnectReleasesLaterAcquiredLease(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	service, gate, driver := setup(t, func(c *Config) {
		c.RFBTarget = listener.Addr().String()
	})
	cookie := authenticate(t, service)
	server := httptest.NewServer(service)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := openRFBForTest(t, ctx, server, cookie, listener)
	epoch := take(t, service, cookie)
	if take(t, service, cookie) != epoch {
		t.Fatal("repeated Take changed the epoch")
	}
	response := epochRequest(service, "/input", `{"events":[{"type":"text","text":"current"}]}`, cookie, strconv.FormatUint(epoch, 10))
	if response.Code != http.StatusOK || driver.inputs.Load() != 1 {
		t.Fatal("current human input was not accepted")
	}
	before := driver.resets.Load()
	_ = conn.CloseNow()
	waitViewerStreamClosed(t, service, cookie)
	if gate.View().Owner != "" || driver.resets.Load() <= before {
		t.Fatal("disconnect did not release and reset its later-acquired lease")
	}
	if response := request(service, http.MethodPost, "/control", `{"action":"acquire"}`, cookie); response.Code != http.StatusConflict {
		t.Fatalf("acquire without replacement stream: %d", response.Code)
	}
}

type blockingReleaseGate struct {
	Gate
	entered, proceed chan struct{}
	once             sync.Once
}

// Release waits for the test to permit lease cleanup.
func (g *blockingReleaseGate) Release(ctx context.Context, c control.Credentials) error {
	g.once.Do(func() {
		close(g.entered)
	})
	select {
	case <-g.proceed:
	case <-ctx.Done():
		return ctx.Err()
	}
	return g.Gate.Release(ctx, c)
}

// TestRFBDisconnectCleanupCannotRevokeReplacementStream verifies rfb disconnect cleanup cannot revoke replacement stream.
func TestRFBDisconnectCleanupCannotRevokeReplacementStream(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var blocking *blockingReleaseGate
	service, gate, _ := setup(t, func(c *Config) {
		c.RFBTarget = listener.Addr().String()
		blocking = &blockingReleaseGate{Gate: c.Gate, entered: make(chan struct{}), proceed: make(chan struct{})}
		c.Gate = blocking
	})
	cookie := authenticate(t, service)
	server := httptest.NewServer(service)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	old := openRFBForTest(t, ctx, server, cookie, listener)
	oldEpoch := take(t, service, cookie)
	_ = old.CloseNow()
	select {
	case <-blocking.entered:
	case <-ctx.Done():
		t.Fatal("disconnect did not reach lease cleanup")
	}
	other, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/websockify", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{server.URL}, "Cookie": []string{cookie.String()}}})
	if err == nil {
		_ = other.CloseNow()
		t.Fatal("replacement stream admitted before old cleanup")
	}
	if response == nil || response.StatusCode != http.StatusConflict {
		t.Fatalf("unexpected replacement status: %+v %v", response, err)
	}
	close(blocking.proceed)
	waitViewerStreamClosed(t, service, cookie)
	fresh := openRFBForTest(t, ctx, server, cookie, listener)
	newEpoch := take(t, service, cookie)
	if newEpoch == oldEpoch || gate.View().Owner != control.Human {
		t.Fatal("replacement did not obtain a fresh lease")
	}
	response2 := epochRequest(service, "/input", `{"events":[{"type":"text","text":"replacement"}]}`, cookie, strconv.FormatUint(newEpoch, 10))
	if response2.Code != http.StatusOK {
		t.Fatalf("replacement input rejected: %d", response2.Code)
	}
	_ = fresh.CloseNow()
	waitViewerStreamClosed(t, service, cookie)
}

// TestRFBDisconnectDuringAcquireCannotPublishToReplacement verifies rfb disconnect during acquire cannot publish to replacement.
func TestRFBDisconnectDuringAcquireCannotPublishToReplacement(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	service, gate, _ := setup(t, func(c *Config) {
		c.RFBTarget = listener.Addr().String()
		c.OnAcquire = func(string, control.Lease) error {
			once.Do(func() {
				close(entered)
				<-proceed
			})
			return nil
		}
	})
	cookie := authenticate(t, service)
	server := httptest.NewServer(service)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	old := openRFBForTest(t, ctx, server, cookie, listener)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(service, http.MethodPost, "/control", `{"action":"acquire"}`, cookie)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("acquire did not reach binding hook")
	}
	_ = old.CloseNow()
	waitViewerStreamClosed(t, service, cookie)
	fresh := openRFBForTest(t, ctx, server, cookie, listener)
	close(proceed)
	select {
	case response := <-done:
		if response.Code == http.StatusOK {
			t.Fatal("old stream acquisition was published after replacement")
		}
	case <-ctx.Done():
		t.Fatal("in-flight acquisition did not finish")
	}
	if gate.View().Owner != "" {
		t.Fatal("in-flight old acquisition left control held")
	}
	take(t, service, cookie)
	if gate.View().Owner != control.Human {
		t.Fatal("replacement could not take control")
	}
	_ = fresh.CloseNow()
	waitViewerStreamClosed(t, service, cookie)
}

// TestPreStreamAcquireCannotMigrateAcrossStreams verifies pre stream acquire cannot migrate across streams.
func TestPreStreamAcquireCannotMigrateAcrossStreams(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	service, gate, _ := setup(t, func(c *Config) {
		c.RFBTarget = listener.Addr().String()
		c.OnAcquire = func(string, control.Lease) error {
			once.Do(func() {
				close(entered)
				<-proceed
			})
			return nil
		}
	})
	cookie := authenticate(t, service)
	server := httptest.NewServer(service)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(service, http.MethodPost, "/control", `{"action":"acquire"}`, cookie)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("pre-stream acquisition did not reach binding hook")
	}
	old := openRFBForTest(t, ctx, server, cookie, listener)
	_ = old.CloseNow()
	waitViewerStreamClosed(t, service, cookie)
	fresh := openRFBForTest(t, ctx, server, cookie, listener)
	close(proceed)
	select {
	case response := <-done:
		if response.Code == http.StatusOK {
			t.Fatal("pre-stream acquisition migrated across a disconnected stream")
		}
	case <-ctx.Done():
		t.Fatal("pre-stream acquisition did not finish")
	}
	if gate.View().Owner != "" {
		t.Fatal("pre-stream acquisition left control held")
	}
	take(t, service, cookie)
	if gate.View().Owner != control.Human {
		t.Fatal("replacement stream could not take control")
	}
	_ = fresh.CloseNow()
	waitViewerStreamClosed(t, service, cookie)
}

// TestActualWebSocketCookieLeaseAndSingleConnection verifies actual web socket cookie lease and single connection.
func TestActualWebSocketCookieLeaseAndSingleConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	service, gate, _ := setup(t, func(c *Config) {
		c.RFBTarget = listener.Addr().String()
	})
	cookie := authenticate(t, service)
	initial := request(service, "POST", "/control", `{"action":"acquire"}`, cookie)
	if initial.Code != http.StatusOK {
		t.Fatal(initial.Body.String())
	}
	initialEpoch := state(t, initial).Epoch
	server := httptest.NewServer(service)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		accepted <- conn
	}()
	headers := http.Header{"Origin": []string{server.URL}, "Cookie": []string{cookie.String()}}
	connection, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/websockify", &websocket.DialOptions{HTTPHeader: headers, Subprotocols: []string{"binary"}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	upstream := <-accepted
	if upstream == nil {
		t.Fatal("private RFB was not connected")
	}
	defer upstream.Close()
	if duplicate, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/websockify", &websocket.DialOptions{HTTPHeader: headers}); err == nil {
		duplicate.CloseNow()
		t.Fatal("same-cookie parallel connection accepted")
	} else if response == nil || response.StatusCode != http.StatusConflict {
		t.Fatalf("parallel connection status: %+v %v", response, err)
	}
	want := append([]byte("RFB 003.008\n\x01\x01"), []byte{3, 1, 0, 0, 0, 0, 5, 0, 2, 208}...)
	read := make(chan []byte, 1)
	go func() {
		data := make([]byte, len(want))
		_, _ = io.ReadFull(upstream, data)
		read <- data
	}()
	if err := connection.Write(ctx, websocket.MessageBinary, want); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-read:
		if string(got) != string(want) {
			t.Fatalf("read-only RFB request bytes: %x", got)
		}
	case <-ctx.Done():
		t.Fatal("read-only RFB request missing")
	}
	if gate.View().Owner != control.Human {
		t.Fatal("human lease disappeared during read-only video")
	}
	// Repeated Take renews the same epoch. Keeping the existing stream must
	// remain usable; disconnect/reconnect is a frontend decision only on a
	// changed epoch, not an automatic side effect of every acquisition.
	repeated := request(service, "POST", "/control", `{"action":"acquire"}`, cookie)
	if repeated.Code != http.StatusOK || state(t, repeated).Epoch != initialEpoch {
		t.Fatal("repeated Take changed current RFB epoch")
	}
	second := []byte{3, 1, 0, 0, 0, 0, 5, 0, 2, 208}
	secondRead := make(chan []byte, 1)
	go func() {
		got := make([]byte, len(second))
		_, _ = io.ReadFull(upstream, got)
		secondRead <- got
	}()
	if err := connection.Write(ctx, websocket.MessageBinary, second); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-secondRead:
		if string(got) != string(second) {
			t.Fatal("repeated Take invalidated existing RFB stream")
		}
	case <-ctx.Done():
		t.Fatal("repeated Take stream stopped dispatching")
	}
	_ = connection.CloseNow()
	deadline := time.Now().Add(time.Second)
	for gate.View().Owner != "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if gate.View().Owner != "" {
		t.Fatal("disconnect did not release actual human lease")
	}
}

// TestActualWebSocketObserverCannotUsePrivateAgentOwner verifies actual web socket observer cannot use private agent owner.
func TestActualWebSocketObserverCannotUsePrivateAgentOwner(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	service, gate, _ := setup(t, func(c *Config) {
		c.RFBTarget = listener.Addr().String()
	})
	cookie := authenticate(t, service)
	if _, err := gate.Acquire(context.Background(), control.Request{Owner: control.Agent, Session: "private-agent", Duration: time.Minute, Mode: control.Gated}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(service)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	forwarded := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			forwarded <- nil
			return
		}
		defer conn.Close()
		data, _ := io.ReadAll(conn)
		forwarded <- data
	}()
	connection, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/websockify", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{server.URL}, "Cookie": []string{cookie.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	if err := connection.Write(ctx, websocket.MessageBinary, []byte("RFB 003.008\n\x01\x01\x05\x01\x00\x01\x00\x01")); err != nil {
		t.Fatal(err)
	}
	_, _, _ = connection.Read(ctx)
	select {
	case got := <-forwarded:
		if string(got) != "RFB 003.008\n\x01\x01" {
			t.Fatalf("observer mutating bytes escaped: %x", got)
		}
	case <-ctx.Done():
		t.Fatal("observer connection not closed after denied input")
	}
	if gate.View().Owner != control.Agent {
		t.Fatal("observer disconnect released foreign private agent")
	}
}

// TestCurrentHumanCookieCannotInjectRFBAndDisconnectReleasesDirectInput verifies current human cookie cannot inject rfb and disconnect releases direct input.
func TestCurrentHumanCookieCannotInjectRFBAndDisconnectReleasesDirectInput(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	service, gate, driver := setup(t, func(c *Config) {
		c.RFBTarget = listener.Addr().String()
	})
	cookie := authenticate(t, service)
	take(t, service, cookie)
	before := driver.resets.Load()
	server := httptest.NewServer(service)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	captured := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			captured <- nil
			return
		}
		defer conn.Close()
		data, _ := io.ReadAll(conn)
		captured <- data
	}()
	connection, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/websockify", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{server.URL}, "Cookie": []string{cookie.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	handshake := []byte("RFB 003.008\n\x01\x01")
	payload := append(append([]byte(nil), handshake...), []byte{4, 1, 0, 0, 0, 0, 0, 65}...)
	if err := connection.Write(ctx, websocket.MessageBinary, payload); err != nil {
		t.Fatal(err)
	}
	_, _, _ = connection.Read(ctx)
	select {
	case got := <-captured:
		if string(got) != string(handshake) {
			t.Fatalf("human lease leaked RFB key bytes: %x", got)
		}
	case <-ctx.Done():
		t.Fatal("malicious human RFB connection was not closed")
	}
	deadline := time.Now().Add(time.Second)
	for gate.View().Owner != "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if gate.View().Owner != "" || driver.resets.Load() <= before {
		t.Fatal("malicious connection close failed to release its direct-input lease")
	}
}
