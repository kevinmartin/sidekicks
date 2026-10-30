package web

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"
)

// FixtureSite is a bounded synthetic diagnostic. It observes only the explicit
// Input Lab page, never other browser tabs or desktop applications.
type FixtureSite struct {
	mu       sync.Mutex
	snapshot json.RawMessage
}

// NewFixtureSite creates an empty synthetic diagnostic site.
func NewFixtureSite() *FixtureSite {
	return &FixtureSite{}
}

// Snapshot returns a copy of the latest synthetic diagnostic state.
func (f *FixtureSite) Snapshot() json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append(json.RawMessage(nil), f.snapshot...)
}

// ServeHTTP serves the fixture and accepts its private state updates.
func (f *FixtureSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host != "127.0.0.1" || r.Host != "127.0.0.1:8081" {
		http.Error(w, "private guest fixture only", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/fixture" {
		Fixture(w, r)
		return
	}
	if r.URL.Path != "/fixture/state" || r.Method != "POST" || r.Header.Get("Origin") != "http://127.0.0.1:8081" || r.Header.Get("Content-Type") != "application/json" {
		http.NotFound(w, r)
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128<<10))
	if err != nil || !json.Valid(b) {
		http.Error(w, "invalid bounded fixture state", http.StatusBadRequest)
		return
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(b, &doc) != nil || doc == nil {
		http.Error(w, "fixture object required", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.snapshot = append(json.RawMessage(nil), b...)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}
