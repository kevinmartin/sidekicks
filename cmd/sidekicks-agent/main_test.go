package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements the test fixture's RoundTrip method.
func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type failedWriter struct{}

// Write implements the test fixture's Write method.
func (failedWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

// TestOneShotRequest verifies one shot request.
func TestOneShotRequest(t *testing.T) {
	token := strings.Repeat("x", 64)
	input := base64.StdEncoding.EncodeToString([]byte(`{"method":"GET","path":"/health"}`))
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "http://127.0.0.1:8080/health" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Fatal("unsafe target/auth")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ready":true}`))}, nil
	})}
	var out bytes.Buffer
	if err := runRequest([]string{"request", input}, token, client, &out); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Status int
		Body   []byte
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || envelope.Status != 200 || string(envelope.Body) != `{"ready":true}` {
		t.Fatal(out.String(), err)
	}
	for _, args := range [][]string{nil, {"request"}, {"wrong", input}, {"request", "!"}, {"request", input, "extra"}} {
		if err := runRequest(args, token, client, io.Discard); err == nil {
			t.Fatal("accepted bad args")
		}
	}
	if err := runRequest([]string{"request", input}, "short", client, io.Discard); err == nil {
		t.Fatal("accepted bad token")
	}
	if err := runRequest([]string{"request", input}, token, client, failedWriter{}); err == nil {
		t.Fatal("ignored output failure")
	}
}

// TestServerGracefulShutdown verifies server graceful shutdown.
func TestServerGracefulShutdown(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	listener := &pipeListener{connections: make(chan net.Conn, 1), done: make(chan struct{})}
	listener.connections <- serverConn
	var err error
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	requestCanceled := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(requestCanceled)
	})
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, listener, handler)
	}()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client := &http.Client{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
			return clientConn, nil
		}}}
		response, err := client.Get("http://127.0.0.1/health")
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler not reached")
	}
	cancel()
	select {
	case <-requestCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("active request did not receive cancellation")
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not drain")
	}
	<-clientDone
}

type failedListener struct{}

// Accept implements the test fixture's Accept method.
func (failedListener) Accept() (net.Conn, error) {
	return nil, errors.New("accept failed")
}

// Close implements the test fixture's Close method.
func (failedListener) Close() error {
	return nil
}

// Addr implements the test fixture's Addr method.
func (failedListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}
}

// TestServeFailureDoesNotLeakShutdownWorker verifies serve failure does not leak shutdown worker.
func TestServeFailureDoesNotLeakShutdownWorker(t *testing.T) {
	if err := serve(context.Background(), failedListener{}, http.NotFoundHandler()); err == nil {
		t.Fatal("listener failure hidden")
	}
}

type pipeListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

// Accept implements the test fixture's Accept method.
func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

// Close implements the test fixture's Close method.
func (l *pipeListener) Close() error {
	l.once.Do(func() {
		close(l.done)
	})
	return nil
}

// Addr implements the test fixture's Addr method.
func (l *pipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}
}
