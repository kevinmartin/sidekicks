package desktop

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute/control"
)

type testClient struct {
	read    chan []byte
	written chan []byte
	closed  chan struct{}
	once    sync.Once
}

func newTestClient() *testClient {
	return &testClient{read: make(chan []byte, 16), written: make(chan []byte, 16), closed: make(chan struct{})}
}

// Read returns the next queued message or a cancellation error.
func (c *testClient) Read(ctx context.Context) ([]byte, error) {
	select {
	case d := <-c.read:
		return d, nil
	case <-c.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Write writes data from the supplied buffer.
func (c *testClient) Write(ctx context.Context, d []byte) error {
	select {
	case c.written <- bytes.Clone(d):
		return nil
	case <-c.closed:
		return io.EOF
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close releases the receiver's resources.
func (c *testClient) Close() error {
	c.once.Do(func() {
		close(c.closed)
	})
	return nil
}

type resetDriver struct {
	calls   atomic.Int64
	failure atomic.Bool
}

// Reset records a reset and returns a configured failure.
func (d *resetDriver) Reset(context.Context) error {
	d.calls.Add(1)
	if d.failure.Load() {
		return errors.New("reset failed")
	}
	return nil
}
func acquiredGate(t *testing.T, owner control.Owner) (*control.Manager, control.Credentials, *resetDriver) {
	t.Helper()
	driver := &resetDriver{}
	gate := control.New(driver)
	lease, err := gate.Acquire(context.Background(), control.Request{Owner: owner, Session: "viewer-test", Mode: control.Gated, Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = gate.Close(context.Background())
	})
	return gate, control.Credentials{Session: lease.Session, Token: lease.Token}, driver
}
func await(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("bridge goroutine did not exit")
		return nil
	}
}

// TestBridgeForwardsReadOnlyVideoAndReleasesDirectInputOnDisconnect verifies bridge forwards read only video and releases direct input on disconnect.
func TestBridgeForwardsReadOnlyVideoAndReleasesDirectInputOnDisconnect(t *testing.T) {
	gate, credentials, driver := acquiredGate(t, control.Human)
	client := newTestClient()
	upstream, server := net.Pipe()
	defer server.Close()
	counters := &Counters{}
	done := make(chan error, 1)
	go func() {
		done <- Bridge(context.Background(), client, upstream, gate, credentials, counters)
	}()
	serverDone := make(chan error, 1)
	want := append([]byte("RFB 003.008\n\x01\x01"), []byte{3, 1, 0, 0, 0, 0, 5, 0, 2, 208}...)
	go func() {
		got := make([]byte, len(want))
		_, err := io.ReadFull(server, got)
		if err == nil && !bytes.Equal(got, want) {
			err = errors.New("incorrect forwarded bytes")
		}
		if err == nil {
			_, err = server.Write([]byte("RFB server pixels"))
		}
		serverDone <- err
	}()
	client.read <- want
	if err := await(t, serverDone); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-client.written:
		if string(got) != "RFB server pixels" {
			t.Fatalf("bad server forwarding: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("server stream missing")
	}
	_ = client.Close()
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
	if counters.Forwarded.Load() != 0 || counters.ReadOnly.Load() != 4 || counters.Rejected.Load() != 0 {
		t.Fatalf("counters: %+v", counters)
	}
	if gate.View().Owner != "" || driver.calls.Load() < 2 {
		t.Fatal("disconnect did not release current actual input")
	}
}

// TestBridgeRejectsAgentRawRFBAndPreservesAgentLease verifies bridge rejects agent raw rfb and preserves agent lease.
func TestBridgeRejectsAgentRawRFBAndPreservesAgentLease(t *testing.T) {
	gate, credentials, _ := acquiredGate(t, control.Agent)
	client := newTestClient()
	upstream, server := net.Pipe()
	defer server.Close()
	counters := &Counters{}
	done := make(chan error, 1)
	go func() {
		done <- Bridge(context.Background(), client, upstream, gate, credentials, counters)
	}()
	got := make([]byte, 14)
	handshakeDone := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(server, got)
		handshakeDone <- err
	}()
	client.read <- []byte("RFB 003.008\n\x01\x01")
	if err := await(t, handshakeDone); err != nil {
		t.Fatal(err)
	}
	client.read <- []byte{5, 1, 0, 1, 0, 1}
	if err := await(t, done); err == nil {
		t.Fatal("agent raw input accepted")
	}
	one := make([]byte, 1)
	if n, _ := server.Read(one); n != 0 {
		t.Fatal("denied bytes reached upstream")
	}
	if counters.Rejected.Load() != 1 || gate.View().Owner != control.Agent {
		t.Fatal("agent lease changed on observer disconnect")
	}
}

// TestBridgeStaleViewerCannotWriteOrReleaseNewOwner verifies bridge stale viewer cannot write or release new owner.
func TestBridgeStaleViewerCannotWriteOrReleaseNewOwner(t *testing.T) {
	gate, old, _ := acquiredGate(t, control.Agent)
	newLease, err := gate.Acquire(context.Background(), control.Request{Owner: control.Human, Session: "new-viewer", Mode: control.Gated, Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient()
	upstream, server := net.Pipe()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		done <- Bridge(context.Background(), client, upstream, gate, old, nil)
	}()
	handshakeDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 14)
		_, err := io.ReadFull(server, buf)
		handshakeDone <- err
	}()
	client.read <- []byte("RFB 003.008\n\x01\x01")
	if err := await(t, handshakeDone); err != nil {
		t.Fatal(err)
	}
	client.read <- []byte{4, 1, 0, 0, 0, 0, 0, 65}
	if err := await(t, done); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("stale error: %v", err)
	}
	if view := gate.View(); view.Owner != control.Human || view.Session != newLease.Session {
		t.Fatal("stale viewer released replacement owner")
	}
}

// TestBridgeCancellationAndResetFailure verifies bridge cancellation and reset failure.
func TestBridgeCancellationAndResetFailure(t *testing.T) {
	gate, credentials, driver := acquiredGate(t, control.Human)
	driver.failure.Store(true)
	client := newTestClient()
	upstream, server := net.Pipe()
	defer server.Close()
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		done <- Bridge(ctx, client, upstream, gate, credentials, nil)
	}()
	cancel()
	if err := await(t, done); err == nil {
		t.Fatal("cleanup reset failure was hidden")
	}
	if gate.View().Ready {
		t.Fatal("failed cleanup left gate ready")
	}
}

