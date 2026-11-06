package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSharedDesktopInputAsset verifies shared desktop input asset.
func TestSharedDesktopInputAsset(t *testing.T) {
	for _, kind := range []string{"desktop-novnc", "desktop-neko"} {
		t.Run(kind, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/input.mjs", nil)
			w := httptest.NewRecorder()
			desktopAssets(kind).ServeHTTP(w, r)
			if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "javascript") || w.Body.Len() == 0 {
				t.Fatalf("shared input asset: status=%d content-type=%q bytes=%d", w.Code, w.Header().Get("Content-Type"), w.Body.Len())
			}
		})
	}
}
