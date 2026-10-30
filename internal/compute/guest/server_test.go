package guest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(Config{Workspace: dir, Token: testToken, MaxConcurrentExec: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s, dir
}
func call(s *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}
func run(t *testing.T, s *Server, req ExecRequest) ExecResult {
	t.Helper()
	body, _ := json.Marshal(req)
	w := call(s, http.MethodPost, "/exec", body)
	if w.Code != 200 {
		t.Fatalf("exec status %d: %s", w.Code, w.Body)
	}
	var result ExecResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// TestConfigurationAndAuth verifies guest configuration and authentication.
func TestConfigurationAndAuth(t *testing.T) {
	for _, cfg := range []Config{{Token: "short", Workspace: t.TempDir()}, {Token: strings.Repeat("x", 257), Workspace: t.TempDir()}, {Token: testToken + "\n", Workspace: t.TempDir()}, {Token: testToken, Workspace: t.TempDir(), MaxConcurrentExec: 17}, {Token: testToken, Workspace: "/missing-sidekicks-workspace"}} {
		if s, err := New(cfg); err == nil {
			_ = s.Close()
			t.Fatal("accepted invalid configuration")
		}
	}
	s, _ := newServer(t)
	for _, route := range []string{"/health", "/exec", "/files?path=a", "/unknown"} {
		req := httptest.NewRequest("GET", route, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", route, w.Code)
		}
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Origin", "http://127.0.0.1")
		w = httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatalf("origin %s: %d", route, w.Code)
		}
	}
	for _, tt := range []struct {
		method, path string
		status       int
	}{{"GET", "/health", 200}, {"POST", "/health", 405}, {"GET", "/exec", 405}, {"POST", "/files?path=a", 405}, {"GET", "/unknown", 404}} {
		w := call(s, tt.method, tt.path, nil)
		if w.Code != tt.status {
			t.Fatalf("%v: %d", tt, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing cache policy")
		}
	}
}

// TestExecWorkingDirectoryEnvironmentAndExit verifies command working directory, environment and exit status.
func TestExecWorkingDirectoryEnvironmentAndExit(t *testing.T) {
	s, dir := newServer(t)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	result := run(t, s, ExecRequest{Argv: []string{"sh", "-c", "printf 'SideKicks ✓!'; printf error >&2; pwd; printf '%s' \"$TZ\"; exit 7"}, Cwd: "sub", Env: map[string]string{"TZ": "UTC"}})
	if result.ExitCode != 7 || !strings.HasPrefix(result.Stdout, "SideKicks ✓!") || !strings.Contains(result.Stdout, "/sub") || !strings.HasSuffix(result.Stdout, "UTC") || result.Stderr != "error" || result.TimedOut {
		t.Fatalf("unexpected result %+v", result)
	}
	if w := call(s, "POST", "/exec", []byte(`{"argv":["definitely-not-a-guest-program"]}`)); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := call(s, "POST", "/exec", []byte(`{"argv":["/not-existing-sidekicks-binary"]}`)); w.Code != 422 {
		t.Fatal(w.Code)
	}
}

// TestExecOutputBoundedWhileReading verifies the limit on command output while it is read.
func TestExecOutputBoundedWhileReading(t *testing.T) {
	s, _ := newServer(t)
	result := run(t, s, ExecRequest{Argv: []string{"sh", "-c", "head -c 400000 /dev/zero; head -c 400000 /dev/zero >&2"}})
	if len(result.Stdout) != MaxOutputBytes || len(result.Stderr) != MaxOutputBytes || !result.StdoutTruncated || !result.StderrTruncated || result.ExitCode != 0 {
		t.Fatalf("output was not bounded: %d %d %+v", len(result.Stdout), len(result.Stderr), result)
	}
	var b boundedOutput
	const workers = 8
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = b.Write(bytes.Repeat([]byte("x"), MaxOutputBytes))
		}()
	}
	wg.Wait()
	out, truncated := b.result()
	if len(out) != MaxOutputBytes || !truncated {
		t.Fatal("concurrent output exceeded limit")
	}
}

