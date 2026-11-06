package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func handlerCall(t *testing.T, h http.Handler, body any) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/browser", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var result map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
	return w.Code, result
}

// TestHandlerActualProtocolFlow verifies handler actual protocol flow.
func TestHandlerActualProtocolFlow(t *testing.T) {
	f := newFake(t)
	data := pngData(t, 32, 24)
	f.reply = func(c command) any {
		switch c.Method {
		case "Runtime.evaluate":
			expression, _ := c.Params["expression"].(string)
			value := "Sidekicks ✓"
			if expression == "String(document.readyState)" {
				value = "complete"
			}
			return map[string]any{"result": map[string]any{"type": "string", "value": value}}
		case "Page.captureScreenshot":
			return map[string]any{"data": data}
		default:
			return map[string]any{}
		}
	}
	client, _ := New(f.server.URL)
	defer client.Close(context.Background())
	h := NewHandler(client)
	status, r := handlerCall(t, h, request{Action: "new"})
	if status != 200 {
		t.Fatal(status, r)
	}
	id, ok := r["pageId"].(string)
	if !ok || len(id) != 32 || strings.Contains(id, "owned") {
		t.Fatal(r)
	}
	for _, req := range []request{
		{Action: "navigate", PageID: id, URL: "https://example.com"},
		{Action: "title", PageID: id},
		{Action: "text", PageID: id, Selector: "body"},
		{Action: "click", PageID: id, X: 10, Y: 20},
		{Action: "type", PageID: id, Text: "Hi ✓"},
		{Action: "screenshot", PageID: id, Width: 32, Height: 24},
		{Action: "close", PageID: id},
	} {
		status, result := handlerCall(t, h, req)
		if status != 200 || result["ok"] != true {
			t.Fatal(req, status, result)
		}
		switch req.Action {
		case "title":
			if result["title"] != "Sidekicks ✓" {
				t.Fatal(result)
			}
		case "text":
			if result["text"] != "Sidekicks ✓" {
				t.Fatal(result)
			}
		case "screenshot":
			b, err := base64.StdEncoding.DecodeString(result["data"].(string))
			if err != nil || len(b) == 0 || result["mime"] != "image/png" {
				t.Fatal(result, err)
			}
		}
	}
	if status, r := handlerCall(t, h, request{Action: "title", PageID: id}); status != 404 {
		t.Fatal(status, r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	want := []string{"Page.navigate", "Runtime.evaluate", "Runtime.evaluate", "Runtime.evaluate", "Input.dispatchMouseEvent", "Input.dispatchMouseEvent", "Input.insertText", "Emulation.setDeviceMetricsOverride", "Page.captureScreenshot"}
	if len(f.commands) != len(want) {
		t.Fatalf("unexpected protocol commands: %+v", f.commands)
	}
	for i, method := range want {
		if f.commands[i].Method != method {
			t.Fatalf("command %d: %+v", i, f.commands[i])
		}
	}
	if f.commands[0].Params["url"] != "https://example.com" || f.commands[4].Params["type"] != "mousePressed" || f.commands[5].Params["type"] != "mouseReleased" || f.commands[6].Params["text"] != "Hi ✓" {
		t.Fatalf("incorrect protocol parameters: %+v", f.commands)
	}
}

// TestHandlerMachineBoundary verifies handler machine boundary.
func TestHandlerMachineBoundary(t *testing.T) {
	h := NewHandler(nil)
	for _, tc := range []struct {
		name, method, path, content, origin, body string
		status                                    int
	}{
		{name: "method", method: "GET", path: "/browser", status: 405},
		{name: "path", method: "POST", path: "/other", status: 404},
		{name: "query", method: "POST", path: "/browser?endpoint=http://evil", status: 404},
		{name: "origin", method: "POST", path: "/browser", origin: "http://127.0.0.1", content: "application/json", body: `{"action":"new"}`, status: 403},
		{name: "content", method: "POST", path: "/browser", content: "text/plain", body: `{"action":"new"}`, status: 415},
		{name: "bad-content", method: "POST", path: "/browser", content: "!", status: 415},
		{name: "invalid-json", method: "POST", path: "/browser", content: "application/json", body: `{`, status: 400},
		{name: "unknown-field", method: "POST", path: "/browser", content: "application/json", body: `{"action":"new","endpoint":"http://evil"}`, status: 400},
		{name: "extra-json", method: "POST", path: "/browser", content: "application/json", body: `{"action":"new"} {}`, status: 400},
		{name: "oversize", method: "POST", path: "/browser", content: "application/json", body: `{"text":"` + strings.Repeat("a", 128<<10) + `"}`, status: 413},
		{name: "oversize-trailing-whitespace", method: "POST", path: "/browser", content: "application/json", body: `{"action":"new"}` + strings.Repeat(" ", 128<<10), status: 413},
		{name: "unavailable", method: "POST", path: "/browser", content: "application/json", body: `{"action":"new"}`, status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.content != "" {
				req.Header.Set("Content-Type", tc.content)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.status == 405 && w.Header().Get("Allow") != "POST" {
				t.Fatal("Allow missing")
			}
		})
	}
}

// TestHandlerRejectsMultipleOriginHeaders verifies every Origin value is checked.
func TestHandlerRejectsMultipleOriginHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/browser", strings.NewReader(`{"action":"new"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Add("Origin", "")
	r.Header.Add("Origin", "https://example.com")
	w := httptest.NewRecorder()
	NewHandler(nil).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("multiple Origin headers accepted: %d", w.Code)
	}
}

