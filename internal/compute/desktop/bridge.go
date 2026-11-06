package desktop

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute/control"
)

// HumanGate owns direct-input dispatch and lifecycle cleanup. RFB Bridge uses
// only validation/release for disconnect cleanup; it never dispatches input.
// Direct X11 callers must still serialize writes with takeover and expiry.
type HumanGate interface {
	DispatchHumanGated(context.Context, control.Credentials, func(context.Context) error) error
	Validate(control.Credentials) (control.Lease, error)
	Release(context.Context, control.Credentials) error
}

// Client is a bounded, message-oriented, binary-only viewer connection.
// Close must unblock Read and Write. Only one caller reads and one writes.
type Client interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
	Close() error
}

// ErrReadOnly rejects every RFB input route, including a currently owned human.
var ErrReadOnly = errors.New("rfb transport is read-only; use lease-gated x11 input")

// Counters tracks desktop bridge activity.
type Counters struct {
	Forwarded atomic.Uint64
	Rejected  atomic.Uint64
	ReadOnly  atomic.Uint64
}

// Bridge relays a single authenticated viewer to a private RFB connection.
// Every mutating RFB message is rejected before any upstream write, even with
// a valid human lease. Asynchronous RFB server queues cannot provide the same
// input-consumption fence as direct synchronized X11 requests.
// A disconnect relinquishes only this viewer's still-current human lease.
// Credentials must identify a distinct viewer session, never a shared token.
func Bridge(ctx context.Context, client Client, upstream net.Conn, gate HumanGate, credentials control.Credentials, counters *Counters) (result error) {
	if client == nil || upstream == nil || gate == nil {
		return errors.New("desktop bridge is not configured")
	}
	if counters == nil {
		counters = &Counters{}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer client.Close()
	defer upstream.Close()
	stop := context.AfterFunc(ctx, func() {
		_ = client.Close()
		_ = upstream.Close()
	})
	defer stop()
	defer func() {
		// Cleanup is bounded independently of the disconnected request. A stale
		// credential cannot release a new owner's lease.
		if lease, err := gate.Validate(credentials); err == nil && lease.Owner == control.Human && lease.Mode == control.Gated {
			cleanup, done := context.WithTimeout(context.Background(), 2*time.Second)
			defer done()
			result = errors.Join(result, gate.Release(cleanup, credentials))
		}
	}()
	serverErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := upstream.Read(buf)
			if n > 0 {
				if writeErr := client.Write(ctx, buf[:n]); writeErr != nil {
					serverErr <- writeErr
					cancel()
					return
				}
			}
			if err != nil {
				serverErr <- err
				cancel()
				return
			}
		}
	}()
	parser := ClientFrames{}
	for {
		data, err := client.Read(ctx)
		if err != nil {
			result = err
			break
		}
		frames, err := parser.Feed(data)
		if err != nil {
			result = err
			break
		}
		for _, frame := range frames {
			if frame.Mutating {
				counters.Rejected.Add(1)
				result = ErrReadOnly
				break
			}
			write := func(writeCtx context.Context) error {
				if err := writeCtx.Err(); err != nil {
					return err
				}
				deadline := time.Now().Add(5 * time.Second)
				if d, ok := writeCtx.Deadline(); ok && d.Before(deadline) {
					deadline = d
				}
				stopWrite := context.AfterFunc(writeCtx, func() {
					_ = upstream.Close()
				})
				defer stopWrite()
				if err := upstream.SetWriteDeadline(deadline); err != nil {
					return err
				}
				return writeAll(upstream, frame.Data)
			}
			if err := write(ctx); err != nil {
				result = err
				break
			}
			counters.ReadOnly.Add(1)
		}
		if result != nil {
			break
		}
	}
	cancel()
	_ = client.Close()
	_ = upstream.Close()
	// Wait for the only spawned goroutine. Both transports are closed above.
	<-serverErr
	if errors.Is(result, io.EOF) || errors.Is(result, context.Canceled) || errors.Is(result, net.ErrClosed) {
		return nil
	}
	return result
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if n < 0 || n > len(data) {
			return io.ErrShortWrite
		}
		data = data[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
