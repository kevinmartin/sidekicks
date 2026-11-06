// Package viewer exposes authenticated desktop-only controls. It intentionally
// has no exec, files, Docker, process, host-shell or machine-administration route.
package viewer

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute/control"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop"
)

const cookiePrefix = "sidekicks_viewer_"

// EpochHeader binds a queued browser request to the non-secret lease generation.
const EpochHeader = "X-Sidekicks-Epoch"

var (
	// ErrLimit indicates that a resource limit has been reached.
	ErrLimit = errors.New("viewer capacity reached")
	// ErrClosed indicates that the resource is closed.
	ErrClosed = errors.New("viewer service closed")
)

// Gate coordinates ownership and input dispatch.
type Gate interface {
	desktop.HumanGate
	Acquire(context.Context, control.Request) (control.Lease, error)
	View() control.View
}

// Driver provides desktop input and capture operations.
type Driver interface {
	Input(context.Context, desktop.Event) error
	CapturePNG(context.Context) ([]byte, error)
	Size() (int, int)
}

// Config configures the viewer service and its control dependencies.
type Config struct {
	Gate      Gate
	Driver    Driver
	RFBTarget string
	Assets    http.Handler
	// NativeViewerID resolves a server-observed native connection for this
	// authenticated viewer session. Caller-supplied backend IDs are never trusted.
	NativeViewerID func(session string) (string, error)
	// OnAcquire binds the exact newly acquired/renewed lease to backend viewer
	// lifecycle state before credentials or an acknowledgement become public.
	// Failure releases only that exact lease, never a newer owner.
	OnAcquire func(session string, lease control.Lease) error
	// Input may implement a different backend's normalized transport. It must
	// preserve final-dispatch human/gated ownership checks for every operation.
	Input func(context.Context, control.Credentials, []desktop.Event) error
	// Additional may serve only fixed desktop SDK paths after cookie/Host/Origin
	// checks. It must return false for unmatched routes and never delegate broad
	// machine APIs. The session argument is server-generated, never a URL token.
	Additional func(http.ResponseWriter, *http.Request, string) bool
	// ReadOnlyAdditionalPaths permits absent Origin only on these exact authenticated GET metadata routes; WebSocket upgrades still require exact Origin.
	ReadOnlyAdditionalPaths []string
	// OpaqueTicketQueryPaths allows one 32-byte opaque SDK token on an exact WebSocket path. The extension must bind that token to the authenticated cookie session; it is not a lease credential.
	OpaqueTicketQueryPaths []string
	// Tests may replace the expiry clock, not credential generation.
	Now func() time.Time
}

type session struct {
	id              string
	expires         time.Time
	credentials     control.Credentials // Service.mu protects this field.
	op              chan struct{}
	websocket       bool               // Service.mu protects the single connection per cookie.
	cancel          context.CancelFunc // Service.mu protects this field.
	rfb             *rfbStream         // Service.mu protects this field.
	rfbDisconnected bool               // Service.mu protects this field; reacquisition requires a new stream.
	rfbGeneration   uint64             // Service.mu protects this field, which changes when a stream is attached.
	dead            atomic.Bool
	lifecycle       context.Context
	closeLifecycle  context.CancelFunc
}

// rfbStream binds a lease to the exact video connection that observed it. Its
// context is canceled before disconnect cleanup, including while an acquire is
// still in flight. Only the associated credentials may be released on close.
type rfbStream struct {
	ctx         context.Context
	credentials control.Credentials // Service.mu protects this field.
}

// Service serves the authenticated viewer API.
type Service struct {
	cookieName         string
	config             Config
	mu                 sync.Mutex
	tickets            map[[32]byte]time.Time
	sessions           map[string]*session // Cookies contain random bearer data and are never logged.
	closed             bool
	capture            chan struct{}
	readOnlyAdditional map[string]struct{}
	opaqueTicketPaths  map[string]struct{}
}

// Response describes the current viewer control state.
type Response struct {
	OK      bool         `json:"ok"`
	Control control.View `json:"control"`
	Owned   bool         `json:"owned"`
	Epoch   uint64       `json:"epoch"`
	Width   int          `json:"width"`
	Height  int          `json:"height"`
}