// TestExecValidation verifies command validation.
func TestExecValidation(t *testing.T) {
	s, _ := newServer(t)
	cases := []string{`{}`, `{"argv":[]}`, `{"argv":[""]}`, `{"argv":["sh"],"timeoutMs":99}`, `{"argv":["sh"],"timeoutMs":60001}`, `{"argv":["sh"],"env":{"LD_PRELOAD":"bad"}}`, `{"argv":["sh"],"env":{"TZ":"bad\n"}}`, `{"argv":["sh"],"cwd":"../"}`, `{"argv":["./sh"]}`, `{"argv":["sh"],"unknown":true}`, `{} {}`, `broken`}
	for _, body := range cases {
		w := call(s, "POST", "/exec", []byte(body))
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
	w := call(s, "POST", "/exec", nil)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("POST", "/exec", strings.NewReader(`{"argv":["true"]}`))
	r.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal(w.Code)
	}
	for _, req := range []ExecRequest{{Argv: []string{"sh", strings.Repeat("x", 65537)}}, {Argv: []string{"sh", "\x00"}}, {Argv: []string{"sh"}, TimeoutMS: -1}} {
		if validateExec(req) == nil {
			t.Fatal("bad exec accepted")
		}
	}
}

// TestExecTimeoutKillsProcessGroup verifies that a command timeout kills its process group.
func TestExecTimeoutKillsProcessGroup(t *testing.T) {
	s, dir := newServer(t)
	result := run(t, s, ExecRequest{Argv: []string{"sh", "-c", "sleep 30 & echo $! > child.pid; wait"}, TimeoutMS: 100})
	if !result.TimedOut || result.ExitCode == 0 {
		t.Fatalf("timeout was not represented: %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(dir, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	assertNotRunning(t, pid)
}

// TestRequestCancellationAndConcurrency verifies request cancellation and concurrent execution.
func TestRequestCancellationAndConcurrency(t *testing.T) {
	s, dir := newServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest("POST", "/exec", strings.NewReader(`{"argv":["sh","-c","echo $$ > own.pid; sleep 30"],"timeoutMs":30000}`)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	done := make(chan struct{})
	go func() {
		s.ServeHTTP(httptest.NewRecorder(), request)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "own.pid")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("process did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if w := call(s, "POST", "/exec", []byte(`{"argv":["true"]}`)); w.Code != 429 {
		t.Fatal(w.Code)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not return")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "own.pid"))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	assertNotRunning(t, pid)
	if result := run(t, s, ExecRequest{Argv: []string{"true"}}); result.ExitCode != 0 {
		t.Fatal(result)
	}
}
func assertNotRunning(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("owned process %d still exists after cleanup", pid)
}

// TestFilesRoundTripAtomicAndLimits verifies file round trips, atomic writes and size limits.
func TestFilesRoundTripAtomicAndLimits(t *testing.T) {
	s, dir := newServer(t)
	body := []byte("CaseSensitive ✓\x00\xff")
	if w := call(s, "PUT", "/files?path=Case.txt", body); w.Code != 200 {
		t.Fatalf("write: %d %s", w.Code, w.Body)
	}
	if w := call(s, "GET", "/files?path=Case.txt", nil); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), body) {
		t.Fatalf("read: %d %q", w.Code, w.Body.Bytes())
	}
	st, err := os.Stat(filepath.Join(dir, "Case.txt"))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("mode: %v %v", st, err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if w := call(s, "PUT", "/files?path=nested/a", nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := call(s, "PUT", "/files?path=Case.txt", bytes.Repeat([]byte("x"), MaxFileBytes+1)); w.Code != 413 {
		t.Fatal(w.Code)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "Case.txt")); !bytes.Equal(data, body) {
		t.Fatal("failed write destroyed previous file")
	}
	if err := os.WriteFile(filepath.Join(dir, "big"), make([]byte, MaxFileBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if w := call(s, "GET", "/files?path=big", nil); w.Code != 413 {
		t.Fatal(w.Code)
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".sidekicks-write-") {
			t.Fatal("temporary write leaked")
		}
	}
}

// TestFileTraversalSymlinksAndTypesRejected verifies that file traversal, symlinks and unsupported types are rejected.
func TestFileTraversalSymlinksAndTypesRejected(t *testing.T) {
	s, dir := newServer(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"escape": outside, "inside": "regular", "final": filepath.Join(outside, "secret")} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "regular"), []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../secret", "/etc/passwd", "a/../regular", "escape/secret", "inside", "final", ".", "a\\b", "a//b", "a/./b", ""} {
		for _, method := range []string{"GET", "PUT"} {
			w := call(s, method, "/files?path="+url.QueryEscape(p), []byte("replacement"))
			if w.Code == 200 {
				t.Fatalf("accepted %s %q", method, p)
			}
		}
	}
	for _, query := range []string{"", "?path=regular&path=regular", "?path=regular&extra=x"} {
		if w := call(s, "GET", "/files"+query, nil); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	if w := call(s, "GET", "/files?path=missing", nil); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	if w := call(s, "GET", "/files?path=folder", nil); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if data, _ := os.ReadFile(filepath.Join(outside, "secret")); string(data) != "secret" {
		t.Fatal("escaped workspace")
	}
	if w := call(s, "POST", "/exec", []byte(`{"argv":["pwd"],"cwd":"escape"}`)); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip calls the configured test transport function.
func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// TestLocalRequestTransport verifies the local request transport.
func TestLocalRequestTransport(t *testing.T) {
	s, _ := newServer(t)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "127.0.0.1:8080" || req.Header.Get("Origin") != "" {
			t.Fatal("unexpected target")
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w.Result(), nil
	})}
	raw, _ := json.Marshal(LocalRequest{Method: "GET", Path: "/health"})
	req, err := DecodeLocalRequest(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	result, err := CallLocal(context.Background(), client, testToken, req)
	if err != nil || result.Status != 200 || !bytes.Contains(result.Body, []byte(`"ready":true`)) {
		t.Fatalf("%+v %v", result, err)
	}
	for _, bad := range []string{"!", base64.StdEncoding.EncodeToString([]byte(`{"method":"GET","path":"http://evil/health"}`)), base64.StdEncoding.EncodeToString([]byte(`{"method":"GET","path":"/private"}`)), base64.StdEncoding.EncodeToString([]byte(`{} {}`)), strings.Repeat("x", MaxTransportRequestBytes*2)} {
		if _, err := DecodeLocalRequest(bad); err == nil {
			t.Fatalf("accepted bad transport request %q", bad[:min(len(bad), 60)])
		}
	}
	for _, request := range []LocalRequest{{Method: "DELETE", Path: "/files?path=a"}, {Method: "GET", Path: "/health?x=1"}, {Method: "POST", Path: "/exec", Body: make([]byte, MaxTransportRequestBytes+1)}} {
		if _, err := CallLocal(context.Background(), client, testToken, request); err == nil {
			t.Fatal("accepted unsupported request")
		}
	}
	if _, err = CallLocal(context.Background(), client, "short", req); err == nil {
		t.Fatal("accepted short token")
	}
	badClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("contains-secret-%s", testToken)
	})}
	if _, err = CallLocal(context.Background(), badClient, testToken, req); err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatal("transport leaked private error")
	}
	hugeClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", MaxTransportResponseBytes+1)))}, nil
	})}
	if _, err = CallLocal(context.Background(), hugeClient, testToken, req); err == nil {
		t.Fatal("unbounded response")
	}
}

