package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute/browser"
	assets "github.com/kevinmartin/sidekicks/web"
)

// prepareBrowser never attaches to a user's host browser. The endpoint and child
// process are fixed inside the disposable Linux guest. Chromium's own sandbox is
// enabled unless an explicit compatibility opt-in is supplied by the operator.
type browserDependencies struct {
	startSite  func(context.Context, string, http.Handler) (func(), <-chan struct{}, error)
	command    func(context.Context, string, ...string) *exec.Cmd
	httpClient *http.Client
}

func prepareBrowser(ctx context.Context, noSandbox bool) (http.Handler, func(), func() error, error) {
	return prepareBrowserWith(ctx, noSandbox, browserDependencies{startSite: startSite, command: exec.CommandContext, httpClient: &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}}})
}
func prepareBrowserWith(ctx context.Context, noSandbox bool, deps browserDependencies) (http.Handler, func(), func() error, error) {
	site := http.NewServeMux()
	fixture := assets.NewFixtureSite()
	site.Handle("/fixture", fixture)
	site.Handle("/fixture/state", fixture)
	stopSite, siteDone, err := deps.startSite(ctx, "127.0.0.1:8081", site)
	if err != nil {
		return nil, nil, nil, err
	}
	retainSite := false
	defer func() {
		if !retainSite {
			stopSite()
		}
	}()
	args := []string{"--headless=new", "--no-first-run", "--no-default-browser-check", "--disable-dev-shm-usage", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=9222", "--user-data-dir=/tmp/sidekicks-headless-profile", "about:blank"}
	if noSandbox {
		args = append([]string{"--no-sandbox"}, args...)
	}
	cmd := deps.command(ctx, "/usr/bin/chromium", args...)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/workspace", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var diagnostic limitedBuffer
	cmd.Stderr = &diagnostic
	cmd.Stdout = &diagnostic
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, errors.New("guest chromium executable could not start")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cmd.Wait()
	}()
	cleanup := func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	client, err := browser.New("http://127.0.0.1:9222")
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	closeAll := func() {
		stopSite()
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = client.Close(c)
		cleanup()
	}
	readyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	clientCopy := *deps.httpClient
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	httpClient := &clientCopy
	for {
		req, _ := http.NewRequestWithContext(readyCtx, http.MethodGet, "http://127.0.0.1:9222/json/version", nil)
		res, e := httpClient.Do(req)
		if e == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				retainSite = true
				health := func() error {
					select {
					case <-siteDone:
						return errors.New("guest fixture site stopped")
					case <-done:
						return errors.New("guest chromium stopped")
					default:
					}
					c, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
					defer cancel()
					req, _ := http.NewRequestWithContext(c, http.MethodGet, "http://127.0.0.1:9222/json/version", nil)
					res, e := httpClient.Do(req)
					if e != nil {
						return errors.New("guest chromium control unavailable")
					}
					res.Body.Close()
					if res.StatusCode != 200 {
						return errors.New("guest chromium control unavailable")
					}
					return nil
				}
				return browser.NewHandler(client), closeAll, health, nil
			}
		}
		select {
		case <-siteDone:
			closeAll()
			return nil, nil, nil, errors.New("guest fixture site stopped before readiness")
		case <-done:
			closeAll()
			return nil, nil, nil, fmt.Errorf("chromium stopped before readiness: %s", diagnostic.String())
		case <-readyCtx.Done():
			closeAll()
			return nil, nil, nil, errors.New("guest chromium readiness timed out")
		case <-tick.C:
		}
	}
}

// Only initialization diagnostics are captured; guest commands have separate,
// synchronized bounded capture. The child is the sole writer until readiness.
type limitedBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

// Write appends diagnostics up to the configured byte limit.
func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	room := 8192 - b.data.Len()
	if len(p) > room {
		p = p[:room]
	}
	_, _ = b.data.Write(p)
	return n, nil
}

// String returns a synchronized snapshot of the captured diagnostics.
func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}
