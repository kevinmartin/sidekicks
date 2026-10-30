package main

import (
	"context"
	"net"
	"net/http"
	"time"
)

func startSite(ctx context.Context, address string, h http.Handler) (func(), error) {
	l, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	c, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = serve(c, l, h)
	}()
	return func() {
		cancel()
		timeout := time.NewTimer(6 * time.Second)
		defer timeout.Stop()
		select {
		case <-done:
		case <-timeout.C:
			_ = l.Close()
		}
	}, nil
}
