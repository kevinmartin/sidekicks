// Package web contains original browser-side Sidekicks assets. Protocol clients
// remain upstream JavaScript supplied by the pinned image/build dependencies.
package web

import (
	"embed"
	"net/http"
)

// Assets contains the embedded fixture and desktop viewer assets.
//
//go:embed fixture.html desktop/home.html desktop/novnc/* desktop/neko/* desktop/viewer.css desktop/input.mjs
var Assets embed.FS

// Fixture serves only the fixed synthetic fixture, never arbitrary filesystem data.
func Fixture(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" || r.URL.Path != "/fixture" {
		http.NotFound(w, r)
		return
	}
	b, _ := Assets.ReadFile("fixture.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}