// TestHandlerRequestAndPageValidation verifies handler request and page validation.
func TestHandlerRequestAndPageValidation(t *testing.T) {
	f := newFake(t)
	client, _ := New(f.server.URL)
	defer client.Close(context.Background())
	h := NewHandler(client)
	_, result := handlerCall(t, h, request{Action: "new"})
	id := result["pageId"].(string)
	for _, r := range []request{
		{Action: "invalid"}, {Action: "new", PageID: id}, {Action: "new", Text: "unexpected"}, {Action: "new", Width: 1},
		{Action: "close"}, {Action: "close", PageID: strings.Repeat("x", 32)},
		{Action: "title", PageID: id, URL: "https://example.com"}, {Action: "title", PageID: id, Text: "unexpected"},
		{Action: "title", PageID: id, Selector: "unexpected"}, {Action: "title", PageID: id, X: 1}, {Action: "title", PageID: id, Width: 1},
		{Action: "navigate", PageID: id, URL: "file:///etc/passwd"}, {Action: "text", PageID: id},
		{Action: "click", PageID: id, X: -1}, {Action: "type", PageID: id, Text: "\x00"}, {Action: "screenshot", PageID: id, Width: 1921},
	} {
		if status, result := handlerCall(t, h, r); status != 400 {
			t.Fatal(r, status, result)
		}
	}
	if status, r := handlerCall(t, h, request{Action: "title", PageID: strings.Repeat("0", 32)}); status != 404 {
		t.Fatal(status, r)
	}
}

// TestHandlerLimitsAndUpstreamFailure verifies handler limits and upstream failure.
func TestHandlerLimitsAndUpstreamFailure(t *testing.T) {
	f := newFake(t)
	client, _ := New(f.server.URL)
	h := NewHandler(client)
	for i := 0; i < maxPages; i++ {
		if status, r := handlerCall(t, h, request{Action: "new"}); status != 200 {
			t.Fatal(status, r)
		}
	}
	if status, r := handlerCall(t, h, request{Action: "new"}); status != 429 {
		t.Fatal(status, r)
	}
	_ = client.Close(context.Background())
	f2 := newFake(t)
	f2.discoveryStatus = 503
	c2, _ := New(f2.server.URL)
	h2 := NewHandler(c2)
	if status, r := handlerCall(t, h2, request{Action: "new"}); status != 503 {
		t.Fatal(status, r)
	}
}

type failedResponseWriter struct{ header http.Header }

// Header returns the fake response headers.
func (w *failedResponseWriter) Header() http.Header {
	return w.header
}

// WriteHeader records no status for the failed response.
func (w *failedResponseWriter) WriteHeader(int) {

}