// New constructs a viewer service with validated configuration.
func New(config Config) (*Service, error) {
	if config.Gate == nil || config.Driver == nil {
		return nil, errors.New("viewer gate and driver are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	width, height := config.Driver.Size()
	if width < 1 || height < 1 || width > 32767 || height > 32767 {
		return nil, errors.New("invalid viewer dimensions")
	}
	if config.RFBTarget != "" {
		// Validate the fixed private target up front. Per-request authority lists are
		// derived only after the public numeric-loopback Host check below.
		_, err := desktop.NewGateway(desktop.Config{Target: config.RFBTarget, Gate: config.Gate, Authenticate: func(*http.Request) (control.Credentials, error) {
			return control.Credentials{}, nil
		}, AllowedHosts: []string{"127.0.0.1:1"}, AllowedOrigins: []string{"http://127.0.0.1:1"}})
		if err != nil {
			return nil, err
		}
	}
	readOnly := map[string]struct{}{}
	for _, path := range config.ReadOnlyAdditionalPaths {
		if path == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?%\\") || strings.Contains(path, "..") || forbiddenPath(path) {
			return nil, errors.New("invalid read-only extension path")
		}
		readOnly[path] = struct{}{}
	}
	opaque := map[string]struct{}{}
	for _, path := range config.OpaqueTicketQueryPaths {
		if !strings.HasPrefix(path, "/desktop/stream/") || strings.ContainsAny(path, "?%\\") || strings.Contains(path, "..") {
			return nil, errors.New("invalid opaque sdk ticket path")
		}
		opaque[path] = struct{}{}
	}
	cookieID, err := secret()
	if err != nil {
		return nil, err
	}
	return &Service{cookieName: cookiePrefix + cookieID[:12], config: config, readOnlyAdditional: readOnly, opaqueTicketPaths: opaque, tickets: map[[32]byte]time.Time{}, sessions: map[string]*session{}, capture: make(chan struct{}, 1)}, nil
}
func secret() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// NewTicket is called by an already-authenticated private machine endpoint.
// The resulting one-time secret belongs in a URL fragment, never a query or log.
func (s *Service) NewTicket() (string, error) {
	expired := s.prune()
	s.cleanup(expired)
	ticket, err := secret()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", ErrClosed
	}
	if len(s.tickets) >= 8 {
		return "", ErrLimit
	}
	s.tickets[sha256.Sum256([]byte(ticket))] = s.config.Now().Add(time.Minute)
	return ticket, nil
}
func (s *Service) prune() []*session {
	now := s.config.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, until := range s.tickets {
		if !now.Before(until) {
			delete(s.tickets, token)
		}
	}
	var expired []*session
	for token, viewer := range s.sessions {
		if !now.Before(viewer.expires) {
			delete(s.sessions, token)
			viewer.dead.Store(true)
			viewer.closeLifecycle()
			expired = append(expired, viewer)
		}
	}
	return expired
}
func (s *Service) cleanup(viewers []*session) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, viewer := range viewers {
		viewer.closeLifecycle()
		s.mu.Lock()
		credentials := viewer.credentials
		close := viewer.cancel
		s.mu.Unlock()
		if close != nil {
			close()
		}
		if credentials.Token != "" {
			if lease, err := s.config.Gate.Validate(credentials); err == nil && lease.Owner == control.Human {
				_ = s.config.Gate.Release(ctx, credentials)
			}
		}
	}
}
func validHost(authority string) bool {
	host, port, err := net.SplitHostPort(authority)
	if err != nil || host != "127.0.0.1" {
		return false
	}
	n, err := strconv.ParseUint(port, 10, 16)
	return err == nil && n > 0 && port == strconv.FormatUint(n, 10)
}
func exactOrigin(r *http.Request) bool {
	values := r.Header.Values("Origin")
	return len(values) == 1 && values[0] == "http://"+r.Host
}
func (s *Service) lookup(r *http.Request) *session {
	cookie, err := r.Cookie(s.cookieName)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	viewer := s.sessions[cookie.Value]
	if viewer == nil || viewer.dead.Load() || !s.config.Now().Before(viewer.expires) {
		return nil
	}
	return viewer
}
func assetPath(path string) bool {
	return path == "/" || path == "/viewer.js" || path == "/viewer.css" || path == "/input.mjs" || path == "/favicon.ico" || strings.HasPrefix(path, "/vendor/novnc/") || strings.HasPrefix(path, "/vendor/neko/")
}
func forbiddenPath(path string) bool {
	return path == "/exec" || path == "/files" || strings.HasPrefix(path, "/files/") || strings.HasPrefix(path, "/v1/") || (strings.HasPrefix(path, "/desktop/") && !strings.HasPrefix(path, "/desktop/stream/"))
}