// TestBridgeProtocolFailureNeverForwards verifies bridge protocol failure never forwards.
func TestBridgeProtocolFailureNeverForwards(t *testing.T) {
	gate, credentials, _ := acquiredGate(t, control.Human)
	client := newTestClient()
	upstream, server := net.Pipe()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		done <- Bridge(context.Background(), client, upstream, gate, credentials, nil)
	}()
	client.read <- []byte("RFB 003.003\n")
	if err := await(t, done); !errors.Is(err, ErrProtocol) {
		t.Fatalf("protocol error: %v", err)
	}
	buf := make([]byte, 1)
	if n, _ := server.Read(buf); n != 0 {
		t.Fatal("unsupported handshake forwarded")
	}
}

// TestWriteAllHandlesPartialAndZeroWrites verifies write all handles partial and zero writes.
func TestWriteAllHandlesPartialAndZeroWrites(t *testing.T) {
	var output bytes.Buffer
	writer := shortWriter{w: &output}
	if err := writeAll(writer, []byte("abcdef")); err != nil || output.String() != "abcdef" {
		t.Fatalf("partial writes: %q %v", output.String(), err)
	}
	if err := writeAll(zeroWriter{}, []byte{1}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
}

type shortWriter struct{ w io.Writer }

// Write forwards at most two bytes to the underlying writer.
func (s shortWriter) Write(d []byte) (int, error) {
	return s.w.Write(d[:min(len(d), 2)])
}

type zeroWriter struct{}

// Write simulates a writer that makes no progress.
func (zeroWriter) Write([]byte) (int, error) {
	return 0, nil
}

// TestBridgeCancellationInterruptsBlockedReadOnlyWrite verifies bridge cancellation interrupts blocked read only write.
func TestBridgeCancellationInterruptsBlockedReadOnlyWrite(t *testing.T) {
	gate, credentials, _ := acquiredGate(t, control.Human)
	client := newTestClient()
	upstream, server := net.Pipe()
	defer server.Close()
	observed := &observedWriteConn{Conn: upstream, entered: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Bridge(ctx, client, observed, gate, credentials, nil)
	}()
	handshakeDone := make(chan error, 1)
	go func() {
		buffer := make([]byte, 14)
		_, err := io.ReadFull(server, buffer)
		handshakeDone <- err
	}()
	client.read <- []byte("RFB 003.008\n\x01\x01")
	if err := await(t, handshakeDone); err != nil {
		t.Fatal(err)
	}
	// A framebuffer request is safe, but its write still must be cancelable.
	client.read <- []byte{3, 1, 0, 0, 0, 0, 5, 0, 2, 208}
	select {
	case <-observed.entered:
	case <-time.After(time.Second):
		t.Fatal("read-only write never began")
	}
	cancel()
	if err := await(t, done); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	if gate.View().Owner != "" {
		t.Fatal("canceled video connection retained its human lease")
	}
}

type observedWriteConn struct {
	net.Conn
	entered chan struct{}
}

// Write writes data from the supplied buffer.
func (c *observedWriteConn) Write(data []byte) (int, error) {
	if len(data) > 0 && data[0] == 3 {
		close(c.entered)
	}
	return c.Conn.Write(data)
}

type nativeResetDriver struct{ resetDriver }

// Give simulates transfer to a native viewer.
func (*nativeResetDriver) Give(context.Context, string) error {
	return nil
}

// TestBridgeNativeLeaseCannotBypassOrBeReleasedByRFB verifies bridge native lease cannot bypass or be released by rfb.
func TestBridgeNativeLeaseCannotBypassOrBeReleasedByRFB(t *testing.T) {
	driver := &nativeResetDriver{}
	gate := control.New(driver)
	defer gate.Close(context.Background())
	lease, err := gate.Acquire(context.Background(), control.Request{Owner: control.Human, Session: "native-viewer", Mode: control.Native, ViewerID: "native-id", Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient()
	upstream, server := net.Pipe()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		done <- Bridge(context.Background(), client, upstream, gate, control.Credentials{Session: lease.Session, Token: lease.Token}, nil)
	}()
	handshakeDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 14)
		_, err := io.ReadFull(server, buf)
		handshakeDone <- err
	}()
	client.read <- []byte("RFB 003.008\n\x01\x01")
	if err := await(t, handshakeDone); err != nil {
		t.Fatal(err)
	}
	client.read <- []byte{5, 1, 0, 1, 0, 1}
	if err := await(t, done); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("native raw bypass: %v", err)
	}
	if view := gate.View(); view.Owner != control.Human || view.Mode != control.Native {
		t.Fatal("RFB observer released unrelated native lease")
	}
}

