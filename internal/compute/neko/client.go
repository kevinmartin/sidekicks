// Package neko implements the Sidekicks-owned control boundary around an upstream
// Neko v3 server. It does not implement Neko, WebRTC, or its browser SDK.
package neko

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const maxMessage = 1 << 20

// Config addresses a private guest-loopback Neko server. Credentials are supplied
// by the instance launcher; no default passwords are compiled into this package.
type Config struct {
	Endpoint       string
	AdminUsername  string
	AdminPassword  string
	ViewerPassword string
}

// Option configures a client.
type Option func(*Client)

// WithHTTPClient supplies the client used for HTTP requests.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		c.http = h
	}
}

// WithOnDisconnect revokes caller-owned lease state after the controller or any
// required upstream control connection is lost. The callback must not block.
func WithOnDisconnect(fn func()) Option {
	return func(c *Client) {
		c.onDisconnect = fn
	}
}

// Client manages a connection to its configured backend.
type Client struct {
	cfg           Config
	base          *url.URL
	http          *http.Client
	mu            sync.Mutex
	connectMu     sync.Mutex
	ws            *websocket.Conn
	token         string
	ready         bool
	width, height int
	viewers       map[string]Viewer
	viewerCancels map[string]context.CancelFunc
	nextViewer    uint64
	mediaPorts    map[uint16]uint16
	onDisconnect  func()
	sentFrames    uint64
	done          chan struct{}
	cancel        context.CancelFunc
	disconnectErr error
}

type message struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// New validates a literal loopback endpoint. This endpoint must never come from
// an untrusted viewer request or discovery response.
func New(cfg Config, options ...Option) (*Client, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("neko: endpoint must be a guest-loopback http origin")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "0" {
		return nil, errors.New("neko: endpoint must use a literal loopback address")
	}
	if cfg.AdminUsername == "" || len(cfg.AdminUsername) > 128 || cfg.AdminPassword == "" || len(cfg.AdminPassword) > 4096 || cfg.ViewerPassword == "" || len(cfg.ViewerPassword) > 4096 {
		return nil, errors.New("neko: explicit bounded credentials required")
	}
	u.Path = ""
	c := &Client{cfg: cfg, base: u, http: &http.Client{Timeout: 4 * time.Second}, width: 1280, height: 720, viewers: map[string]Viewer{}, viewerCancels: map[string]context.CancelFunc{}}
	for _, option := range options {
		if option != nil {
			option(c)
		}
	}
	if c.http == nil {
		return nil, errors.New("neko: nil http client")
	}
	// Redirects must never carry the bearer credential to another origin.
	clone := *c.http
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	c.http = &clone
	return c, nil
}

// SentFrames counts successful controller control/* WebSocket writes. It is not
// an acknowledgement that a guest application has rendered or consumed input.
func (c *Client) SentFrames() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sentFrames
}

// Ready reports whether the client is ready.
func (c *Client) Ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready
}

// Size returns the current display dimensions.
func (c *Client) Size() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.width, c.height
}

func (c *Client) request(ctx context.Context, method, path, token string, body any) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, errors.New("neko: invalid request")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("neko: invalid request")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("neko: upstream request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("neko: upstream status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxMessage+1))
	if err != nil || len(b) > maxMessage {
		return nil, errors.New("neko: upstream response exceeds limit or is unreadable")
	}
	return b, nil
}

type memberProfile struct {
	IsAdmin            bool `json:"is_admin"`
	CanLogin           bool `json:"can_login"`
	CanConnect         bool `json:"can_connect"`
	CanWatch           bool `json:"can_watch"`
	CanHost            bool `json:"can_host"`
	CanShareMedia      bool `json:"can_share_media"`
	CanAccessClipboard bool `json:"can_access_clipboard"`
}

type upstreamSettings struct {
	LockedControls  bool `json:"locked_controls"`
	ImplicitHosting bool `json:"implicit_hosting"`
}

func (s upstreamSettings) safe() bool {
	return s.LockedControls && !s.ImplicitHosting
}

type login struct {
	ID      string        `json:"id"`
	Token   string        `json:"token"`
	Profile memberProfile `json:"profile"`
}

