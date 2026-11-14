package main

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"time"
)

type observation struct {
	key   string
	event string
}

func (f *fixture) record(key, event string) {
	if key == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.recent) == 512 {
		copy(f.recent, f.recent[1:])
		f.recent = f.recent[:len(f.recent)-1]
	}
	f.recent = append(f.recent, observation{
		key:   key,
		event: event,
	})
}

func (f *fixture) observer() http.Handler {
	expected := []byte("Bearer " + f.config.Token)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /origins", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"signed":      "https://" + net.JoinHostPort(f.config.SignedHost, f.port),
			"blockedHost": f.config.BlockedHost,
		})
	})
	mux.HandleFunc("GET /readiness", func(w http.ResponseWriter, r *http.Request) {
		targetID := r.URL.Query().Get("targetId")
		f.mu.Lock()
		state := f.ready[targetID]
		f.mu.Unlock()
		current := time.Now().Before(state.expires)
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]bool{
			"ready":   current && !state.retired,
			"retired": current && state.retired,
		})
	})
	mux.HandleFunc("GET /observations", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		var events []string
		f.mu.Lock()
		for _, entry := range f.recent {
			if entry.key == key {
				events = append(events, entry.event)
			}
		}
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(events)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), expected) != 1 {
			http.Error(w, "fixture authorization required", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mux.ServeHTTP(w, r)
	})
}