// ServeHTTP serves the HTTP request.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if !validHost(r.Host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.URL.RawPath != "" || strings.Contains(r.URL.Path, "\\") || strings.Contains(r.URL.Path, "..") {
		http.NotFound(w, r)
		return
	}
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	if queryErr != nil {
		http.Error(w, "invalid query", http.StatusBadRequest)
		return
	}
	_, opaquePath := s.opaqueTicketPaths[r.URL.Path]
	opaqueAllowed := false
	if opaquePath && r.Method == http.MethodGet && strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && len(query) == 1 && len(query["token"]) == 1 {
		decoded, err := base64.RawURLEncoding.DecodeString(query.Get("token"))
		opaqueAllowed = err == nil && len(decoded) == 32 && len(query.Get("token")) == 43
	}
	for key := range query {
		credential := strings.ToLower(key)
		if credential == "ticket" || credential == "session" || credential == "auth" || (credential == "token" && !(key == "token" && opaqueAllowed)) {
			http.Error(w, "query credentials are forbidden", http.StatusBadRequest)
			return
		}
	}
	if forbiddenPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		http.Error(w, "viewer unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodGet && assetPath(r.URL.Path) {
		if s.config.Assets == nil {
			http.NotFound(w, r)
			return
		}
		s.config.Assets.ServeHTTP(w, r)
		return
	}
	if r.Method != http.MethodGet && !exactOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	expired := s.prune()
	s.cleanup(expired)
	if r.URL.Path == "/auth" {
		if r.Method != http.MethodPost {
			method(w, http.MethodPost)
			return
		}
		s.authenticate(w, r)
		return
	}
	viewer := s.lookup(r)
	if viewer == nil {
		http.Error(w, "viewer authentication required", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), viewer.expires)
	stopLifecycle := context.AfterFunc(viewer.lifecycle, cancel)
	defer func() {
		stopLifecycle()
		cancel()
	}()
	r = r.WithContext(ctx)
	switch r.URL.Path {
	case "/state":
		if r.Method != http.MethodGet {
			method(w, http.MethodGet)
			return
		}
		s.state(w, viewer)
	case "/control":
		if r.Method == http.MethodGet {
			s.state(w, viewer)
			return
		}
		if r.Method != http.MethodPost {
			method(w, "GET, POST")
			return
		}
		s.control(w, r, viewer)
	case "/input":
		if r.Method != http.MethodPost {
			method(w, http.MethodPost)
			return
		}
		s.input(w, r, viewer)
	case "/screenshot":
		if r.Method != http.MethodGet {
			method(w, http.MethodGet)
			return
		}
		s.screenshot(w, r)
	case "/websockify":
		if r.Method != http.MethodGet {
			method(w, http.MethodGet)
			return
		}
		if !exactOrigin(r) || r.URL.RawQuery != "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		s.websocket(w, r, viewer)
	default:
		_, readOnly := s.readOnlyAdditional[r.URL.Path]
		allowOrigin := exactOrigin(r) || (r.Method == http.MethodGet && len(r.Header.Values("Origin")) == 0 && readOnly && !strings.EqualFold(r.Header.Get("Upgrade"), "websocket"))
		if s.config.Additional != nil && allowOrigin {
			if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				s.mu.Lock()
				if viewer.websocket || viewer.dead.Load() {
					s.mu.Unlock()
					http.Error(w, "viewer already connected", http.StatusConflict)
					return
				}
				viewer.websocket = true
				s.mu.Unlock()
				defer func() {
					s.mu.Lock()
					viewer.websocket = false
					s.mu.Unlock()
				}()
				tracked := &lifecycleWriter{ResponseWriter: w, ctx: r.Context()}
				defer tracked.stop()
				w = tracked
			}
			if s.config.Additional(w, r, viewer.id) {
				return
			}
		}
		http.NotFound(w, r)
	}
}
func method(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
func decode(w http.ResponseWriter, r *http.Request, target any) error {
	if content := r.Header.Get("Content-Type"); !strings.EqualFold(strings.TrimSpace(strings.Split(content, ";")[0]), "application/json") {
		return errors.New("json content type required")
	}
	reader := http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid json request")
	}
	var tail any
	if err := decoder.Decode(&tail); !errors.Is(err, io.EOF) {
		return errors.New("exactly one json value required")
	}
	return nil
}
func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (s *Service) authenticate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ticket string `json:"ticket"`
	}
	if decode(w, r, &body) != nil || len(body.Ticket) != 43 {
		http.Error(w, "invalid authentication", http.StatusUnauthorized)
		return
	}
	token, err := secret()
	if err != nil {
		http.Error(w, "viewer unavailable", http.StatusServiceUnavailable)
		return
	}
	id, err := secret()
	if err != nil {
		http.Error(w, "viewer unavailable", http.StatusServiceUnavailable)
		return
	}
	viewer := &session{id: "viewer-" + id, expires: s.config.Now().Add(30 * time.Minute), op: make(chan struct{}, 1)}
	viewer.op <- struct{}{}
	var retired *session
	s.mu.Lock()
	expiry, ok := s.tickets[sha256.Sum256([]byte(body.Ticket))]
	if !ok || !s.config.Now().Before(expiry) || s.closed {
		s.mu.Unlock()
		http.Error(w, "invalid authentication", http.StatusUnauthorized)
		return
	}
	if cookie, err := r.Cookie(s.cookieName); err == nil {
		retired = s.sessions[cookie.Value]
	}
	count := len(s.sessions)
	if retired != nil {
		count--
	}
	if count >= 8 {
		s.mu.Unlock()
		http.Error(w, "viewer capacity reached", http.StatusTooManyRequests)
		return
	}
	delete(s.tickets, sha256.Sum256([]byte(body.Ticket)))
	if retired != nil {
		for key, value := range s.sessions {
			if value == retired {
				delete(s.sessions, key)
			}
		}
		retired.dead.Store(true)
		retired.closeLifecycle()
	}
	viewer.lifecycle, viewer.closeLifecycle = context.WithCancel(context.Background())
	s.sessions[token] = viewer
	s.mu.Unlock()
	if retired != nil {
		s.cleanup([]*session{retired})
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 1800, Expires: viewer.expires})
	jsonResponse(w, http.StatusOK, map[string]bool{"ok": true})
}
func (s *Service) credentials(viewer *session) control.Credentials {
	s.mu.Lock()
	defer s.mu.Unlock()
	return viewer.credentials
}
func (s *Service) state(w http.ResponseWriter, viewer *session) {
	credentials := s.credentials(viewer)
	lease, err := s.config.Gate.Validate(credentials)
	owned := err == nil && lease.Owner == control.Human && lease.Session == viewer.id && !viewer.dead.Load()
	epoch := uint64(0)
	if owned {
		epoch = lease.Generation
	}
	width, height := s.config.Driver.Size()
	view := s.config.Gate.View()
	if driver, ok := s.config.Driver.(interface{ Ready() bool }); ok && !driver.Ready() {
		view.Ready = false
	}
	jsonResponse(w, http.StatusOK, Response{OK: true, Control: view, Owned: owned, Epoch: epoch, Width: width, Height: height})
}

