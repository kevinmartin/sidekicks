package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func browserUnitDeps(t *testing.T, script string) (browserDependencies, *atomic.Int32, *atomic.Int32, **exec.Cmd, *[]string) {
	t.Helper()
	var mode, stops atomic.Int32
	mode.Store(200)
	var command *exec.Cmd
	var args []string
	deps := browserDependencies{
		startSite: func(_ context.Context, address string, handler http.Handler) (func(), error) {
			if address != "127.0.0.1:8081" {
				t.Fatal("fixture bound outside private guest address")
			}
			request := httptest.NewRequest("GET", "http://127.0.0.1:8081/fixture", nil)
			request.RemoteAddr = "127.0.0.1:1234"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != 200 {
				t.Fatal("fixture was not mounted", response.Code)
			}
			return func() {
				stops.Add(1)
			}, nil
		},
		command: func(ctx context.Context, path string, argv ...string) *exec.Cmd {
			if path != "/usr/bin/chromium" {
				t.Fatal("host browser path selected")
			}
			args = append([]string(nil), argv...)
			command = exec.CommandContext(ctx, "/bin/sh", "-c", script)
			return command
		},
		httpClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "http://127.0.0.1:9222/json/version" {
				t.Fatal("non-private CDP discovery")
			}
			status := mode.Load()
			if status == 0 {
				return nil, errors.New("control endpoint unavailable")
			}
			return &http.Response{StatusCode: int(status), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		})},
	}
	return deps, &mode, &stops, &command, &args
}

// TestBrowserBootstrapHealthAndCleanup verifies browser bootstrap health and cleanup.
func TestBrowserBootstrapHealthAndCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps, mode, stops, command, args := browserUnitDeps(t, "sleep 30")
	handler, cleanup, health, err := prepareBrowserWith(ctx, false, deps)
	if err != nil || handler == nil || health == nil || cleanup == nil {
		t.Fatal("bootstrap failed", err)
	}
	defer cleanup()
	if strings.Contains(strings.Join(*args, " "), "--no-sandbox") || !strings.Contains(strings.Join(*args, " "), "--remote-debugging-address=127.0.0.1") {
		t.Fatal("sandbox or debugger address weakened")
	}
	for _, entry := range (*command).Env {
		if strings.Contains(entry, "SIDEKICKS_AGENT_TOKEN") || strings.Contains(entry, "HOME=/Users/") {
			t.Fatal("host or credential environment leaked")
		}
	}
	if err = health(); err != nil {
		t.Fatal(err)
	}
	mode.Store(503)
	if err = health(); err == nil {
		t.Fatal("unavailable CDP passed health")
	}
	mode.Store(0)
	if err = health(); err == nil {
		t.Fatal("failed CDP passed health")
	}
	mode.Store(200)
	cleanup()
	deadline := time.Now().Add(time.Second)
	for health() == nil {
		if time.Now().After(deadline) {
			t.Fatal("child exit did not revoke health")
		}
		time.Sleep(time.Millisecond)
	}
	if stops.Load() == 0 {
		t.Fatal("fixture server leaked")
	}
}

// TestBrowserCompatibilityFlagIsExplicit verifies browser compatibility flag is explicit.
func TestBrowserCompatibilityFlagIsExplicit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps, _, _, _, args := browserUnitDeps(t, "sleep 30")
	_, cleanup, _, err := prepareBrowserWith(ctx, true, deps)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(*args) == 0 || (*args)[0] != "--no-sandbox" {
		t.Fatal("explicit compatibility option was ignored")
	}
}

// TestBrowserBootstrapFailuresCloseOnlyOwnedSiteAndChild verifies browser bootstrap failures close only owned site and child.
func TestBrowserBootstrapFailuresCloseOnlyOwnedSiteAndChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps, _, stops, _, _ := browserUnitDeps(t, "sleep 30")
	deps.startSite = func(context.Context, string, http.Handler) (func(), error) {
		return nil, errors.New("fixture listener unavailable")
	}
	if _, _, _, err := prepareBrowserWith(ctx, false, deps); err == nil || stops.Load() != 0 {
		t.Fatal("failed site boot accepted")
	}
	deps, _, stops, _, _ = browserUnitDeps(t, "sleep 30")
	deps.command = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/missing-sidekicks-browser")
	}
	if _, _, _, err := prepareBrowserWith(ctx, false, deps); err == nil || stops.Load() == 0 {
		t.Fatal("child start failure leaked site")
	}
	deps, mode, stops, _, _ := browserUnitDeps(t, "printf 'bounded startup failure' >&2; exit 3")
	mode.Store(503)
	if _, _, _, err := prepareBrowserWith(ctx, false, deps); err == nil || !strings.Contains(err.Error(), "stopped before readiness") || stops.Load() == 0 {
		t.Fatal("child early exit not observed", err)
	}
	deps, mode, stops, _, _ = browserUnitDeps(t, "sleep 30")
	mode.Store(503)
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	if _, _, _, err := prepareBrowserWith(short, false, deps); err == nil || stops.Load() == 0 {
		t.Fatal("startup cancellation leaked resources")
	}
}

// TestBoundedBrowserDiagnosticsConcurrentReaders verifies bounded browser diagnostics concurrent readers.
func TestBoundedBrowserDiagnosticsConcurrentReaders(t *testing.T) {
	var buffer limitedBuffer
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 10 {
				_, _ = buffer.Write(bytes.Repeat([]byte("x"), 2048))
				_ = buffer.String()
			}
		}()
	}
	workers.Wait()
	if len(buffer.String()) != 8192 {
		t.Fatal("diagnostic capture exceeded its fixed budget")
	}
}

// TestChildStartupFailureDoneAndCleanup verifies child startup failure done and cleanup.
func TestChildStartupFailureDoneAndCleanup(t *testing.T) {
	if _, _, err := startChild(context.Background(), nil, "/missing-sidekicks-child", "private-argument"); err == nil || strings.Contains(err.Error(), "private-argument") {
		t.Fatal("child failure was accepted or leaked argv", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, done, err := startChild(ctx, []string{"PATH=/usr/bin:/bin"}, "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("owned child not reaped")
	}
	already, cancel := context.WithCancel(context.Background())
	cancel()
	if err = waitTCP(already, "127.0.0.1:0"); err == nil {
		t.Fatal("canceled readiness accepted")
	}
}

// TestMainRejectsUnsupportedEntrypointsWithoutRuntimeMutation verifies main rejects unsupported entrypoints without runtime mutation.
func TestMainRejectsUnsupportedEntrypointsWithoutRuntimeMutation(t *testing.T) {
	original := os.Args
	defer func() {
		os.Args = original
	}()
	for _, args := range [][]string{{"sidekicks-agent", "launch", "sh"}, {"sidekicks-agent", "request", "!"}, {"sidekicks-agent", "unknown"}} {
		os.Args = args
		if err := runMain(); err == nil {
			t.Fatal("invalid entrypoint accepted")
		}
	}
	if runtime.GOOS != "linux" {
		os.Args = []string{"sidekicks-agent"}
		if err := runMain(); err == nil || !strings.Contains(err.Error(), "requires linux") {
			t.Fatal("host process claimed guest support", err)
		}
	}
}