// Write simulates a disconnected client.
func (w *failedResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

// TestHandlerLostNewResponseRetiresUnclaimedPage verifies handler lost new response retires unclaimed page.
func TestHandlerLostNewResponseRetiresUnclaimedPage(t *testing.T) {
	f := newFake(t)
	client, _ := New(f.server.URL)
	defer client.Close(context.Background())
	h := NewHandler(client)
	for i := 0; i < maxPages+1; i++ {
		req := httptest.NewRequest(http.MethodPost, "/browser", strings.NewReader(`{"action":"new"}`))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(&failedResponseWriter{header: make(http.Header)}, req)
	}
	f.mu.Lock()
	closed := f.closeCount
	f.mu.Unlock()
	if closed != maxPages+1 {
		t.Fatalf("lost responses did not close owned pages: got %d", closed)
	}
	if status, body := handlerCall(t, h, request{Action: "new"}); status != http.StatusOK {
		t.Fatalf("unclaimed pages exhausted capacity: %d %v", status, body)
	}
}

// TestUnclaimedExpiryAndClaimAreMutuallyExclusive verifies unclaimed expiry and claim are mutually exclusive.
func TestUnclaimedExpiryAndClaimAreMutuallyExclusive(t *testing.T) {
	f := newFake(t)
	f.reply = func(command) any {
		return map[string]any{"result": map[string]any{"type": "string", "value": "ready"}}
	}
	client, _ := New(f.server.URL)
	defer client.Close(context.Background())
	h := NewHandler(client).(*handler)
	status, body := handlerCall(t, h, request{Action: "new"})
	if status != http.StatusOK || body["claimWithinMs"] != float64(unclaimedPageTTL.Milliseconds()) {
		t.Fatal(status, body)
	}
	id := body["pageId"].(string)
	h.mu.Lock()
	expiring := h.unclaimed[id]
	h.mu.Unlock()
	h.expireUnclaimed(id, expiring)
	if status, _ = handlerCall(t, h, request{Action: "title", PageID: id}); status != http.StatusNotFound {
		t.Fatal("expired page remained claimable", status)
	}
	status, body = handlerCall(t, h, request{Action: "new"})
	if status != http.StatusOK {
		t.Fatal(status, body)
	}
	id = body["pageId"].(string)
	h.mu.Lock()
	claimed := h.unclaimed[id]
	h.mu.Unlock()
	if status, body = handlerCall(t, h, request{Action: "title", PageID: id}); status != http.StatusOK {
		t.Fatal(status, body)
	}
	// Simulate a timer callback that was already queued when the page was claimed.
	h.expireUnclaimed(id, claimed)
	if status, body = handlerCall(t, h, request{Action: "title", PageID: id}); status != http.StatusOK {
		t.Fatal("expiry closed a claimed page", status, body)
	}
}

// TestConcurrentClaimAndExpiryNeverCloseClaimedPage verifies concurrent claim and expiry never close claimed page.
func TestConcurrentClaimAndExpiryNeverCloseClaimedPage(t *testing.T) {
	f := newFake(t)
	f.reply = func(command) any {
		return map[string]any{"result": map[string]any{"type": "string", "value": "ready"}}
	}
	client, _ := New(f.server.URL)
	defer client.Close(context.Background())
	h := NewHandler(client).(*handler)
	for i := 0; i < 16; i++ {
		status, body := handlerCall(t, h, request{Action: "new"})
		if status != http.StatusOK {
			t.Fatal(status, body)
		}
		id := body["pageId"].(string)
		f.mu.Lock()
		closedBefore := f.closeCount
		f.mu.Unlock()
		h.mu.Lock()
		entry := h.unclaimed[id]
		expired := make(chan struct{})
		result := make(chan int, 1)
		go func() {
			h.expireUnclaimed(id, entry)
			close(expired)
		}()
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/browser", strings.NewReader(`{"action":"title","pageId":"`+id+`"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			result <- w.Code
		}()
		h.mu.Unlock()
		code := <-result
		<-expired
		f.mu.Lock()
		closedAfter := f.closeCount
		f.mu.Unlock()
		switch code {
		case http.StatusOK:
			if closedAfter != closedBefore {
				t.Fatal("expiry closed a successfully claimed page")
			}
			if status, body := handlerCall(t, h, request{Action: "close", PageID: id}); status != http.StatusOK {
				t.Fatal(status, body)
			}
		case http.StatusNotFound:
			if closedAfter != closedBefore+1 {
				t.Fatal("expiry did not close unclaimed page")
			}
		default:
			t.Fatal("unexpected claim outcome", code)
		}
	}
}