func (c *Client) login(ctx context.Context, username, password string) (login, error) {
	b, err := c.request(ctx, http.MethodPost, "/api/login", "", map[string]string{"username": username, "password": password})
	if err != nil {
		return login{}, err
	}
	var result login
	if json.Unmarshal(b, &result) != nil || len(result.Token) < 1 || len(result.Token) > 4096 || len(result.ID) > 128 {
		return login{}, errors.New("neko: invalid login response")
	}
	return result, nil
}

// Connect waits for system/init and a successful upstream reset/take before
// publishing readiness. On disconnect Ready becomes false; callers must revoke
// their leases and explicitly reconnect. No lease survives reconnection.
func (c *Client) Connect(ctx context.Context) error {
	c.connectMu.Lock()
	defer c.connectMu.Unlock()
	c.mu.Lock()
	if c.done != nil {
		select {
		case <-c.done:
		default:
			c.mu.Unlock()
			return errors.New("neko: prior connection cleanup is pending")
		}
	}
	if c.ws != nil {
		c.mu.Unlock()
		return errors.New("neko: already connected")
	}
	c.mu.Unlock()
	l, err := c.login(ctx, c.cfg.AdminUsername, c.cfg.AdminPassword)
	if err != nil {
		return err
	}
	if !l.Profile.IsAdmin {
		return errors.New("neko: controller administrator role required")
	}
	u := *c.base
	u.Scheme = "ws"
	u.Path = "/api/ws"
	u.RawQuery = url.Values{"token": {l.Token}}.Encode()
	ws, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: c.http, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return errors.New("neko: controller websocket connection failed")
	}
	ws.SetReadLimit(maxMessage)
	initCtx, cancelInit := context.WithTimeout(ctx, 4*time.Second)
	defer cancelInit()
	typ, data, err := ws.Read(initCtx)
	if err != nil || typ != websocket.MessageText {
		ws.CloseNow()
		return errors.New("neko: controller initialization failed")
	}
	var m message
	if json.Unmarshal(data, &m) != nil || m.Event != "system/init" {
		ws.CloseNow()
		return errors.New("neko: expected system/init")
	}
	var payload struct {
		Settings   upstreamSettings `json:"settings"`
		ScreenSize struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"screen_size"`
	}
	if json.Unmarshal(m.Payload, &payload) != nil || !validSize(payload.ScreenSize.Width, payload.ScreenSize.Height) || !payload.Settings.safe() {
		ws.CloseNow()
		return errors.New("neko: invalid initialization payload")
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	if c.ws != nil {
		c.mu.Unlock()
		cancel()
		ws.CloseNow()
		return errors.New("neko: concurrent connection")
	}
	c.ws = ws
	c.token = l.Token
	c.cancel = cancel
	c.done = make(chan struct{})
	c.ready = false
	c.disconnectErr = nil
	c.width = payload.ScreenSize.Width
	c.height = payload.ScreenSize.Height
	done := c.done
	c.mu.Unlock()
	if err = c.resetWithToken(ctx, l.Token); err != nil {
		cancel()
		ws.CloseNow()
		c.mu.Lock()
		c.ws = nil
		c.token = ""
		c.ready = false
		close(done)
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()
	go c.readLoop(runCtx, ws, done)
	return nil
}
func validSize(w, h int) bool {
	return w >= 1 && h >= 1 && w <= 4096 && h <= 4096
}
func (c *Client) readLoop(ctx context.Context, ws *websocket.Conn, done chan struct{}) {
	defer func() {
		ws.CloseNow()
		c.mu.Lock()
		var cancels []context.CancelFunc
		var token string
		if c.ws == ws {
			c.ready = false
			c.ws = nil
			token = c.token
			c.token = ""
			for _, cancel := range c.viewerCancels {
				cancels = append(cancels, cancel)
			}
			c.viewerCancels = map[string]context.CancelFunc{}
			c.viewers = map[string]Viewer{}
		}
		callback := c.onDisconnect
		c.mu.Unlock()
		for _, cancel := range cancels {
			cancel()
		}
		if callback != nil {
			callback()
		}
		// Best effort backend reset supplements terminating every native viewer.
		// Its failure is not converted into a successful readiness acknowledgement.
		if token != "" {
			resetCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			_, err := c.request(resetCtx, http.MethodPost, "/api/room/control/reset", token, nil)
			cancel()
			c.mu.Lock()
			if c.done == done {
				c.disconnectErr = err
			}
			c.mu.Unlock()
		}
		close(done)
	}()
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			return
		}
		var m message
		if json.Unmarshal(data, &m) != nil {
			return
		}
		switch m.Event {
		case "system/settings":
			var update struct {
				Settings upstreamSettings `json:"settings"`
			}
			if json.Unmarshal(m.Payload, &update) != nil || !update.Settings.safe() {
				return
			}
		case "system/heartbeat":
			if c.send(ctx, "client/heartbeat", nil) != nil {
				return
			}
		case "screen/updated":
			var size struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			}
			if json.Unmarshal(m.Payload, &size) != nil || !validSize(size.Width, size.Height) {
				return
			}
			c.mu.Lock()
			c.width = size.Width
			c.height = size.Height
			c.mu.Unlock()
		}
	}
}
func (c *Client) send(ctx context.Context, event string, payload any) error {
	if event != "client/heartbeat" {
		return ErrUpstreamInputUnsupported
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	ws, ready := c.ws, c.ready
	c.mu.Unlock()
	if !ready || ws == nil {
		return errors.New("neko: controller disconnected")
	}
	data, err := json.Marshal(struct {
		Event   string `json:"event"`
		Payload any    `json:"payload,omitempty"`
	}{event, payload})
	if err != nil {
		return errors.New("neko: invalid control payload")
	}
	bounded, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if err = ws.Write(bounded, websocket.MessageText, data); err != nil {
		return errors.New("neko: control write failed")
	}
	if strings.HasPrefix(event, "control/") {
		c.mu.Lock()
		c.sentFrames++
		c.mu.Unlock()
	}
	return nil
}
func (c *Client) authenticated(ctx context.Context, method, path string, body any) ([]byte, error) {
	c.mu.Lock()
	token, ready := c.token, c.ready
	c.mu.Unlock()
	if token == "" || !ready {
		return nil, errors.New("neko: controller disconnected")
	}
	return c.request(ctx, method, path, token, body)
}

// Reset is the lease driver's fail-closed held-input reset, followed by returning
// upstream ownership to the trusted controller. It must succeed before a new
// lease is published by control.Manager.
func (c *Client) Reset(ctx context.Context) error {
	c.mu.Lock()
	token, ready := c.token, c.ready
	c.mu.Unlock()
	if token == "" || !ready {
		return errors.New("neko: controller disconnected")
	}
	return c.resetWithToken(ctx, token)
}

func (c *Client) resetWithToken(ctx context.Context, token string) error {
	if _, err := c.request(ctx, http.MethodPost, "/api/room/control/reset", token, nil); err != nil {
		return err
	}
	if _, err := c.request(ctx, http.MethodPost, "/api/room/control/take", token, nil); err != nil {
		return err
	}
	return nil
}

// Give is deliberately unsupported for pinned upstream Neko. Its native input
// authorization snapshot is not atomic with reset/ownership changes.
func (c *Client) Give(_ context.Context, _ string) error {
	return ErrNativeInputUnsupported
}

func safeID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

// Close releases backend ownership before closing the controller. A returned
// reset failure means cleanup is unconfirmed; callers must not report success.
func (c *Client) Close(ctx context.Context) error {
	c.connectMu.Lock()
	defer c.connectMu.Unlock()
	c.mu.Lock()
	ws, cancel, done := c.ws, c.cancel, c.done
	c.mu.Unlock()
	if ws == nil {
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.disconnectErr
	}
	err := c.Reset(ctx)
	if cancel != nil {
		cancel()
	}
	ws.CloseNow()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			if err == nil {
				err = ctx.Err()
			}
		}
	}
	c.mu.Lock()
	disconnectErr := c.disconnectErr
	c.mu.Unlock()
	return errors.Join(err, disconnectErr)
}

func validSession(session string) bool {
	if len(session) < 1 || len(session) > 128 {
		return false
	}
	for _, ch := range session {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("_.-", ch)) {
			return false
		}
	}
	return true
}
