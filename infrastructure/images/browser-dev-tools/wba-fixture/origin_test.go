package main

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOriginReadinessProbe(t *testing.T) {
	f := &fixture{
		config: config{
			PodIP:        netip.MustParseAddr("10.244.0.17"),
			SignedHost:   "signed.example.test",
			UnsignedHost: "unsigned.example.test",
			BlockedHost:  "blocked.example.test",
		},
		port: "8443",
	}
	handler := f.origin()
	for _, tc := range []struct {
		name   string
		host   string
		method string
		status int
	}{
		{"pod IP", "10.244.0.17:8443", http.MethodGet, http.StatusNoContent},
		{"pod IP HEAD", "10.244.0.17:8443", http.MethodHead, http.StatusNoContent},
		{"another IP", "10.244.0.18:8443", http.MethodGet, http.StatusForbidden},
		{"pod IP with another Host port", "10.244.0.17:9443", http.MethodGet, http.StatusNoContent},
		{"wrong method", "10.244.0.17:8443", http.MethodPost, http.StatusForbidden},
		{"signed host", "signed.example.test:8443", http.MethodGet, http.StatusUnauthorized},
		{"signed host HEAD", "signed.example.test:8443", http.MethodHead, http.StatusUnauthorized},
		{"unsigned host", "unsigned.example.test:8443", http.MethodGet, http.StatusOK},
		{"blocked host", "blocked.example.test:8443", http.MethodGet, http.StatusForbidden},
		{"unknown host", "other.example.test:8443", http.MethodGet, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "/healthz", nil)
			request.Host = tc.host
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code)
		})
	}
}
