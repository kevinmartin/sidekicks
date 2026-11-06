package neko

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/compute/control"
)

// HandlerOptions configures the private machine API. The public viewer wrapper
// must independently authenticate browser cookies and validate exact Host/Origin
// before proxying to this handler; never make this an unauthenticated LAN API.
type HandlerOptions struct {
	Token string
	// Deprecated: native input is unsupported for the pinned upstream server.
	// Setting this true fails construction instead of silently enabling it.
	EnableNative bool
}
type viewerTicket struct {
	Viewer  Viewer
	Expires time.Time
	Lease   control.Credentials
}

// Handler serves the Neko capability API.
type Handler struct {
	client  *Client
	gate    *control.Manager
	token   string
	native  bool
	mu      sync.Mutex
	tickets map[string]viewerTicket
	issued  control.Credentials
}

// NewHandler constructs a Neko capability handler.
func NewHandler(client *Client, gate *control.Manager, options HandlerOptions) (*Handler, error) {
	if options.EnableNative {
		return nil, ErrNativeInputUnsupported
	}
	if client == nil || gate == nil || len(options.Token) < 32 || len(options.Token) > 256 || strings.ContainsAny(options.Token, " \t\r\n") {
		return nil, errors.New("neko: private handler requires client, manager and strong instance token")
	}
	h := &Handler{client: client, gate: gate, token: options.Token, native: options.EnableNative, tickets: map[string]viewerTicket{}}
	client.mu.Lock()
	previous := client.onDisconnect
	client.onDisconnect = func() {
		h.mu.Lock()
		h.issued = control.Credentials{}
		h.tickets = map[string]viewerTicket{}
		h.mu.Unlock()
		// Stop revokes every manager lease and cancels in-flight dispatch before
		// waiting for reset. The controller is already unavailable and reconnect
		// remains fenced by Client.done until this callback returns. A timed-out
		// reset is retried only by Manager's generation-guarded cleanup; never
		// schedule a later global Stop which could cancel a newer owner.
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		_ = gate.Stop(ctx)
		cancel()
		if previous != nil {
			previous()
		}
	}
	client.mu.Unlock()
	return h, nil
}

