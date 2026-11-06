package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFixturePrivateBoundedState verifies fixture private bounded state.
func TestFixturePrivateBoundedState(t *testing.T) {
	f := NewFixtureSite()
	call := func(path, origin, host, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:123"
		r.Host = host
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.ServeHTTP(w, r)
		return w
	}
	if r := call("/fixture/state", "http://127.0.0.1:8081", "127.0.0.1:8081", `{"text":"exact"}`); r.Code != http.StatusOK {
		t.Fatal(r.Code)
	}
	snapshot := f.Snapshot()
	snapshot[0] = 'x'
	if string(f.Snapshot()) != `{"text":"exact"}` {
		t.Fatal("alias")
	}
	for _, tc := range []struct{ path, origin, host, body string }{{"/fixture/state", "http://evil.test", "127.0.0.1:8081", `{}`}, {"/fixture/state", "http://127.0.0.1:8081", "evil.test", `{}`}, {"/fixture/state", "http://127.0.0.1:8081", "127.0.0.1:8081", strings.Repeat("x", 128*1024+1)}, {"/fixture/state", "http://127.0.0.1:8081", "127.0.0.1:8081", `[]`}} {
		if r := call(tc.path, tc.origin, tc.host, tc.body); r.Code < 400 {
			t.Fatal("accepted badrequest")
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:8081/fixture", nil)
	r.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	f.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Input Lab") {
		t.Fatal(w.Code)
	}
}