// TestExtraRoutesRemainAuthenticatedAndFixed verifies that extra routes remain authenticated and fixed.
func TestExtraRoutesRemainAuthenticatedAndFixed(t *testing.T) {
	for _, route := range []string{"/health", "/exec", "/files", "/private", ""} {
		if s, err := New(Config{Workspace: t.TempDir(), Token: testToken, ExtraRoutes: map[string]http.Handler{route: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {

		})}}); err == nil {
			_ = s.Close()
			t.Fatal("accepted reserved route")
		}
	}
	if s, err := New(Config{Workspace: t.TempDir(), Token: testToken, ExtraRoutes: map[string]http.Handler{"/browser": nil}}); err == nil {
		_ = s.Close()
		t.Fatal("accepted nil route")
	}
	calls := 0
	extra := map[string]http.Handler{"/browser": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(202)
	})}
	s, err := New(Config{Workspace: t.TempDir(), Token: testToken, ExtraRoutes: extra})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	delete(extra, "/browser")
	if w := call(s, "POST", "/browser", []byte(`{}`)); w.Code != 202 || calls != 1 {
		t.Fatal(w.Code, calls)
	}
	req := httptest.NewRequest("POST", "/browser", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 401 || calls != 1 {
		t.Fatal(w.Code, calls)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Origin", "http://evil.test")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 403 || calls != 1 {
		t.Fatal(w.Code, calls)
	}
	if w := call(s, "GET", "/browser", nil); w.Code != 405 || calls != 1 {
		t.Fatal(w.Code, calls)
	}
	bare, _ := newServer(t)
	if w := call(bare, "POST", "/browser", nil); w.Code != 404 {
		t.Fatal(w.Code)
	}
	raw, _ := json.Marshal(LocalRequest{Method: "POST", Path: "/browser", Body: []byte(`{}`)})
	if _, err := DecodeLocalRequest(base64.StdEncoding.EncodeToString(raw)); err != nil {
		t.Fatal(err)
	}
}

