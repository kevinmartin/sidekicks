package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testApp() *app { return &app{root: "../..", token: "test-token", client: http.DefaultClient} }
func call(a *app, method, path, body, origin, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:7790"+path, strings.NewReader(body))
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	return w
}
func TestMutationsRequireSessionAndSameOrigin(t *testing.T) {
	a := testApp()
	body := `{"bot":"Reviewer","intent":"Inspect synthetic fixture"}`
	for _, c := range []struct {
		origin, token string
		want          int
	}{{"", "", 403}, {"http://evil.example", "test-token", 403}, {"http://localhost:7790", "test-token", 403}, {"http://127.0.0.1:7790", "test-token", 201}, {"", "test-token", 201}} {
		w := call(a, "POST", "/api/thread", body, c.origin, c.token)
		if w.Code != c.want {
			t.Fatalf("origin=%s token=%s got %d: %s", c.origin, c.token, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "http://attacker.example:7790/health", nil)
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted Host accepted")
	}
}
func TestInputValidation(t *testing.T) {
	cases := []struct {
		body  string
		valid bool
	}{{`{"session":"s","token":"t","events":[{"type":"click","x":30,"y":40,"button":"right","count":2}]}`, true}, {`{"session":"s","token":"t","events":[{"type":"pointer","x":30,"y":40,"action":"up","button":"left"}]}`, true}, {`{"session":"s","events":[{"type":"releaseAll"}]}`, false}, {`{"session":"s","token":"t","events":[{"type":"exec","command":"rm"}]}`, false}, {`{"session":"s","token":"t","events":[{"type":"click","x":-1,"y":2,"button":"left","count":1}]}`, false}, {`{"session":"s","token":"t","events":[{"type":"scroll","x":1,"y":2,"deltaX":0,"deltaY":2001}]}`, false}, {`{"session":"s","token":"t","events":[]}`, false}}
	for _, c := range cases {
		var body map[string]any
		json.Unmarshal([]byte(c.body), &body)
		err := validatePayload("input", body)
		if (err == nil) != c.valid {
			t.Fatalf("valid=%v err=%v payload=%s", c.valid, err, c.body)
		}
	}
}
func TestControlValidation(t *testing.T) {
	for _, body := range []map[string]any{{"action": "acquire", "owner": "root", "session": "s"}, {"action": "acquire", "owner": "human", "session": "s", "leaseMs": float64(60001)}, {"action": "steal", "owner": "agent", "session": "s"}} {
		if validatePayload("control", body) == nil {
			t.Fatalf("invalid control accepted: %v", body)
		}
	}
}
func TestScreenshotRejectsOversizeAndBounds(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 50, 30))
	img.Set(2, 2, color.White)
	var pngBody bytes.Buffer
	png.Encode(&pngBody, img)
	payload := pngBody.Bytes()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload) }))
	defer backend.Close()
	prior := endpoints["novnc"]
	endpoints["novnc"] = backend.URL
	defer func() { endpoints["novnc"] = prior }()
	a := testApp()
	w := call(a, "GET", "/api/adapters/novnc/screenshot?maxWidth=60&maxHeight=40", "", "", "")
	if w.Code != 200 || w.Header().Get("X-Screenshot-Width") != "50" {
		t.Fatalf("valid screenshot: %d %s", w.Code, w.Body.String())
	}
	for _, c := range []struct {
		path string
		want int
	}{{"?maxWidth=40&maxHeight=20", 502}, {"?maxWidth=1921", 400}, {"?maxHeight=-1", 400}} {
		w = call(a, "GET", "/api/adapters/novnc/screenshot"+c.path, "", "", "")
		if w.Code != c.want {
			t.Fatalf("%s: got %d expected %d", c.path, w.Code, c.want)
		}
	}
	payload = bytes.Repeat([]byte{'x'}, maxScreenshot+1)
	w = call(a, "GET", "/api/adapters/novnc/screenshot", "", "", "")
	if w.Code != 502 {
		t.Fatal("oversized screenshot accepted")
	}
	payload = []byte("not an image")
	w = call(a, "GET", "/api/adapters/novnc/screenshot", "", "", "")
	if w.Code != 502 {
		t.Fatal("non-image accepted")
	}
}
func TestProxyPreservesAdapterConflict(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		w.Write([]byte(`{"ok":false,"error":"human owns control"}`))
	}))
	defer backend.Close()
	prior := endpoints["neko"]
	endpoints["neko"] = backend.URL
	defer func() { endpoints["neko"] = prior }()
	w := call(testApp(), "POST", "/api/adapters/neko/control", `{"action":"acquire","owner":"agent","session":"s"}`, "", "test-token")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "human owns control") {
		t.Fatalf("adapter conflict lost: %d %s", w.Code, w.Body.String())
	}
}
