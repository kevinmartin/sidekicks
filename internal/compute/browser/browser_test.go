package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type fakeCDP struct {
	server          *httptest.Server
	mu              sync.Mutex
	commands        []command
	closeCount      int
	closeStatuses   []int
	newCount        int
	reply           func(command) any
	badTarget       func(*target)
	discoveryStatus int
	badDiscovery    string
	wsBehavior      string
}
type command struct {
	ID     uint64         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

func newFake(t *testing.T) *fakeCDP {
	t.Helper()
	f := &fakeCDP{}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}
func (f *fakeCDP) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/json/new":
		if r.Method != http.MethodPut {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		f.mu.Lock()
		f.newCount++
		f.mu.Unlock()
		if f.discoveryStatus != 0 {
			w.WriteHeader(f.discoveryStatus)
			return
		}
		if f.badDiscovery != "" {
			w.Write([]byte(f.badDiscovery))
			return
		}
		v := target{ID: "owned-1", Type: "page", WebSocketURL: "ws" + strings.TrimPrefix(f.server.URL, "http") + "/devtools/page/owned-1"}
		if f.badTarget != nil {
			f.badTarget(&v)
		}
		json.NewEncoder(w).Encode(v)
	case strings.HasPrefix(r.URL.Path, "/json/close/"):
		f.mu.Lock()
		f.closeCount++
		status := http.StatusOK
		if len(f.closeStatuses) > 0 {
			status = f.closeStatuses[0]
			f.closeStatuses = f.closeStatuses[1:]
		}
		f.mu.Unlock()
		w.WriteHeader(status)
		w.Write([]byte("Target is closing"))
	case strings.HasPrefix(r.URL.Path, "/devtools/page/"):
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, b, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var cmd command
			if json.Unmarshal(b, &cmd) != nil {
				return
			}
			f.mu.Lock()
			f.commands = append(f.commands, cmd)
			f.mu.Unlock()
			switch f.wsBehavior {
			case "hang":
				continue
			case "binary":
				_ = conn.Write(r.Context(), websocket.MessageBinary, []byte{1})
				continue
			case "bad-json":
				_ = conn.Write(r.Context(), websocket.MessageText, []byte("{"))
				continue
			case "wrong-id":
				_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"id":999,"result":{}}`))
				continue
			}
			// Interleaved events must not be mistaken for command responses.
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"method":"Page.frameStartedLoading","params":{}}`))
			result := any(map[string]any{})
			if f.reply != nil {
				result = f.reply(cmd)
			}
			reply := map[string]any{"id": cmd.ID, "result": result}
			if e, ok := result.(protocolFailure); ok {
				reply = map[string]any{"id": cmd.ID, "error": map[string]any{"code": e.code, "message": "private error details"}}
			}
			out, _ := json.Marshal(reply)
			if err = conn.Write(r.Context(), websocket.MessageText, out); err != nil {
				return
			}
		}
	default:
		http.NotFound(w, r)
	}
}

type protocolFailure struct{ code int }