// TestExpiredPageWithFailedCleanupRemainsOwnedForCloseRetry verifies expired page with failed cleanup remains owned for close retry.
func TestExpiredPageWithFailedCleanupRemainsOwnedForCloseRetry(t *testing.T) {
	f := newFake(t)
	f.closeStatuses = []int{http.StatusServiceUnavailable, http.StatusOK}
	client, _ := New(f.server.URL)
	h := NewHandler(client).(*handler)
	status, body := handlerCall(t, h, request{Action: "new"})
	if status != http.StatusOK {
		t.Fatal(status, body)
	}
	id := body["pageId"].(string)
	h.mu.Lock()
	entry := h.unclaimed[id]
	h.mu.Unlock()
	h.expireUnclaimed(id, entry)
	client.mu.Lock()
	owned := len(client.pages)
	client.mu.Unlock()
	if owned != 1 {
		t.Fatal("failed cleanup discarded ownership")
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal("cleanup retry failed:", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closeCount != 2 {
		t.Fatalf("close attempts: %d", f.closeCount)
	}
}

// TestHandlerCommandFailureIsRedacted verifies handler command failure is redacted.
func TestHandlerCommandFailureIsRedacted(t *testing.T) {
	f := newFake(t)
	f.reply = func(c command) any {
		return protocolFailure{-32000}
	}
	c, _ := New(f.server.URL)
	defer c.Close(context.Background())
	h := NewHandler(c)
	_, r := handlerCall(t, h, request{Action: "new"})
	id := r["pageId"].(string)
	if status, r := handlerCall(t, h, request{Action: "title", PageID: id}); status != 502 || r["error"] != "browser operation failed" {
		t.Fatal(status, r)
	}
}

// TestClickFailureRetiresOwnedPage verifies click failure retires owned page.
func TestClickFailureRetiresOwnedPage(t *testing.T) {
	f := newFake(t)
	f.reply = func(c command) any {
		if c.Params["type"] == "mouseReleased" {
			return protocolFailure{-32000}
		}
		return map[string]any{}
	}
	c, _ := New(f.server.URL)
	defer c.Close(context.Background())
	h := NewHandler(c)
	_, r := handlerCall(t, h, request{Action: "new"})
	id := r["pageId"].(string)
	if status, r := handlerCall(t, h, request{Action: "click", PageID: id, X: 10, Y: 20}); status != 502 {
		t.Fatal(status, r)
	}
	if status, r := handlerCall(t, h, request{Action: "title", PageID: id}); status != 404 {
		t.Fatal(status, r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closeCount != 1 {
		t.Fatal("failed click target not retired", f.closeCount)
	}
}

// TestHandlerCloseFailureRetainsHandleForRetry verifies handler close failure retains handle for retry.
func TestHandlerCloseFailureRetainsHandleForRetry(t *testing.T) {
	f := newFake(t)
	f.closeStatuses = []int{http.StatusServiceUnavailable, http.StatusOK}
	c, _ := New(f.server.URL)
	defer c.Close(context.Background())
	h := NewHandler(c)
	_, r := handlerCall(t, h, request{Action: "new"})
	id := r["pageId"].(string)
	if status, r := handlerCall(t, h, request{Action: "close", PageID: id}); status != http.StatusBadGateway {
		t.Fatal(status, r)
	}
	c.mu.Lock()
	count := len(c.pages)
	c.mu.Unlock()
	if count != 1 {
		t.Fatal("client forgot failed close")
	}
	if status, r := handlerCall(t, h, request{Action: "close", PageID: id}); status != http.StatusOK {
		t.Fatal(status, r)
	}
	if status, r := handlerCall(t, h, request{Action: "title", PageID: id}); status != http.StatusNotFound {
		t.Fatal(status, r)
	}
}

// TestHandlerCommandFailuresDoNotExhaustOwnedSlots verifies handler command failures do not exhaust owned slots.
func TestHandlerCommandFailuresDoNotExhaustOwnedSlots(t *testing.T) {
	f := newFake(t)
	f.wsBehavior = "bad-json"
	c, _ := New(f.server.URL)
	defer c.Close(context.Background())
	h := NewHandler(c)
	for i := 0; i < maxPages*2; i++ {
		status, r := handlerCall(t, h, request{Action: "new"})
		if status != http.StatusOK {
			t.Fatalf("creation%d: %d %v", i, status, r)
		}
		id := r["pageId"].(string)
		if status, r := handlerCall(t, h, request{Action: "title", PageID: id}); status != http.StatusBadGateway {
			t.Fatal(status, r)
		}
		c.mu.Lock()
		count := len(c.pages)
		c.mu.Unlock()
		if count != 0 {
			t.Fatal("failed target was not cleaned", i, count)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closeCount != maxPages*2 {
		t.Fatal(f.closeCount)
	}
}

// TestHandlerFailedAutoCleanupKeepsCloseHandle verifies handler failed auto cleanup keeps close handle.
func TestHandlerFailedAutoCleanupKeepsCloseHandle(t *testing.T) {
	f := newFake(t)
	f.wsBehavior = "bad-json"
	f.closeStatuses = []int{http.StatusServiceUnavailable, http.StatusOK}
	c, _ := New(f.server.URL)
	defer c.Close(context.Background())
	h := NewHandler(c)
	_, r := handlerCall(t, h, request{Action: "new"})
	id := r["pageId"].(string)
	if status, r := handlerCall(t, h, request{Action: "title", PageID: id}); status != http.StatusBadGateway {
		t.Fatal(status, r)
	}
	c.mu.Lock()
	count := len(c.pages)
	c.mu.Unlock()
	if count != 1 {
		t.Fatal("failed auto cleanup lost ownership")
	}
	if status, r := handlerCall(t, h, request{Action: "close", PageID: id}); status != http.StatusOK {
		t.Fatal(status, r)
	}
}

func noisyPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	state := uint32(0x4e454b4f)
	for i := 0; i < len(img.Pix); i += 4 {
		for j := 0; j < 3; j++ {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			img.Pix[i+j] = byte(state)
		}
		img.Pix[i+3] = 255
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestHandlerPNGResponseBound verifies handler png response bound.
func TestHandlerPNGResponseBound(t *testing.T) {
	for _, tc := range []struct{ width, height, status int }{
		{512, 512, http.StatusOK}, {800, 600, http.StatusRequestEntityTooLarge},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			raw := noisyPNG(t, tc.width, tc.height)
			if tc.status == http.StatusRequestEntityTooLarge && len(raw) <= maxHandlerPNG {
				t.Fatal("oversize test fixture did not exceed raw bound")
			}
			f := newFake(t)
			f.reply = func(c command) any {
				return map[string]any{"data": base64.StdEncoding.EncodeToString(raw)}
			}
			c, _ := New(f.server.URL)
			defer c.Close(context.Background())
			h := NewHandler(c)
			_, result := handlerCall(t, h, request{Action: "new"})
			id := result["pageId"].(string)
			body, _ := json.Marshal(request{Action: "screenshot", PageID: id, Width: tc.width, Height: tc.height})
			req := httptest.NewRequest(http.MethodPost, "/browser", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
			// The entire JSON, including base64 and envelope fields, stays below the
			// guest 2MiB bound and the Docker3MiB outer envelope bound.
			if w.Body.Len() > 1400<<10 {
				t.Fatal("private response exceeds wire bound", w.Body.Len())
			}
			if tc.status == http.StatusOK {
				var result struct{ Data []byte }
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(result.Data, raw) {
					t.Fatal("encoded body lost PNG data")
				}
			} else if strings.Contains(w.Body.String(), "data") {
				t.Fatal("oversize error leaked image payload")
			}
		})
	}
}

// TestHandlerBoundsEscapedTextEnvelope verifies handler bounds escaped text envelope.
func TestHandlerBoundsEscapedTextEnvelope(t *testing.T) {
	f := newFake(t)
	f.reply = func(c command) any {
		return map[string]any{"result": map[string]any{"type": "string", "value": strings.Repeat("\x01", 1<<20)}}
	}
	c, _ := New(f.server.URL)
	defer c.Close(context.Background())
	h := NewHandler(c)
	_, r := handlerCall(t, h, request{Action: "new"})
	id := r["pageId"].(string)
	if status, r := handlerCall(t, h, request{Action: "text", PageID: id, Selector: "body"}); status != http.StatusRequestEntityTooLarge {
		t.Fatal(status, r)
	}
}