// ServeHTTP serves the HTTP request.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("Origin") != "" {
		handlerError(w, http.StatusForbidden, "browser origin must be validated by the public viewer wrapper")
		return
	}
	if r.URL.RawPath != "" || strings.Contains(r.URL.Path, "//") || strings.Contains(r.URL.Path, "/../") {
		handlerError(w, 400, "invalid path")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/desktop/stream/api/") {
		h.sdk(w, r)
		return
	}
	if !validBearer(r.Header.Get("Authorization"), h.token) {
		handlerError(w, 401, "instance authentication required")
		return
	}
	switch r.URL.Path {
	case "/desktop/health":
		if !handlerMethod(w, r, http.MethodGet) {
			return
		}
		ready := h.client.Ready() && h.gate.View().Ready
		status := 200
		if !ready {
			status = 503
		}
		handlerJSON(w, status, map[string]any{"ready": ready, "adapter": "desktop-neko"})
	case "/desktop/capabilities":
		if !handlerMethod(w, r, http.MethodGet) {
			return
		}
		width, height := h.client.Size()
		handlerJSON(w, 200, map[string]any{"adapter": "desktop-neko", "desktop": true, "humanControl": true, "nativeControl": h.native, "upstreamInput": false, "inputTransport": "external synchronized Go X11 driver", "width": width, "height": height, "injectionBoundary": "synchronized Go X11; no application queue flush"})
	case "/desktop/lease":
		h.lease(w, r)
	case "/desktop/input":
		if !handlerMethod(w, r, http.MethodPost) {
			return
		}
		handlerError(w, http.StatusNotImplemented, "neko upstream input is disabled; use the synchronized compute desktop input api")
	case "/desktop/capture":
		if !handlerMethod(w, r, http.MethodGet) {
			return
		}
		width, height := 1280, 720
		var err error
		if v := r.URL.Query().Get("maxWidth"); v != "" {
			width, err = strconv.Atoi(v)
		}
		if err != nil {
			handlerError(w, 400, "invalid capture width")
			return
		}
		if v := r.URL.Query().Get("maxHeight"); v != "" {
			height, err = strconv.Atoi(v)
		}
		if err != nil {
			handlerError(w, 400, "invalid capture height")
			return
		}
		shot, err := h.client.Capture(r.Context(), width, height)
		if err != nil {
			handlerError(w, 503, "capture unavailable or invalid bounds")
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("X-Image-Width", strconv.Itoa(shot.Width))
		w.Header().Set("X-Image-Height", strconv.Itoa(shot.Height))
		w.Header().Set("X-Source-Width", strconv.Itoa(shot.SourceWidth))
		w.Header().Set("X-Source-Height", strconv.Itoa(shot.SourceHeight))
		w.WriteHeader(200)
		_, _ = w.Write(shot.JPEG)
	case "/desktop/media-ports":
		if !handlerMethod(w, r, http.MethodPost) {
			return
		}
		var body struct {
			Ports []struct {
				Guest uint16 `json:"guest"`
				Host  uint16 `json:"host"`
			} `json:"ports"`
		}
		if !handlerBody(w, r, &body) {
			return
		}
		ports := make(map[uint16]uint16, len(body.Ports))
		for _, p := range body.Ports {
			if _, exists := ports[p.Guest]; exists {
				handlerError(w, 400, "duplicate guest media port")
				return
			}
			ports[p.Guest] = p.Host
		}
		if err := h.SetMediaPorts(ports); err != nil {
			handlerError(w, 409, "invalid or active media allocation")
			return
		}
		handlerJSON(w, 200, map[string]bool{"ok": true})
	case "/desktop/viewer":
		h.viewer(w, r)
	default:
		handlerError(w, 404, "not found")
	}
}
func validBearer(header, token string) bool {
	return strings.HasPrefix(header, "Bearer ") && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(header, "Bearer ")), []byte(token)) == 1
}
func (h *Handler) lease(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		handlerJSON(w, 200, h.gate.View())
		return
	}
	if !handlerMethod(w, r, http.MethodPost) {
		return
	}
	var b struct {
		Action  string        `json:"action"`
		Owner   control.Owner `json:"owner"`
		Session string        `json:"session"`
		Token   string        `json:"token"`
		Mode    control.Mode  `json:"mode"`
		LeaseMS int           `json:"leaseMs"`
	}
	if !handlerBody(w, r, &b) {
		return
	}
	creds := control.Credentials{Session: b.Session, Token: b.Token}
	switch b.Action {
	case "acquire":
		if !h.client.Ready() {
			handlerError(w, 503, "controller disconnected")
			return
		}
		if b.LeaseMS == 0 {
			b.LeaseMS = 30000
		}
		if b.LeaseMS < 100 || b.LeaseMS > 60000 {
			handlerError(w, 400, "lease duration must be 100..60000 milliseconds")
			return
		}
		id := ""
		var err error
		if b.Mode == control.Native {
			if !h.native {
				handlerError(w, 422, "native control was not enabled by the launcher")
				return
			}
			id, err = h.client.ViewerID(b.Session)
			if err != nil {
				handlerError(w, 409, "connected viewer required")
				return
			}
		}
		lease, err := h.gate.Acquire(r.Context(), control.Request{Owner: b.Owner, Session: b.Session, Token: b.Token, Mode: b.Mode, ViewerID: id, Duration: time.Duration(b.LeaseMS) * time.Millisecond})
		if err != nil {
			handlerControlError(w, err)
			return
		}
		creds = control.Credentials{Session: b.Session, Token: lease.Token}
		if b.Mode == control.Native {
			if current, err := h.client.ViewerID(b.Session); err != nil || current != id {
				_ = h.gate.Release(context.WithoutCancel(r.Context()), creds)
				handlerError(w, 409, "viewer disconnected during acquisition")
				return
			}
		}
		h.mu.Lock()
		hasViewer := false
		for _, ticket := range h.tickets {
			if ticket.Viewer.Session == b.Session {
				hasViewer = true
				break
			}
		}
		h.mu.Unlock()
		if lease.Owner == control.Human && (hasViewer || lease.Mode == control.Native) {
			err = h.BindLease(b.Session, lease)
		} else {
			err = h.recordMachineLease(lease)
		}
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			_ = h.gate.Release(cleanupCtx, creds)
			cancel()
			handlerError(w, 409, "lease was replaced or its viewer disconnected before binding")
			return
		}
		handlerJSON(w, 200, lease)
	case "release":
		if err := h.gate.Release(r.Context(), creds); err != nil {
			handlerControlError(w, err)
			return
		}
		handlerJSON(w, 200, map[string]bool{"ok": true})
	case "stop":
		if err := h.gate.Stop(r.Context()); err != nil {
			handlerControlError(w, err)
			return
		}
		handlerJSON(w, 200, map[string]bool{"ok": true})
	default:
		handlerError(w, 400, "unknown lease action")
	}
}
func (h *Handler) viewer(w http.ResponseWriter, r *http.Request) {
	if !handlerMethod(w, r, http.MethodPost) {
		return
	}
	var b struct {
		Session string `json:"session"`
	}
	if !handlerBody(w, r, &b) {
		return
	}
	ticket, err := h.CreateSDKViewer(r.Context(), b.Session)
	if err != nil {
		handlerError(w, 409, "viewer unavailable")
		return
	}
	handlerJSON(w, 200, ticket)
}