// TestDurableWriteAnchoredThroughParentRename verifies that durable writes remain anchored through a parent rename.
func TestDurableWriteAnchoredThroughParentRename(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "parent")
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	parent, err := os.OpenRoot(original)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	moved := filepath.Join(dir, "moved")
	// This is the point of concurrent guest interference: the opened parent
	// remains valid, while its old path now names an unrelated directory.
	done := make(chan error, 1)
	go func() {
		if err := os.Rename(original, moved); err != nil {
			done <- err
			return
		}
		done <- os.Mkdir(original, 0700)
	}()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = writeDurable(parent, "proof", []byte("anchored")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(moved, "proof")); err != nil || string(data) != "anchored" {
		t.Fatalf("write escaped opened parent: %q %v", data, err)
	}
	if _, err = os.Stat(filepath.Join(original, "proof")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("replacement parent received write")
	}
	entries, _ := os.ReadDir(moved)
	if len(entries) != 1 {
		t.Fatal("temporary file leaked")
	}
}

// TestNamedPipeReadIsRejectedWithoutBlocking verifies that a named pipe is rejected without blocking.
func TestNamedPipeReadIsRejectedWithoutBlocking(t *testing.T) {
	s, dir := newServer(t)
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		done <- call(s, "GET", "/files?path=fifo", nil).Code
	}()
	select {
	case status := <-done:
		if status != 400 {
			t.Fatal(status)
		}
	case <-time.After(time.Second):
		t.Fatal("file API blocked opening a FIFO")
	}
}