func (s *Service) ownedEpoch(r *http.Request, viewer *session) (control.Credentials, control.Lease, error) {
	values := r.Header.Values(EpochHeader)
	if len(values) != 1 {
		return control.Credentials{}, control.Lease{}, control.ErrStale
	}
	epoch, err := strconv.ParseUint(values[0], 10, 64)
	if err != nil || epoch == 0 || strconv.FormatUint(epoch, 10) != values[0] {
		return control.Credentials{}, control.Lease{}, control.ErrStale
	}
	credentials := s.credentials(viewer)
	lease, err := s.config.Gate.Validate(credentials)
	if err != nil || viewer.dead.Load() || lease.Owner != control.Human || lease.Session != viewer.id || lease.Generation != epoch {
		return control.Credentials{}, control.Lease{}, control.ErrStale
	}
	return credentials, lease, nil
}

func (s *Service) control(w http.ResponseWriter, r *http.Request, viewer *session) {
	var body struct {
		Action  string       `json:"action"`
		Mode    control.Mode `json:"mode,omitempty"`
		LeaseMS int          `json:"leaseMs,omitempty"`
	}
	if err := decode(w, r, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.Mode != "" && body.Mode != control.Gated && body.Mode != control.Native {
		http.Error(w, "invalid control mode", http.StatusBadRequest)
		return
	}
	if body.LeaseMS == 0 {
		body.LeaseMS = 30000
	}
	if body.LeaseMS < 100 || body.LeaseMS > 60000 {
		http.Error(w, "lease duration must be 100..60000 milliseconds", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		http.Error(w, "control unavailable", http.StatusServiceUnavailable)
		return
	case <-viewer.op:
	}
	defer func() {
		viewer.op <- struct{}{}
	}()
	if viewer.dead.Load() {
		http.Error(w, "viewer expired", http.StatusUnauthorized)
		return
	}
	credentials := s.credentials(viewer)
	var stream *rfbStream
	var streamGeneration uint64
	if s.config.RFBTarget != "" && (body.Action == "acquire" || body.Action == "renew") {
		s.mu.Lock()
		stream = viewer.rfb
		streamGeneration = viewer.rfbGeneration
		disconnected := viewer.rfbDisconnected || (stream != nil && stream.ctx.Err() != nil)
		s.mu.Unlock()
		if disconnected {
			http.Error(w, "viewer stream disconnected", http.StatusConflict)
			return
		}
		if stream != nil {
			stop := context.AfterFunc(stream.ctx, cancel)
			defer stop()
		}
	}
	if body.Action == "renew" || body.Action == "release" || body.Action == "stop" {
		var err error
		credentials, _, err = s.ownedEpoch(r, viewer)
		if err != nil {
			http.Error(w, "stale viewer epoch", http.StatusConflict)
			return
		}
	}
	switch body.Action {
	case "acquire", "renew":
		mode := body.Mode
		if mode == "" {
			mode = control.Gated
		}
		if body.Action == "renew" {
			lease, err := s.config.Gate.Validate(credentials)
			if err != nil || lease.Owner != control.Human {
				http.Error(w, "not the current owner", http.StatusConflict)
				return
			}
			mode = lease.Mode
		}
		request := control.Request{Owner: control.Human, Session: viewer.id, Token: credentials.Token, Mode: mode, Duration: time.Duration(body.LeaseMS) * time.Millisecond}
		if mode == control.Native {
			if s.config.NativeViewerID == nil {
				http.Error(w, "native mode unsupported", http.StatusUnprocessableEntity)
				return
			}
			id, err := s.config.NativeViewerID(viewer.id)
			if err != nil || id == "" {
				http.Error(w, "native viewer unavailable", http.StatusConflict)
				return
			}
			request.ViewerID = id
		}
		lease, err := s.config.Gate.Acquire(ctx, request)
		if err != nil {
			controlError(w, err)
			return
		}
		newCredentials := control.Credentials{Session: lease.Session, Token: lease.Token}
		if err := callAcquireHook(s.config.OnAcquire, viewer.id, lease); err != nil {
			cleanupErr := s.releaseExact(newCredentials)
			controlError(w, errors.Join(err, cleanupErr))
			return
		}
		// A backend disconnect may have revoked ownership while the binding hook
		// ran. Do not acknowledge a lease that is already stale.
		if _, err := s.config.Gate.Validate(newCredentials); err != nil {
			cleanupErr := s.releaseExact(newCredentials)
			controlError(w, errors.Join(err, cleanupErr))
			return
		}
		s.mu.Lock()
		dead := viewer.dead.Load()
		// An acquire initiated for a particular stream may not publish after
		// that stream closes or a replacement connects. Even an acquisition
		// begun without a stream must not migrate across stream generations.
		currentStream := viewer.rfb
		disconnected := s.config.RFBTarget != "" && (viewer.rfbDisconnected ||
			viewer.rfbGeneration != streamGeneration ||
			(stream != nil && currentStream != stream) ||
			(currentStream != nil && currentStream.ctx.Err() != nil))
		if !dead && !disconnected {
			viewer.credentials = newCredentials
			if currentStream != nil {
				currentStream.credentials = newCredentials
			}
		}
		s.mu.Unlock()
		if dead || disconnected {
			_ = s.releaseExact(newCredentials)
			if dead {
				http.Error(w, "viewer expired", http.StatusUnauthorized)
			} else {
				http.Error(w, "viewer stream disconnected", http.StatusConflict)
			}
			return
		}
	case "release", "stop":
		lease, err := s.config.Gate.Validate(credentials)
		if err != nil || lease.Owner != control.Human || lease.Session != viewer.id {
			http.Error(w, "not the current owner", http.StatusConflict)
			return
		}
		if err := s.config.Gate.Release(ctx, credentials); err != nil {
			controlError(w, err)
			return
		}
		s.mu.Lock()
		viewer.credentials = control.Credentials{}
		s.mu.Unlock()
	default:
		http.Error(w, "unknown control action", http.StatusBadRequest)
		return
	}
	s.state(w, viewer)
}

func callAcquireHook(hook func(string, control.Lease) error, session string, lease control.Lease) (err error) {
	if hook == nil {
		return nil
	}
	completed := false
	defer func() {
		if !completed {
			_ = recover()
			err = errors.New("desktop acquisition binding failed")
		}
	}()
	err = hook(session, lease)
	completed = true
	return err
}
func (s *Service) releaseExact(credentials control.Credentials) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.config.Gate.Release(ctx, credentials)
	if errors.Is(err, control.ErrStale) || errors.Is(err, control.ErrClosed) {
		return nil
	}
	return err
}

func controlError(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	if errors.Is(err, control.ErrInvalid) {
		status = 400
	}
	if errors.Is(err, control.ErrConflict) || errors.Is(err, control.ErrStale) {
		status = 409
	}
	http.Error(w, "control request rejected", status)
}
func (s *Service) input(w http.ResponseWriter, r *http.Request, viewer *session) {
	credentials, _, epochErr := s.ownedEpoch(r, viewer)
	if epochErr != nil {
		http.Error(w, "stale viewer epoch", http.StatusConflict)
		return
	}
	var body struct {
		Events []desktop.Event `json:"events"`
	}
	if err := decode(w, r, &body); err != nil {
		s.rejectInput(w, r, viewer, credentials)
		return
	}
	width, height := s.config.Driver.Size()
	if err := desktop.ValidateEvents(body.Events, width, height); err != nil {
		s.rejectInput(w, r, viewer, credentials)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	// Check before backend-specific dispatch as well. The override must repeat
	// this at its actual queued/native transport boundary.
	lease, err := s.config.Gate.Validate(credentials)
	if err != nil || lease.Owner != control.Human || lease.Mode != control.Gated {
		http.Error(w, "not the gated owner", http.StatusConflict)
		return
	}
	if s.config.Input != nil {
		err = s.config.Input(ctx, credentials, body.Events)
	} else {
		for _, event := range body.Events {
			err = s.config.Gate.DispatchHumanGated(ctx, credentials, func(ctx context.Context) error {
				return s.config.Driver.Input(ctx, event)
			})
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		controlError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (s *Service) rejectInput(w http.ResponseWriter, r *http.Request, viewer *session, credentials control.Credentials) {
	if lease, err := s.config.Gate.Validate(credentials); err == nil && lease.Owner == control.Human && lease.Mode == control.Gated && lease.Session == viewer.id {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.config.Gate.Release(ctx, credentials); err != nil {
			controlError(w, err)
			return
		}
		s.mu.Lock()
		if viewer.credentials == credentials {
			viewer.credentials = control.Credentials{}
		}
		s.mu.Unlock()
	}
	http.Error(w, "invalid input batch", http.StatusBadRequest)
}
func (s *Service) screenshot(w http.ResponseWriter, r *http.Request) {
	select {
	case s.capture <- struct{}{}:
		defer func() {
			<-s.capture
		}()
	default:
		http.Error(w, "capture busy", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	data, err := s.config.Driver.CapturePNG(ctx)
	if err != nil || len(data) == 0 || len(data) > 16<<20 {
		http.Error(w, "capture unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}
func (s *Service) websocket(w http.ResponseWriter, r *http.Request, viewer *session) {
	if s.config.RFBTarget == "" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	if viewer.websocket || viewer.dead.Load() {
		s.mu.Unlock()
		http.Error(w, "viewer already connected", http.StatusConflict)
		return
	}
	viewer.websocket = true
	ctx, cancel := context.WithCancel(r.Context())
	viewer.cancel = cancel
	stream := &rfbStream{ctx: ctx, credentials: viewer.credentials}
	viewer.rfbGeneration++
	viewer.rfb = stream
	viewer.rfbDisconnected = false
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		// Keep the connection slot reserved until this exact stream's release
		// finishes, so a delayed close cannot revoke a replacement stream.
		viewer.rfbDisconnected = true
		credentials := stream.credentials
		s.mu.Unlock()
		keepCredentials := false
		if credentials.Token != "" {
			if lease, err := s.config.Gate.Validate(credentials); err == nil && lease.Owner == control.Human {
				if lease.Mode == control.Gated {
					_ = s.releaseExact(credentials)
				} else {
					keepCredentials = true
				}
			}
		}
		s.mu.Lock()
		if viewer.rfb == stream {
			if !keepCredentials && viewer.credentials == credentials {
				viewer.credentials = control.Credentials{}
			}
			viewer.rfb = nil
			viewer.websocket = false
			viewer.cancel = nil
		}
		s.mu.Unlock()
	}()
	gateway, err := desktop.NewGateway(desktop.Config{Target: s.config.RFBTarget, Gate: s.config.Gate, AllowedHosts: []string{r.Host}, AllowedOrigins: []string{"http://" + r.Host}, MaxConnections: 1, Authenticate: func(*http.Request) (control.Credentials, error) {
		if viewer.dead.Load() {
			return control.Credentials{}, ErrClosed
		}
		// RFB is read-only. Lease cleanup uses the stream binding above rather
		// than a credential snapshot from before the user pressed Take.
		return control.Credentials{}, nil
	}})
	if err != nil {
		http.Error(w, "desktop unavailable", http.StatusServiceUnavailable)
		return
	}
	gateway.ServeHTTP(w, r.WithContext(ctx))
}

// Close cancels only sessions belonging to this public viewer service. It does
// not globally stop a foreign human or private agent lease.
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	var viewers []*session
	for token, viewer := range s.sessions {
		delete(s.sessions, token)
		viewer.dead.Store(true)
		viewer.closeLifecycle()
		if viewer.cancel != nil {
			viewer.cancel()
		}
		viewers = append(viewers, viewer)
	}
	s.tickets = map[[32]byte]time.Time{}
	s.mu.Unlock()
	var result error
	for _, viewer := range viewers {
		credentials := s.credentials(viewer)
		if lease, err := s.config.Gate.Validate(credentials); err == nil && lease.Owner == control.Human && lease.Session == viewer.id {
			result = errors.Join(result, s.config.Gate.Release(ctx, credentials))
		}
	}
	return result
}

// String deliberately omits sessions, cookies and bootstrap credentials.
func (*Service) String() string {
	return "Sidekicks desktop viewer"
}

// lifecycleWriter owns only a connection hijacked by this authenticated SDK
// request. Cancellation closes the socket even if backend cleanup is delayed.
type lifecycleWriter struct {
	http.ResponseWriter
	ctx       context.Context
	stopClose func() bool
}

// Hijack hijacks the underlying HTTP connection.
func (w *lifecycleWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}
	w.stopClose = context.AfterFunc(w.ctx, func() {
		_ = conn.Close()
	})
	return conn, buffer, nil
}

// Unwrap returns the underlying response writer.
func (w *lifecycleWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
func (w *lifecycleWriter) stop() {
	if w.stopClose != nil {
		w.stopClose()
	}
}