// TestBridgeRejectsEveryRFBMutationEvenForCurrentHuman verifies bridge rejects every rfb mutation even for current human.
func TestBridgeRejectsEveryRFBMutationEvenForCurrentHuman(t *testing.T) {
	packets := map[string][]byte{
		"key":            {4, 1, 0, 0, 0, 0, 0, 65},
		"pointer":        {5, 1, 0, 10, 0, 20},
		"clipboard":      {6, 0, 0, 0, 0, 0, 0, 3, 'a', 'b', 'c'},
		"power":          {250, 0, 1, 2},
		"desktop-resize": append([]byte{251, 0, 5, 0, 2, 208, 1, 0}, make([]byte, 16)...),
		"extended-key":   {255, 0, 1, 0, 0, 0, 0, 65, 0, 0, 0, 38},
	}
	for name, packet := range packets {
		for _, fragmented := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "-coalesced", true: "-fragmented"}[fragmented], func(t *testing.T) {
				gate, credentials, driver := acquiredGate(t, control.Human)
				forbidden := &noRFBDispatch{Manager: gate}
				client := newTestClient()
				upstream, server := net.Pipe()
				defer server.Close()
				done := make(chan error, 1)
				captured := make(chan []byte, 1)
				counters := &Counters{}
				go func() {
					done <- Bridge(context.Background(), client, upstream, forbidden, credentials, counters)
				}()
				go func() {
					data, _ := io.ReadAll(server)
					captured <- data
				}()
				readOnly := append([]byte("RFB 003.008\n\x01\x01"), []byte{3, 1, 0, 0, 0, 0, 5, 0, 2, 208}...)
				if fragmented {
					client.read <- readOnly
					for _, b := range packet {
						client.read <- []byte{b}
					}
				} else {
					client.read <- append(bytes.Clone(readOnly), packet...)
				}
				if err := await(t, done); !errors.Is(err, ErrReadOnly) {
					t.Fatalf("mutating %s error: %v", name, err)
				}
				if got := <-captured; !bytes.Equal(got, readOnly) {
					t.Fatalf("mutating bytes reached private RFB server: %x", got)
				}
				if forbidden.calls.Load() != 0 || counters.Forwarded.Load() != 0 || counters.Rejected.Load() != 1 {
					t.Fatal("RFB mutation reached dispatch or was not rejected")
				}
				if gate.View().Owner != "" || driver.calls.Load() < 2 {
					t.Fatal("malicious human disconnect did not reset its own direct-input state")
				}
			})
		}
	}
}

type noRFBDispatch struct {
	*control.Manager
	calls atomic.Uint64
}

// DispatchHumanGated records a forbidden RFB dispatch attempt.
func (g *noRFBDispatch) DispatchHumanGated(context.Context, control.Credentials, func(context.Context) error) error {
	g.calls.Add(1)
	return errors.New("rfb dispatch must never be called")
}