// TestCurrentWorkloadHealthIsPrivateAndDynamic verifies that workload health is private and dynamic.
func TestCurrentWorkloadHealthIsPrivateAndDynamic(t *testing.T) {
	alive := true
	calls := 0
	s, err := New(Config{Workspace: t.TempDir(), Token: testToken, Health: func() error {
		calls++
		if !alive {
			return errors.New("secret backend path /private/config and token")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	request := httptest.NewRequest("GET", "/health", nil)
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 401 || calls != 0 {
		t.Fatal("health probed before authentication")
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Origin", "http://evil.test")
	response = httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 403 || calls != 0 {
		t.Fatal("origin reached health callback")
	}
	if response := call(s, "GET", "/health", nil); response.Code != 200 || !strings.Contains(response.Body.String(), `"ready":true`) {
		t.Fatal(response.Code, response.Body)
	}
	alive = false
	if response := call(s, "GET", "/health", nil); response.Code != 503 || !strings.Contains(response.Body.String(), `"ready":false`) || strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "private") {
		t.Fatal(response.Code, response.Body)
	}
	alive = true
	if response := call(s, "GET", "/health", nil); response.Code != 200 {
		t.Fatal("current readiness did not recover", response.Code)
	}
}

// TestFixedDesktopRoutesAuthMethodsAndNoPrivateProxy verifies desktop route authentication and methods without a private proxy.
func TestFixedDesktopRoutesAuthMethodsAndNoPrivateProxy(t *testing.T) {
	accepted := map[string][]string{"/desktop/connect": {"POST"}, "/desktop/state": {"GET"}, "/desktop/launch": {"POST"}, "/desktop/control": {"GET", "POST"}, "/desktop/input": {"POST"}, "/desktop/capture": {"GET"}}
	count := 0
	routes := map[string]http.Handler{}
	for route := range accepted {
		routes[route] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			if _, err := io.ReadAll(r.Body); err != nil {
				w.WriteHeader(413)
				return
			}
			w.WriteHeader(202)
		})
	}
	s, err := New(Config{Workspace: t.TempDir(), Token: testToken, ExtraRoutes: routes})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	delete(routes, "/desktop/state")
	for route, methods := range accepted {
		for _, method := range methods {
			before := count
			req := httptest.NewRequest(method, route, nil)
			response := httptest.NewRecorder()
			s.ServeHTTP(response, req)
			if response.Code != 401 || count != before {
				t.Fatal("unauthenticated capability invocation", route)
			}
			req.Header.Set("Authorization", "Bearer "+testToken)
			req.Header.Set("Origin", "http://127.0.0.1")
			response = httptest.NewRecorder()
			s.ServeHTTP(response, req)
			if response.Code != 403 || count != before {
				t.Fatal("origin reached capability", route)
			}
			if response = call(s, method, route, []byte(`{}`)); response.Code != 202 || count != before+1 {
				t.Fatal("fixed route unavailable", route, method, response.Code)
			}
			raw, _ := json.Marshal(LocalRequest{Method: method, Path: route})
			if _, err = DecodeLocalRequest(base64.StdEncoding.EncodeToString(raw)); err != nil {
				t.Fatal("local transport rejected fixed route", route, method, err)
			}
		}
		if response := call(s, "DELETE", route, nil); response.Code != 405 {
			t.Fatal("unsupported method accepted", route, response.Code)
		}
		if response := call(s, methods[0], route+"?maxWidth=960", nil); response.Code != 400 {
			t.Fatal("capability query accepted", route, response.Code)
		}
		raw, _ := json.Marshal(LocalRequest{Method: methods[0], Path: route + "?x=1"})
		if _, err = DecodeLocalRequest(base64.StdEncoding.EncodeToString(raw)); err == nil {
			t.Fatal("transport accepted capability query")
		}
	}
	for _, route := range []string{"/desktop", "/desktop/", "/desktop/control/private", "/desktop/shell", "/desktop/admin", "/desktop/exec", "/desktop/launch/../exec", "/api/exec", "/rfb", "/health", "/files"} {
		if handler, err := New(Config{Workspace: t.TempDir(), Token: testToken, ExtraRoutes: map[string]http.Handler{route: http.NotFoundHandler()}}); err == nil {
			_ = handler.Close()
			t.Fatal("arbitrary or reserved route registered", route)
		}
		raw, _ := json.Marshal(LocalRequest{Method: "POST", Path: route})
		if _, err = DecodeLocalRequest(base64.StdEncoding.EncodeToString(raw)); err == nil {
			t.Fatal("transport accepted private route", route)
		}
	}
	if response := call(s, "POST", "/desktop/input", make([]byte, maxRequestBytes+1)); response.Code != 413 {
		t.Fatal("unbounded capability request", response.Code)
	}
	if response := call(s, "GET", "/desktop%2Fstate", nil); response.Code != 400 {
		t.Fatal("escaped alias accepted", response.Code)
	}
}

// TestExecGateRunsOnlyAfterAuthenticationAndValidation verifies that the execution gate runs only after authentication and validation.
func TestExecGateRunsOnlyAfterAuthenticationAndValidation(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	s, err := New(Config{Workspace: dir, Token: testToken, ExecGate: func(context.Context, func(context.Context) error) error {
		calls++
		return errors.New("secret controller token and ownership details")
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	request := httptest.NewRequest("POST", "/exec", strings.NewReader(`{"argv":["true"]}`))
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 401 || calls != 0 {
		t.Fatal("unauthenticated command reached gate")
	}
	for _, body := range []string{`{}`, `{"argv":["true"],"cwd":"../bad"}`, `{"argv":["definitely-not-found"]}`, `{"argv":["true"],"env":{"LD_PRELOAD":"x"}}`} {
		response := call(s, "POST", "/exec", []byte(body))
		if response.Code != 400 || calls != 0 {
			t.Fatal("invalid command reached gate", response.Code, calls)
		}
	}
	response = call(s, "POST", "/exec", []byte(`{"argv":["sh","-c","printf forbidden > proof"]}`))
	if response.Code != 409 || calls != 1 || strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "token") {
		t.Fatal(response.Code, response.Body, calls)
	}
	if _, err = os.Stat(filepath.Join(dir, "proof")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected command executed")
	}
}

// TestExecGateSuccessAndCallbackAtMostOnce verifies successful execution gating and the single-callback limit.
func TestExecGateSuccessAndCallbackAtMostOnce(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	s, err := New(Config{Workspace: dir, Token: testToken, ExecGate: func(ctx context.Context, run func(context.Context) error) error {
		calls++
		return run(ctx)
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result := run(t, s, ExecRequest{Argv: []string{"sh", "-c", "printf approved; exit 7"}})
	if result.ExitCode != 7 || result.Stdout != "approved" || calls != 1 {
		t.Fatal(result, calls)
	}
	twice, err := New(Config{Workspace: dir, Token: testToken, ExecGate: func(ctx context.Context, run func(context.Context) error) error {
		if err := run(ctx); err != nil {
			return err
		}
		return run(ctx)
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer twice.Close()
	response := call(twice, "POST", "/exec", []byte(`{"argv":["sh","-c","printf x >> once"]}`))
	if response.Code != 409 {
		t.Fatal(response.Code)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "once"))
	if err != nil || string(raw) != "x" {
		t.Fatal("callback executed multiple times", string(raw), err)
	}
	var body map[string]any
	decoder := json.NewDecoder(response.Body)
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("multiple HTTP response values")
	}
}

// TestExecGatePreemptionKillsOwnedCommandGroup verifies that gate preemption kills the owned command group.
func TestExecGatePreemptionKillsOwnedCommandGroup(t *testing.T) {
	dir := t.TempDir()
	leaseCtx, revoke := context.WithCancel(context.Background())
	defer revoke()
	s, err := New(Config{Workspace: dir, Token: testToken, ExecGate: func(_ context.Context, run func(context.Context) error) error {
		return run(leaseCtx)
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- call(s, "POST", "/exec", []byte(`{"argv":["sh","-c","sleep 30 & echo $! > child.pid; wait"],"timeoutMs":30000}`))
	}()
	pid := waitPID(t, filepath.Join(dir, "child.pid"))
	revoke()
	select {
	case response := <-done:
		if response.Code != 409 {
			t.Fatal(response.Code, response.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("revoked command did not stop")
	}
	assertNotRunning(t, pid)
}

// TestExecGateRequestTimeoutStillBoundsMisparentedContext verifies that request timeouts bound a misparented gate context.
func TestExecGateRequestTimeoutStillBoundsMisparentedContext(t *testing.T) {
	s, err := New(Config{Workspace: t.TempDir(), Token: testToken, ExecGate: func(_ context.Context, run func(context.Context) error) error {
		return run(context.Background())
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result := run(t, s, ExecRequest{Argv: []string{"sh", "-c", "sleep 30"}, TimeoutMS: 100})
	if !result.TimedOut || result.ExitCode == 0 {
		t.Fatal("outer deadline lost through gate", result)
	}
}

// TestExecGateCannotRetainAsyncCommand verifies that the execution gate cannot retain an asynchronous command.
func TestExecGateCannotRetainAsyncCommand(t *testing.T) {
	dir := t.TempDir()
	finished := make(chan error, 1)
	s, err := New(Config{Workspace: dir, Token: testToken, ExecGate: func(ctx context.Context, run func(context.Context) error) error {
		go func() {
			finished <- run(ctx)
		}()
		_ = waitPID(t, filepath.Join(dir, "child.pid"))
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	response := call(s, "POST", "/exec", []byte(`{"argv":["sh","-c","echo $$ > child.pid; sleep 30"]}`))
	if response.Code != 409 {
		t.Fatal(response.Code, response.Body)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("retained callback outlived gate")
	}
	pid := waitPID(t, filepath.Join(dir, "child.pid"))
	assertNotRunning(t, pid)
}
func waitPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(path); err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("owned process proof missing")
	return 0
}