// SDKViewerTicket is an opaque, limited gateway capability. It never contains
// the upstream Neko credential.
type SDKViewerTicket struct {
	Session string `json:"session"`
	ID      string `json:"id"`
	Token   string `json:"token"`
}

// CreateSDKViewer serves trusted callers which already authenticated their own
// browser session, without routing through an HTTP response recorder.
func (h *Handler) CreateSDKViewer(ctx context.Context, session string) (SDKViewerTicket, error) {
	h.expireTickets()
	viewer, err := h.client.CreateViewer(ctx, session)
	if err != nil {
		return SDKViewerTicket{}, err
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		h.client.removeViewer(viewer)
		return SDKViewerTicket{}, errors.New("neko: sdk ticket unavailable")
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	h.mu.Lock()
	h.tickets[token] = viewerTicket{Viewer: viewer, Expires: time.Now().Add(2 * time.Minute)}
	h.mu.Unlock()
	return SDKViewerTicket{Session: session, ID: viewer.ID, Token: token}, nil
}

// TicketBelongs binds SDK REST/signaling to an already authenticated public
// cookie session. Check it before forwarding a browser request to this handler.
func (h *Handler) TicketBelongs(token, session string) bool {
	if len(token) != 43 || !validSession(session) {
		return false
	}
	h.mu.Lock()
	ticket, ok := h.tickets[token]
	h.mu.Unlock()
	if !ok || ticket.Viewer.Session != session || !time.Now().Before(ticket.Expires) {
		return false
	}
	h.client.mu.Lock()
	current, exists := h.client.viewers[session]
	ready := h.client.ready
	h.client.mu.Unlock()
	return ready && exists && current.reservation == ticket.Viewer.reservation
}

func (h *Handler) expireTickets() {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	for token, ticket := range h.tickets {
		if now.Before(ticket.Expires) {
			continue
		}
		h.client.mu.Lock()
		v, ok := h.client.viewers[ticket.Viewer.Session]
		active := ok && v.reservation == ticket.Viewer.reservation && (v.connected || v.connecting)
		h.client.mu.Unlock()
		if !active {
			delete(h.tickets, token)
			h.client.removeViewer(ticket.Viewer)
		}
	}
}
func (c *Client) removeViewer(viewer Viewer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if current, ok := c.viewers[viewer.Session]; ok && current.reservation == viewer.reservation && !current.connected && !current.connecting {
		delete(c.viewers, viewer.Session)
	}
}
func (h *Handler) sdk(w http.ResponseWriter, r *http.Request) {
	if !handlerMethod(w, r, http.MethodGet) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/desktop/stream")
	if path != "/api/ws" && path != "/api/whoami" && path != "/api/room/screen" && path != "/api/room/screen/configurations" {
		handlerError(w, 403, "sdk route denied")
		return
	}
	token := ""
	if path == "/api/ws" {
		token = r.URL.Query().Get("token")
	} else {
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
	}
	h.mu.Lock()
	ticket, ok := h.tickets[token]
	h.mu.Unlock()
	h.client.mu.Lock()
	currentViewer, viewerExists := h.client.viewers[ticket.Viewer.Session]
	h.client.mu.Unlock()
	if !ok || len(token) != 43 || !viewerExists || currentViewer.reservation != ticket.Viewer.reservation {
		handlerError(w, 401, "viewer authentication required")
		return
	}
	if time.Now().After(ticket.Expires) {
		handlerError(w, 401, "viewer ticket expired")
		return
	}
	if path != "/api/ws" {
		data, err := h.client.ViewerREST(r.Context(), ticket.Viewer.Session, path)
		if err != nil {
			handlerError(w, 503, "viewer metadata unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write(data)
		return
	}
	if time.Now().After(ticket.Expires) {
		handlerError(w, 401, "viewer ticket expired")
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	_ = h.client.RelaySignaling(r.Context(), ticket.Viewer.Session, ws, func(_ string) {
		h.mu.Lock()
		current, exists := h.tickets[token]
		if exists {
			delete(h.tickets, token)
		}
		h.mu.Unlock()
		// Exact issued credentials make a late old-viewer callback unable to revoke
		// a newer human's lease, even when the textual session name is reused.
		if exists && current.Lease.Token != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = h.gate.Release(ctx, current.Lease)
		}
	})
}
func handlerMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		w.Header().Set("Allow", method)
		handlerError(w, 405, "method not allowed")
		return false
	}
	return true
}
func handlerBody(w http.ResponseWriter, r *http.Request, v any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		handlerError(w, 415, "application/json required")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	decoder.DisallowUnknownFields()
	if decoder.Decode(v) != nil {
		handlerError(w, 400, "invalid or oversized json")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		handlerError(w, 400, "one json value required")
		return false
	}
	return true
}
func handlerControlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, control.ErrInvalid):
		handlerError(w, 400, "invalid lease or input")
	case errors.Is(err, control.ErrStale), errors.Is(err, control.ErrConflict):
		handlerError(w, 409, "stale lease or ownership conflict")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		handlerError(w, 408, "operation canceled")
	default:
		handlerError(w, 503, "desktop control unavailable")
	}
}
func handlerError(w http.ResponseWriter, status int, message string) {
	handlerJSON(w, status, map[string]string{"error": message})
}
func handlerJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// BindLease connects a trusted public viewer acquisition to its SDK connection.
// Call it before acknowledging control acquisition. On error the caller must
// compensate by releasing only this exact lease's credentials. It never accepts
// a caller-selected upstream identity or revokes another session on failure.
func (h *Handler) BindLease(session string, lease control.Lease) error {
	if !validSession(session) || session != lease.Session || lease.Owner != control.Human || (lease.Mode != control.Gated && lease.Mode != control.Native) {
		return errors.New("neko: a matching human viewer lease is required")
	}
	if lease.Mode == control.Native && !h.native {
		return errors.New("neko: native control is disabled")
	}
	creds := control.Credentials{Session: session, Token: lease.Token}
	// Keep the established h.mu -> client.mu order used by ticket expiration.
	// Viewer disconnect removes its client reservation before invoking h cleanup;
	// it therefore either observes these bound credentials or makes binding fail.
	h.mu.Lock()
	defer h.mu.Unlock()
	h.client.mu.Lock()
	defer h.client.mu.Unlock()
	viewer, exists := h.client.viewers[session]
	if !exists || !h.client.ready || !viewer.connected || (lease.Mode == control.Native && viewer.ID != lease.ViewerID) {
		return errors.New("neko: the matching viewer is no longer connected")
	}
	var ticketKey string
	for key, ticket := range h.tickets {
		if time.Now().Before(ticket.Expires) && ticket.Viewer.Session == session && ticket.Viewer.reservation == viewer.reservation && ticket.Viewer.ID == viewer.ID {
			ticketKey = key
			break
		}
	}
	if ticketKey == "" {
		return errors.New("neko: the matching sdk ticket is unavailable")
	}
	current, err := h.gate.Validate(creds)
	if err != nil || current.Generation != lease.Generation || current.Owner != control.Human || current.Mode != lease.Mode || current.ViewerID != lease.ViewerID {
		return errors.New("neko: the human lease was replaced before binding")
	}
	ticket := h.tickets[ticketKey]
	ticket.Lease = creds
	h.tickets[ticketKey] = ticket
	h.issued = creds
	return nil
}

// recordMachineLease binds private normalized clients which have no SDK viewer.
func (h *Handler) recordMachineLease(lease control.Lease) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.client.mu.Lock()
	defer h.client.mu.Unlock()
	creds := control.Credentials{Session: lease.Session, Token: lease.Token}
	current, err := h.gate.Validate(creds)
	if err != nil || !h.client.ready || current.Generation != lease.Generation {
		return errors.New("neko: machine lease changed before binding")
	}
	h.issued = creds
	return nil
}
