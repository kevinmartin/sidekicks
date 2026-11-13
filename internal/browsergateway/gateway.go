// Package browsergateway serves an authenticated browser connection for a
// Chromium instance on the same network namespace's loopback.
package browsergateway

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var browserSocketID = regexp.MustCompile(`^[a-f0-9-]+$`)

type Gateway struct {
	expectedAuthorization []byte
	discoveryURL          string
	client                *http.Client
	proxy                 *httputil.ReverseProxy
}

// New configures an authenticated proxy to Chromium on guest loopback.
func New(token, cdpAddress string) (*Gateway, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("browser gateway token is required")
	}
	host, port, err := net.SplitHostPort(cdpAddress)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("browser CDP address must contain a loopback IP and port")
	}
	if number, err := strconv.ParseUint(port, 10, 16); err != nil || number == 0 {
		return nil, errors.New("browser CDP address must contain a valid TCP port")
	}
	upstream := &url.URL{
		Scheme: "http",
		Host:   cdpAddress,
	}
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		ResponseHeaderTimeout: 3 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   2 * time.Second,
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(upstream)
			r.Out.Header.Del("Authorization")
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "Chromium CDP is unavailable", http.StatusBadGateway)
		},
	}
	return &Gateway{
		expectedAuthorization: []byte("Bearer " + token),
		discoveryURL:          upstream.String() + "/json/version",
		client:                client,
		proxy:                 proxy,
	}, nil
}

func (g *Gateway) Health(w http.ResponseWriter, r *http.Request) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, g.discoveryURL, nil)
	if err == nil {
		resp, err := g.client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
	}
	http.Error(w, "Chromium CDP is unavailable", http.StatusServiceUnavailable)
}

func (g *Gateway) Authenticate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), g.expectedAuthorization) != 1 {
			http.Error(w, "browser gateway authorization required", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// Discovery retains Chromium's loopback URL for the Stagehand worker.
func (g *Gateway) Discovery(w http.ResponseWriter, r *http.Request) {
	g.forward(w, r)
}

func (g *Gateway) WebSocket(w http.ResponseWriter, r *http.Request) {
	if !browserSocketID.MatchString(r.PathValue("id")) || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.NotFound(w, r)
		return
	}
	g.forward(w, r)
}

func (g *Gateway) forward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.URL.RawPath != "" {
		http.NotFound(w, r)
		return
	}
	g.proxy.ServeHTTP(w, r)
}
