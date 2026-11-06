// Package guest implements the authenticated service inside a disposable workload.
// It deliberately executes guest commands, not host commands. Runtime isolation is
// provided by the container/VM, not by this HTTP API or its workspace file policy.
package guest

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"
)

const (
	// MaxFileBytes limits a workspace file operation.
	MaxFileBytes = 1 << 20
	// MaxOutputBytes limits each command output stream.
	MaxOutputBytes  = 256 << 10
	maxRequestBytes = 128 << 10
)

// Config describes a guest service and its authenticated routes.
type Config struct {
	Workspace         string
	Token             string
	MaxConcurrentExec int
	// Health reports current workload readiness. Its errors are never returned to clients.
	Health func() error
	// ExecGate wraps the complete command lifetime for workload-specific ownership.
	// A nil gate leaves headless-shell exec unchanged.
	ExecGate func(context.Context, func(context.Context) error) error
	// ExtraRoutes accepts only the fixed browser/desktop capability routes.
	ExtraRoutes map[string]http.Handler
}

// Server owns an opened workspace root. Close it only after HTTP shutdown has
// drained requests. Authentication is mandatory on every route, including health.
type Server struct {
	token       string
	root        *os.Root
	slots       chan struct{}
	closeOnce   sync.Once
	extraRoutes map[string]http.Handler
	health      func() error
	execGate    func(context.Context, func(context.Context) error) error
	requests    *requestStore
}

// New constructs an authenticated guest service.
func New(cfg Config) (*Server, error) {
	if len(cfg.Token) < 32 || len(cfg.Token) > 256 || strings.ContainsAny(cfg.Token, " \t\r\n") {
		return nil, errors.New("agent token must contain 32..256 non-whitespace bytes")
	}
	if cfg.MaxConcurrentExec == 0 {
		cfg.MaxConcurrentExec = 4
	}
	if cfg.MaxConcurrentExec < 1 || cfg.MaxConcurrentExec > 16 {
		return nil, errors.New("concurrent exec must be 1..16")
	}
	extra := make(map[string]http.Handler, len(cfg.ExtraRoutes))
	for route, handler := range cfg.ExtraRoutes {
		if len(extraMethods(route)) == 0 || handler == nil {
			return nil, errors.New("unsupported extra route")
		}
		extra[route] = handler
	}
	root, err := os.OpenRoot(cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("open workspace: %w", err)
	}
	return &Server{token: cfg.Token, root: root, slots: make(chan struct{}, cfg.MaxConcurrentExec), extraRoutes: extra, health: cfg.Health, execGate: cfg.ExecGate, requests: newRequestStore()}, nil
}

// Close closes the guest service and its workspace root.
func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.requests.close()
		err = s.root.Close()
	})
	return err
}

// ServeHTTP serves authenticated guest API requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Guest APIs are not browser endpoints. In particular, an arbitrary web page
	// must not turn its visitor's local access into authenticated guest operations.
	for _, origin := range r.Header.Values("Origin") {
		if origin != "" {
			writeError(w, http.StatusForbidden, "browser origins are not accepted")
			return
		}
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(s.token)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="sidekicks-agent"`)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if r.URL.Path == "/cancel" {
		s.handleCancel(w, r)
		return
	}
	tracked, complete, ok := s.trackRequest(w, r)
	if !ok {
		return
	}
	defer complete()
	r = tracked
	if handler, ok := s.extraRoutes[r.URL.Path]; ok {
		if r.URL.RawQuery != "" || r.URL.RawPath != "" {
			writeError(w, http.StatusBadRequest, "capability route queries and aliases are not accepted")
			return
		}
		methods := extraMethods(r.URL.Path)
		allowed := false
		for _, want := range methods {
			if r.Method == want {
				allowed = true
				break
			}
		}
		if !allowed {
			w.Header().Set("Allow", strings.Join(methods, ", "))
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if r.ContentLength > maxRequestBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "capability request exceeds limit")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		handler.ServeHTTP(w, r)
		return
	}
	switch r.URL.Path {
	case "/health":
		if !method(w, r, http.MethodGet) {
			return
		}
		if s.health != nil && s.health() != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ready": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ready": true})
	case "/exec":
		if !method(w, r, http.MethodPost) {
			return
		}
		s.handleExec(w, r)
	case "/files":
		switch r.Method {
		case http.MethodGet:
			s.handleRead(w, r)
		case http.MethodPut:
			s.handleWrite(w, r)
		default:
			w.Header().Set("Allow", "GET, PUT")
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func method(w http.ResponseWriter, r *http.Request, want string) bool {
	if r.Method == want {
		return true
	}
	w.Header().Set("Allow", want)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "application/json required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid bounded json request")
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		writeError(w, http.StatusBadRequest, "one json value required")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// extraMethods is an exact route/method allowlist, not a prefix proxy.
func extraMethods(route string) []string {
	switch route {
	case "/browser", "/desktop/connect", "/desktop/launch", "/desktop/input":
		return []string{http.MethodPost}
	case "/desktop/state", "/desktop/capture":
		return []string{http.MethodGet}
	case "/desktop/control":
		return []string{http.MethodGet, http.MethodPost}
	default:
		return nil
	}
}
