package browsergateway

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
)

// stagehandTransport owns the SDK's connection to Chromium. Closing the
// Stagehand SDK's Browser would close Chromium itself.
type stagehandTransport struct {
	endpoint string
	cancel   context.CancelFunc
	done     chan struct{}
}

func newStagehandTransport(ctx context.Context, endpoint string) (*stagehandTransport, error) {
	target, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse Chromium endpoint: %w", err)
	}
	upstream, err := (&net.Dialer{}).DialContext(ctx, "tcp", target.Host)
	if err != nil {
		return nil, fmt.Errorf("dial Chromium CDP: %w", err)
	}
	var config net.ListenConfig
	listener, err := config.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		_ = upstream.Close()
		return nil, fmt.Errorf("listen for Stagehand SDK: %w", err)
	}
	target.Host = listener.Addr().String()
	lifetime, cancel := context.WithCancel(context.WithoutCancel(ctx))
	transport := &stagehandTransport{
		endpoint: target.String(),
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	go transport.forward(lifetime, listener, upstream)
	return transport, nil
}

func (t *stagehandTransport) close() {
	t.cancel()
	<-t.done
}

func (t *stagehandTransport) forward(ctx context.Context, listener net.Listener, upstream net.Conn) {
	defer close(t.done)
	defer t.cancel()
	defer func() {
		_ = listener.Close()
		_ = upstream.Close()
	}()
	stop := context.AfterFunc(ctx, func() {
		_ = listener.Close()
		_ = upstream.Close()
	})
	defer stop()

	client, err := listener.Accept()
	if err != nil {
		return
	}
	// Each transport serves a single SDK dial on guest loopback.
	_ = listener.Close()
	stopClient := context.AfterFunc(ctx, func() {
		_ = client.Close()
	})
	defer stopClient()

	copied := make(chan struct{})
	go func() {
		defer close(copied)
		_, _ = io.Copy(upstream, client)
		_ = upstream.Close()
	}()
	// The SDK observes read and write failures when its connection closes.
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
	<-copied
}