func newPage(t *testing.T, f *fakeCDP) *Page {
	t.Helper()
	c, err := New(f.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.NewPage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = c.Close(ctx)
	})
	return p
}
func pngData(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{10, 20, 30, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

// TestEndpointValidation verifies endpoint validation.
func TestEndpointValidation(t *testing.T) {
	for _, v := range []string{"", "http://example.com:9222", "https://127.0.0.1:9222", "http://localhost:9222", "http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://user:pass@127.0.0.1:9222", "http://127.0.0.1:9222/path", "http://127.0.0.1:9222/?x=1", "http://127.0.0.1:9222/#x", "http://[::1]", "http://[::1]:-1", "http://["} {
		t.Run(v, func(t *testing.T) {
			if _, err := New(v); !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
	for _, v := range []string{"http://127.0.0.1:9222", "http://[::1]:9222/"} {
		if _, err := New(v); err != nil {
			t.Fatal(v, err)
		}
	}
}

// TestDiscoveryBoundaryAndCleanup verifies discovery boundary and cleanup.
func TestDiscoveryBoundaryAndCleanup(t *testing.T) {
	for name, mutate := range map[string]func(*target){
		"other-authority": func(v *target) {
			v.WebSocketURL = "ws://127.0.0.1:9/devtools/page/owned-1"
		},
		"credentials": func(v *target) {
			v.WebSocketURL = strings.Replace(v.WebSocketURL, "ws://", "ws://user:secret@", 1)
		},
		"other-target": func(v *target) {
			v.WebSocketURL = strings.Replace(v.WebSocketURL, "owned-1", "foreign", 1)
		},
		"browser-target": func(v *target) {
			v.Type = "browser"
		},
		"target-traversal": func(v *target) {
			v.ID = "../private"
		},
		"encoded-path": func(v *target) {
			v.WebSocketURL = strings.Replace(v.WebSocketURL, "owned-1", "%6fwned-1", 1)
		},
		"query": func(v *target) {
			v.WebSocketURL += "?token=secret"
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			f.badTarget = mutate
			c, _ := New(f.server.URL)
			if _, err := c.NewPage(context.Background()); err == nil {
				t.Fatal("accepted invalid target")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.commands) != 0 {
				t.Fatal("connected invalid target")
			}
			if name != "target-traversal" && f.closeCount != 1 {
				t.Fatal("owned target not cleaned")
			}
		})
	}
}

// TestDiscoveryErrorsAndLimits verifies discovery errors and limits.
func TestDiscoveryErrorsAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{name: "status", status: 503}, {name: "redirect", status: http.StatusFound}, {name: "bad-json", body: "{"}, {name: "oversize", body: strings.Repeat(" ", maxDiscovery+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			f.discoveryStatus = tc.status
			f.badDiscovery = tc.body
			c, _ := New(f.server.URL)
			if _, err := c.NewPage(context.Background()); err == nil {
				t.Fatal("wanted error")
			}
		})
	}
}

// TestNoRedirectsEvenWithCallerClient verifies no redirects even with caller client.
func TestNoRedirectsEvenWithCallerClient(t *testing.T) {
	calls := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer other.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer s.Close()
	c, err := New(s.URL, WithHTTPClient(&http.Client{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.NewPage(context.Background()); err == nil {
		t.Fatal("accepted redirect")
	}
	if calls != 0 {
		t.Fatal("followed redirect")
	}
}

// TestBrowserCommandsAndSelectorEscaping verifies browser commands and selector escaping.
func TestBrowserCommandsAndSelectorEscaping(t *testing.T) {
	f := newFake(t)
	f.reply = func(c command) any {
		if c.Method == "Runtime.evaluate" {
			return map[string]any{"result": map[string]any{"type": "string", "value": "Sidekicks ✓"}}
		}
		return map[string]any{}
	}
	p := newPage(t, f)
	ctx := context.Background()
	if err := p.Navigate(ctx, "https://example.com/?x=1#fragment"); err != nil {
		t.Fatal(err)
	}
	if v, err := p.Title(ctx); err != nil || v != "Sidekicks ✓" {
		t.Fatal(v, err)
	}
	selector := "div[data-x=\"');alert(1);//\"]"
	if v, err := p.Text(ctx, selector); err != nil || v != "Sidekicks ✓" {
		t.Fatal(v, err)
	}
	if err := p.Click(ctx, 12.5, 200); err != nil {
		t.Fatal(err)
	}
	if err := p.Type(ctx, "Hi ✓\n"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	methods := []string{"Page.navigate", "Runtime.evaluate", "Runtime.evaluate", "Input.dispatchMouseEvent", "Input.dispatchMouseEvent", "Input.insertText"}
	if len(f.commands) != len(methods) {
		t.Fatal(f.commands)
	}
	for i, c := range f.commands {
		if c.Method != methods[i] {
			t.Fatal(c)
		}
	}
	quoted, _ := json.Marshal(selector)
	expected := "String(document.querySelector(" + string(quoted) + ")?.textContent ?? '').slice(0,1048576)"
	if f.commands[2].Params["expression"] != expected {
		t.Fatal("selector not JSON quoted")
	}
	if f.commands[4].Params["buttons"] != float64(0) {
		t.Fatal("release missing")
	}
}

// TestInputAndNavigationValidationBeforeDispatch verifies input and navigation validation before dispatch.
func TestInputAndNavigationValidationBeforeDispatch(t *testing.T) {
	f := newFake(t)
	p := newPage(t, f)
	ctx := context.Background()
	for _, address := range []string{"file:///etc/passwd", "javascript:alert(1)", "data:text/html,hi", "http://", "https://user:secret@example.com/", "http://example.com/\n", "http://example.com/\x00", strings.Repeat("x", 8193)} {
		if !errors.Is(p.Navigate(ctx, address), ErrInvalid) {
			t.Fatal(address)
		}
	}
	for _, v := range []float64{-1, 4097, math.NaN(), math.Inf(1)} {
		if !errors.Is(p.Click(ctx, v, 0), ErrInvalid) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"\x00", strings.Repeat("x", 65537), string([]byte{0xff})} {
		if !errors.Is(p.Type(ctx, v), ErrInvalid) {
			t.Fatal("accepted invalid text")
		}
	}
	for _, v := range []string{"", "a\x00", strings.Repeat("x", 1025)} {
		if _, err := p.Text(ctx, v); !errors.Is(err, ErrInvalid) {
			t.Fatal("accepted invalid selector")
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.commands) != 0 {
		t.Fatal("invalid input reached browser")
	}
}

// TestScreenshotBoundsAndPNG verifies screenshot bounds and png.
func TestScreenshotBoundsAndPNG(t *testing.T) {
	f := newFake(t)
	data := pngData(t, 32, 24)
	f.reply = func(c command) any {
		if c.Method == "Page.captureScreenshot" {
			return map[string]any{"data": data}
		}
		return map[string]any{}
	}
	p := newPage(t, f)
	b, err := p.ScreenshotPNG(context.Background(), ScreenshotOptions{32, 24})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width != 32 || cfg.Height != 24 {
		t.Fatal(cfg, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.commands) != 2 {
		t.Fatal(f.commands)
	}
	if f.commands[1].Params["captureBeyondViewport"] != false {
		t.Fatal("unbounded capture")
	}
	for _, o := range []ScreenshotOptions{{-1, 1}, {1921, 1}, {1, -1}, {1, 1081}} {
		if _, err := p.ScreenshotPNG(context.Background(), o); !errors.Is(err, ErrInvalid) {
			t.Fatal(o, err)
		}
	}
}

// TestScreenshotRejectsMalformedOrMismatched verifies screenshot rejects malformed or mismatched.
func TestScreenshotRejectsMalformedOrMismatched(t *testing.T) {
	for _, tc := range []struct{ name, data string }{
		{"invalid-base64", "%"}, {"not-png", base64.StdEncoding.EncodeToString([]byte("hello"))}, {"wrong-size", pngData(t, 1, 1)}, {"oversize", strings.Repeat("A", base64.StdEncoding.EncodedLen(maxPNG+1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			f.reply = func(c command) any {
				return map[string]any{"data": tc.data}
			}
			p := newPage(t, f)
			if _, err := p.ScreenshotPNG(context.Background(), ScreenshotOptions{32, 24}); err == nil {
				t.Fatal("accepted bad image")
			}
		})
	}
}

// TestDefaultScreenshot verifies default screenshot.
func TestDefaultScreenshot(t *testing.T) {
	f := newFake(t)
	data := pngData(t, 1280, 720)
	f.reply = func(c command) any {
		return map[string]any{"data": data}
	}
	p := newPage(t, f)
	if _, err := p.ScreenshotPNG(context.Background(), ScreenshotOptions{}); err != nil {
		t.Fatal(err)
	}
}

// TestProtocolFailuresAndClosedState verifies protocol failures and closed state.
func TestProtocolFailuresAndClosedState(t *testing.T) {
	for _, behavior := range []string{"binary", "bad-json", "wrong-id"} {
		t.Run(behavior, func(t *testing.T) {
			f := newFake(t)
			f.wsBehavior = behavior
			p := newPage(t, f)
			if err := p.Type(context.Background(), "x"); err == nil {
				t.Fatal("accepted bad protocol")
			}
			if err := p.Type(context.Background(), "x"); !errors.Is(err, ErrClosed) {
				t.Fatal(err)
			}
		})
	}
}

// TestCDPErrorDoesNotLeakDetailsAndReleasesClick verifies cdp error does not leak details and releases click.
func TestCDPErrorDoesNotLeakDetailsAndReleasesClick(t *testing.T) {
	f := newFake(t)
	f.reply = func(c command) any {
		if c.Params["type"] == "mousePressed" {
			return protocolFailure{-32602}
		}
		return map[string]any{}
	}
	p := newPage(t, f)
	err := p.Click(context.Background(), 1, 2)
	if err == nil || strings.Contains(err.Error(), "private error details") {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.commands) != 2 || f.commands[1].Params["type"] != "mouseReleased" {
		t.Fatal(f.commands)
	}
}

// TestNavigationAndTextExceptions verifies navigation and text exceptions.
func TestNavigationAndTextExceptions(t *testing.T) {
	f := newFake(t)
	f.reply = func(c command) any {
		if c.Method == "Page.navigate" {
			return map[string]any{"errorText": "private URL failure"}
		}
		return map[string]any{"exceptionDetails": map[string]any{"text": "bad selector"}}
	}
	p := newPage(t, f)
	if err := p.Navigate(context.Background(), "https://example.com"); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
	if _, err := p.Title(context.Background()); err == nil {
		t.Fatal("accepted exception")
	}
}

// TestCancellationClosesConnectionAndAllowsCleanup verifies cancellation closes connection and allows cleanup.
func TestCancellationClosesConnectionAndAllowsCleanup(t *testing.T) {
	f := newFake(t)
	f.wsBehavior = "hang"
	p := newPage(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := p.Type(ctx, "x"); err == nil {
		t.Fatal("missing timeout")
	}
	if err := p.Type(context.Background(), "y"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closeCount != 1 {
		t.Fatal(f.closeCount)
	}
}

// TestClientLimitAndOwnership verifies client limit and ownership.
func TestClientLimitAndOwnership(t *testing.T) {
	f := newFake(t)
	c, _ := New(f.server.URL)
	for i := 0; i < maxPages; i++ {
		if _, err := c.NewPage(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.NewPage(context.Background()); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NewPage(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closeCount != maxPages {
		t.Fatal(f.closeCount)
	}
}

// TestOperationsDoNotInterleaveClickPairs verifies operations do not interleave click pairs.
func TestOperationsDoNotInterleaveClickPairs(t *testing.T) {
	f := newFake(t)
	p := newPage(t, f)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := p.Click(context.Background(), float64(i), 10); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.commands) != 24 {
		t.Fatal(len(f.commands))
	}
	for i := 0; i < 24; i += 2 {
		a, b := f.commands[i], f.commands[i+1]
		if a.Params["type"] != "mousePressed" || b.Params["type"] != "mouseReleased" || a.Params["x"] != b.Params["x"] {
			t.Fatal("interleaved", a, b)
		}
	}
}

// TestWaitReady verifies wait ready.
func TestWaitReady(t *testing.T) {
	f := newFake(t)
	var mu sync.Mutex
	states := []string{"loading", "interactive"}
	f.reply = func(c command) any {
		mu.Lock()
		defer mu.Unlock()
		v := states[0]
		if len(states) > 1 {
			states = states[1:]
		}
		return map[string]any{"result": map[string]any{"type": "string", "value": v}}
	}
	p := newPage(t, f)
	if err := p.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.commands) != 2 {
		t.Fatal(f.commands)
	}
}

type lostCreationTransport struct{ calls int }

// RoundTrip simulates a target created before its response is lost.
func (t *lostCreationTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++ // Model a target created before its response is lost.
	return nil, errors.New("response lost")
}

// TestUnknownCreationFencesNewPagesAndCloseReportsUncertainty verifies unknown creation fences new pages and close reports uncertainty.
func TestUnknownCreationFencesNewPagesAndCloseReportsUncertainty(t *testing.T) {
	transport := &lostCreationTransport{}
	c, err := New("http://127.0.0.1:9222", WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxPages+2; i++ {
		if _, err := c.NewPage(context.Background()); !errors.Is(err, ErrCreationUncertain) {
			t.Fatalf("creation %d: %v", i, err)
		}
	}
	if transport.calls != 1 {
		t.Fatalf("uncertain creation allowed %d upstream requests", transport.calls)
	}
	if err := c.Close(context.Background()); !errors.Is(err, ErrCreationUncertain) {
		t.Fatalf("Close hid unconfirmed target: %v", err)
	}
}

// TestHandlerNavigationAndReadinessRemainOnePageOperation verifies handler navigation and readiness remain one page operation.
func TestHandlerNavigationAndReadinessRemainOnePageOperation(t *testing.T) {
	f := newFake(t)
	started, release := make(chan struct{}), make(chan struct{})
	f.reply = func(c command) any {
		if c.Method == "Page.navigate" && c.Params["url"] == "https://a.example" {
			close(started)
			<-release
		}
		if c.Method == "Runtime.evaluate" {
			return map[string]any{"result": map[string]any{"type": "string", "value": "complete"}}
		}
		return map[string]any{}
	}
	p := newPage(t, f)
	h := &handler{client: p.client, pages: map[string]*Page{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": p}}
	results := make(chan error, 2)
	go func() {
		_, err := h.execute(context.Background(), request{Action: "navigate", PageID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", URL: "https://a.example"})
		results <- err
	}()
	<-started
	go func() {
		_, err := h.execute(context.Background(), request{Action: "navigate", PageID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", URL: "https://b.example"})
		results <- err
	}()
	// A is blocked waiting for its CDP response, so the only goroutine that can
	// be waiting in Page.operation is B. Observe that wait before letting A
	// complete; the old split-lock handler then deterministically dispatches B
	// before A's readiness check.
	waiting := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		stack := make([]byte, 1<<20)
		n := runtime.Stack(stack, true)
		for _, goroutine := range strings.Split(string(stack[:n]), "\n\n") {
			if strings.Contains(goroutine, "browser.(*Page).operation(") && strings.Contains(goroutine, "TestHandlerNavigationAndReadinessRemainOnePageOperation") {
				waiting = true
				break
			}
		}
		if waiting {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	if !waiting {
		t.Fatal("second navigation did not wait for the page operation")
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.commands) != 4 || f.commands[0].Method != "Page.navigate" || f.commands[0].Params["url"] != "https://a.example" || f.commands[1].Method != "Runtime.evaluate" || f.commands[2].Method != "Page.navigate" || f.commands[2].Params["url"] != "https://b.example" || f.commands[3].Method != "Runtime.evaluate" {
		t.Fatalf("interleaved navigations: %+v", f.commands)
	}
}

// TestOperationWaitHonorsCancellation verifies operation wait honors cancellation.
func TestOperationWaitHonorsCancellation(t *testing.T) {
	f := newFake(t)
	p := newPage(t, f)
	p.operations <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Title(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := p.Text(ctx, "body"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := p.Navigate(ctx, "about:blank"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := p.Click(ctx, 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := p.Type(ctx, "hello"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := p.WaitReady(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := p.ScreenshotPNG(ctx, ScreenshotOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-p.operations
}

// TestMalformedResultsAndOutputLimit verifies malformed results and output limit.
func TestMalformedResultsAndOutputLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result any
	}{
		{"invalid-shape", []int{1}},
		{"wrong-value-type", map[string]any{"result": map[string]any{"type": "number", "value": 1}}},
		{"text-limit", map[string]any{"result": map[string]any{"type": "string", "value": strings.Repeat("x", (1<<20)+1)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			f.reply = func(c command) any {
				return tc.result
			}
			p := newPage(t, f)
			if _, err := p.Title(context.Background()); err == nil {
				t.Fatal("accepted result")
			}
		})
	}
}

// TestDiscoveryConnectionFailure verifies discovery connection failure.
func TestDiscoveryConnectionFailure(t *testing.T) {
	f := newFake(t)
	endpoint := f.server.URL
	f.server.Close()
	c, _ := New(endpoint)
	if _, err := c.NewPage(context.Background()); err == nil {
		t.Fatal("missing connection error")
	}
}

// TestCanceledCloseRemainsOwnedAndCanRetry verifies canceled close remains owned and can retry.
func TestCanceledCloseRemainsOwnedAndCanRetry(t *testing.T) {
	f := newFake(t)
	p := newPage(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("close=%v", err)
	}
	p.client.mu.Lock()
	_, owned := p.client.pages[p]
	p.client.mu.Unlock()
	if !owned {
		t.Fatal("canceled close forgot owned target")
	}
	p.mu.Lock()
	retired := p.retired
	p.mu.Unlock()
	if retired {
		t.Fatal("unconfirmed close marked retired")
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	p.client.mu.Lock()
	_, owned = p.client.pages[p]
	p.client.mu.Unlock()
	if owned {
		t.Fatal("confirmed close remains tracked")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closeCount != 1 {
		t.Fatal(f.closeCount)
	}
}

// TestClientCloseFailureCanRetryAndAlreadyGoneRetires verifies client close failure can retry and already gone retires.
func TestClientCloseFailureCanRetryAndAlreadyGoneRetires(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newFake(t)
			f.closeStatuses = []int{http.StatusServiceUnavailable, status}
			p := newPage(t, f)
			c := p.client
			if err := c.Close(context.Background()); err == nil {
				t.Fatal("transient failure hidden")
			}
			c.mu.Lock()
			count := len(c.pages)
			c.mu.Unlock()
			if count != 1 {
				t.Fatal("failed close lost target")
			}
			if err := c.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			c.mu.Lock()
			count = len(c.pages)
			c.mu.Unlock()
			if count != 0 {
				t.Fatal("confirmed close not retired")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.closeCount != 2 {
				t.Fatal(f.closeCount)
			}
		})
	}
}

// TestFailedDiscoveryCleanupRemainsBoundedAndRetryable verifies failed discovery cleanup remains bounded and retryable.
func TestFailedDiscoveryCleanupRemainsBoundedAndRetryable(t *testing.T) {
	f := newFake(t)
	f.badTarget = func(v *target) {
		v.WebSocketURL = "ws://127.0.0.1:9/devtools/page/owned-1"
	}
	f.closeStatuses = []int{http.StatusServiceUnavailable, http.StatusOK}
	c, _ := New(f.server.URL)
	if _, err := c.NewPage(context.Background()); err == nil {
		t.Fatal("invalid target accepted")
	}
	c.mu.Lock()
	count := len(c.pages)
	c.mu.Unlock()
	if count != 1 {
		t.Fatal("unconfirmed created target forgotten")
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	count = len(c.pages)
	c.mu.Unlock()
	if count != 0 {
		t.Fatal("retry did not retire target")
	}
}
