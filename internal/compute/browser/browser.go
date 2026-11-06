// Package browser controls a dedicated Chromium workload over its private CDP
// endpoint. It must never be configured with a personal browser's endpoint.
package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

const (
	maxDiscovery = 64 << 10
	maxMessage   = 12 << 20
	maxPNG       = 8 << 20
	maxPages     = 8
	callTimeout  = 15 * time.Second
)

var (
	// ErrInvalid indicates an invalid request.
	ErrInvalid = errors.New("invalid browser request")
	// ErrClosed indicates that the resource is closed.
	ErrClosed = errors.New("browser page or client is closed")
	// ErrLimit indicates that a resource limit has been reached.
	ErrLimit = errors.New("browser resource limit reached")
	// ErrCreationUncertain indicates that target creation could not be confirmed.
	// An unknown target may have been created. Only destruction of the dedicated
	// browser workload can reconcile this; never guess which remote page to close.
	ErrCreationUncertain = errors.New("browser target creation unconfirmed; workload teardown required")
	safeTarget           = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

// Client is tied to one trusted, literal-loopback CDP authority. Discovery cannot
// redirect it to another service. It only closes pages that it created itself.
type Client struct {
	endpoint          *url.URL
	http              *http.Client
	mu                sync.Mutex
	pages             map[*Page]struct{}
	pending           int
	closed            bool
	creationUncertain bool
}

// Option configures a client.
type Option func(*http.Client)

// WithHTTPClient supplies a trusted transport. Redirects are still disabled.
func WithHTTPClient(c *http.Client) Option {
	return func(dst *http.Client) {
		if c != nil {
			*dst = *c
		}
	}
}

// New constructs a client for a dedicated loopback Chromium endpoint.
func New(endpoint string, options ...Option) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return nil, ErrInvalid
	}
	ip := net.ParseIP(u.Hostname())
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if ip == nil || !ip.IsLoopback() || err != nil || port == 0 {
		return nil, ErrInvalid
	}
	h := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: callTimeout}
	for _, o := range options {
		if o != nil {
			o(h)
		}
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if h.Timeout <= 0 || h.Timeout > callTimeout {
		h.Timeout = callTimeout
	}
	u.Path = ""
	u.RawPath = ""
	return &Client{endpoint: u, http: h, pages: make(map[*Page]struct{})}, nil
}

type target struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	WebSocketURL string `json:"webSocketDebuggerUrl"`
}

type statusError struct{ status int }

// Error returns the discovery status as an error message.
func (e statusError) Error() string {
	return fmt.Sprintf("cdp discovery returned http %d", e.status)
}

func (c *Client) request(ctx context.Context, method, path string, out any) error {
	u := *c.endpoint
	u.Path = path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return ErrInvalid
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cdp discovery request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError{resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDiscovery+1))
	if err != nil {
		return fmt.Errorf("read cdp discovery: %w", err)
	}
	if len(body) > maxDiscovery {
		return ErrLimit
	}
	if out != nil {
		if err = json.Unmarshal(body, out); err != nil {
			return errors.New("invalid cdp discovery response")
		}
	}
	return nil
}
func (c *Client) validSocket(t target) bool {
	if !safeTarget.MatchString(t.ID) || t.Type != "page" {
		return false
	}
	u, err := url.Parse(t.WebSocketURL)
	return err == nil && u.Scheme == "ws" && u.Host == c.endpoint.Host && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.RawPath == "" && u.Path == "/devtools/page/"+t.ID
}

// NewPage creates an owned blank target; it does not attach to arbitrary targets.
func (c *Client) NewPage(ctx context.Context) (*Page, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	if c.creationUncertain {
		c.mu.Unlock()
		return nil, ErrCreationUncertain
	}
	if len(c.pages)+c.pending >= maxPages {
		c.mu.Unlock()
		return nil, ErrLimit
	}
	c.pending++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.pending--
		c.mu.Unlock()
	}()
	var t target
	if err := c.request(ctx, http.MethodPut, "/json/new", &t); err != nil {
		// A missing or malformed response does not establish whether Chromium
		// created a page. Fence further creation before releasing our pending slot.
		c.mu.Lock()
		c.creationUncertain = true
		c.mu.Unlock()
		return nil, errors.Join(ErrCreationUncertain, err)
	}
	if !safeTarget.MatchString(t.ID) {
		c.mu.Lock()
		c.creationUncertain = true
		c.mu.Unlock()
		return nil, errors.Join(ErrCreationUncertain, errors.New("malformed cdp target id"))
	}
	// Track the creation before validating/dialing its socket. Failed cleanup
	// remains owned and bounded, and a later Client.Close can retry it.
	p := &Page{client: c, id: t.ID, gate: make(chan struct{}, 1), operations: make(chan struct{}, 1), closeGate: make(chan struct{}, 1), closed: true}
	c.mu.Lock()
	c.pages[p] = struct{}{}
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil, errors.Join(ErrClosed, c.cleanupPage(p))
	}
	if !c.validSocket(t) {
		return nil, errors.Join(errors.New("cdp target escaped configured endpoint or was malformed"), c.cleanupPage(p))
	}
	conn, _, err := websocket.Dial(ctx, t.WebSocketURL, &websocket.DialOptions{HTTPClient: c.http, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("connect cdp target: %w", err), c.cleanupPage(p))
	}
	conn.SetReadLimit(maxMessage)
	p.mu.Lock()
	p.conn = conn
	p.closed = false
	p.mu.Unlock()
	c.mu.Lock()
	closed = c.closed
	c.mu.Unlock()
	if closed {
		return nil, errors.Join(ErrClosed, c.cleanupPage(p))
	}
	return p, nil
}
func (c *Client) cleanupPage(p *Page) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return p.Close(ctx)
}

// Close releases every target created by this client, never unrelated tabs.
func (c *Client) Close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		c.mu.Lock()
		pending := c.pending
		c.mu.Unlock()
		if pending == 0 {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.mu.Lock()
	pages := make([]*Page, 0, len(c.pages))
	for p := range c.pages {
		pages = append(pages, p)
	}
	c.mu.Unlock()
	var errs []error
	for _, p := range pages {
		if err := p.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	c.mu.Lock()
	uncertain := c.creationUncertain
	c.mu.Unlock()
	if uncertain {
		errs = append(errs, ErrCreationUncertain)
	}
	return errors.Join(errs...)
}

// Page represents an owned browser page.
type Page struct {
	client     *Client
	id         string
	conn       *websocket.Conn
	gate       chan struct{}
	operations chan struct{}
	mu         sync.Mutex
	closed     bool
	retired    bool
	closeGate  chan struct{}
	seq        uint64
}
type reply struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

// operation keeps multi-command interactions (click and capture) atomic against
// other calls on this Page and bounds the whole operation, including queue wait.
func (p *Page) operation(ctx context.Context) (context.Context, func(), error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	if err := ctx.Err(); err != nil {
		cancel()
		return ctx, func() {
		}, err
	}
	select {
	case p.operations <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-p.operations
			cancel()
			return ctx, func() {
			}, err
		}
		return ctx, func() {
			<-p.operations
			cancel()
		}, nil
	case <-ctx.Done():
		cancel()
		return ctx, func() {

		}, ctx.Err()
	}
}

func (p *Page) call(ctx context.Context, method string, params any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case p.gate <- struct{}{}:
		defer func() {
			<-p.gate
		}()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return ErrClosed
	}
	p.seq++
	b, err := json.Marshal(struct {
		ID     uint64 `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{p.seq, method, params})
	if err != nil {
		return ErrInvalid
	}
	if err = p.conn.Write(ctx, websocket.MessageText, b); err != nil {
		p.fail()
		return fmt.Errorf("send cdp command: %w", err)
	}
	for {
		typ, b, err := p.conn.Read(ctx)
		if err != nil {
			p.fail()
			return fmt.Errorf("receive cdp response: %w", err)
		}
		if typ != websocket.MessageText {
			p.fail()
			return errors.New("invalid cdp response type")
		}
		var r reply
		if json.Unmarshal(b, &r) != nil {
			p.fail()
			return errors.New("invalid cdp response")
		}
		if r.ID == 0 {
			continue
		} // This is an unsolicited CDP event.
		if r.ID != p.seq {
			p.fail()
			return errors.New("mismatched cdp response")
		}
		if r.Error != nil {
			return fmt.Errorf("cdp %s failed (code %d)", method, r.Error.Code)
		}
		if out != nil && json.Unmarshal(r.Result, out) != nil {
			return errors.New("invalid cdp command result")
		}
		return nil
	}
}
func (p *Page) fail() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	_ = p.conn.CloseNow()
}

// Close is idempotent and also retires targets whose connection has failed.
func (p *Page) Close(ctx context.Context) error {
	p.mu.Lock()
	p.closed = true
	conn := p.conn
	p.mu.Unlock()
	if conn != nil {
		_ = conn.CloseNow()
	}
	select {
	case p.closeGate <- struct{}{}:
		defer func() {
			<-p.closeGate
		}()
	case <-ctx.Done():
		return ctx.Err()
	}
	p.mu.Lock()
	retired := p.retired
	p.mu.Unlock()
	if retired {
		return nil
	}
	err := p.client.request(ctx, http.MethodGet, "/json/close/"+p.id, nil)
	var status statusError
	if err != nil && !(errors.As(err, &status) && status.status == http.StatusNotFound) {
		return err
	}
	p.mu.Lock()
	p.retired = true
	p.mu.Unlock()
	p.client.mu.Lock()
	delete(p.client.pages, p)
	p.client.mu.Unlock()
	return nil
}

// Navigate acknowledges navigation; use WaitReady for DOM readiness. It accepts
// HTTP(S) and about:blank only. This is URL curation, not an
// egress firewall: public pages can still request private destinations unless the
// runtime applies an independent network policy.
func (p *Page) Navigate(ctx context.Context, address string) error {
	ctx, done, opErr := p.operation(ctx)
	if opErr != nil {
		return opErr
	}
	defer done()
	return p.navigate(ctx, address)
}

func (p *Page) navigate(ctx context.Context, address string) error {
	if len(address) > 8192 || strings.ContainsAny(address, "\x00\r\n") {
		return ErrInvalid
	}
	u, err := url.Parse(address)
	if err != nil || u.User != nil || ((u.Scheme != "http" && u.Scheme != "https") && address != "about:blank") || (address != "about:blank" && u.Hostname() == "") {
		return ErrInvalid
	}
	var r struct {
		ErrorText string `json:"errorText"`
	}
	if err = p.call(ctx, "Page.navigate", map[string]any{"url": address}, &r); err != nil {
		return err
	}
	if r.ErrorText != "" {
		return errors.New("browser navigation failed")
	}
	return nil
}
func (p *Page) text(ctx context.Context, expression string) (string, error) {
	var r struct {
		Result struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err := p.call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": false}, &r); err != nil {
		return "", err
	}
	if len(r.Exception) > 0 || r.Result.Type != "string" {
		return "", errors.New("browser text query failed")
	}
	if len(r.Result.Value) > 1<<20 {
		return "", ErrLimit
	}
	return r.Result.Value, nil
}

// WaitReady waits for the current document to become interactive or complete.
// This is DOM readiness, not proof that arbitrary application work has settled.
func (p *Page) WaitReady(ctx context.Context) error {
	ctx, done, opErr := p.operation(ctx)
	if opErr != nil {
		return opErr
	}
	defer done()
	return p.waitReady(ctx)
}

// NavigateAndWaitReady keeps the navigation and readiness check in one page
// operation. Page scripts can still navigate independently of this client.
func (p *Page) NavigateAndWaitReady(ctx context.Context, address string) error {
	ctx, done, err := p.operation(ctx)
	if err != nil {
		return err
	}
	defer done()
	if err := p.navigate(ctx, address); err != nil {
		return err
	}
	return p.waitReady(ctx)
}

func (p *Page) waitReady(ctx context.Context) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		value, err := p.text(ctx, "String(document.readyState)")
		if err != nil {
			return err
		}
		if value == "interactive" || value == "complete" {
			return nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Title returns the current page title.
func (p *Page) Title(ctx context.Context) (string, error) {
	ctx, done, opErr := p.operation(ctx)
	if opErr != nil {
		return "", opErr
	}
	defer done()
	return p.text(ctx, "String(document.title).slice(0,1048576)")
}

// Text returns the text of one CSS selector, not arbitrary JavaScript evaluation.
func (p *Page) Text(ctx context.Context, selector string) (string, error) {
	ctx, done, opErr := p.operation(ctx)
	if opErr != nil {
		return "", opErr
	}
	defer done()
	if selector == "" || len(selector) > 1024 || strings.ContainsRune(selector, 0) {
		return "", ErrInvalid
	}
	q, _ := json.Marshal(selector)
	return p.text(ctx, "String(document.querySelector("+string(q)+")?.textContent ?? '').slice(0,1048576)")
}
func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 4096
}

// Click sends a complete primary-button pair. Even failed press acknowledgments
// trigger a best-effort release, because the browser may have applied the press.
func (p *Page) Click(ctx context.Context, x, y float64) error {
	ctx, done, opErr := p.operation(ctx)
	if opErr != nil {
		return opErr
	}
	defer done()
	if !finite(x) || !finite(y) {
		return ErrInvalid
	}
	params := map[string]any{"type": "mousePressed", "x": x, "y": y, "button": "left", "buttons": 1, "clickCount": 1}
	err := p.call(ctx, "Input.dispatchMouseEvent", params, nil)
	releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	params["type"] = "mouseReleased"
	params["buttons"] = 0
	releaseErr := p.call(releaseCtx, "Input.dispatchMouseEvent", params, nil)
	if releaseErr != nil {
		// A disconnected CDP session cannot acknowledge a release. Retire this
		// owned page rather than leave it available for subsequent automation.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		cleanupErr := p.Close(cleanupCtx)
		cleanupCancel()
		return errors.Join(err, releaseErr, cleanupErr)
	}
	return err
}

// Type inserts Unicode text without holding modifier keys.
func (p *Page) Type(ctx context.Context, text string) error {
	ctx, done, opErr := p.operation(ctx)
	if opErr != nil {
		return opErr
	}
	defer done()
	if len(text) > 65536 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return ErrInvalid
	}
	return p.call(ctx, "Input.insertText", map[string]any{"text": text}, nil)
}

// ScreenshotOptions sets screenshot dimensions.
type ScreenshotOptions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ScreenshotPNG captures a bounded viewport; it never requests an unbounded full
// document. The PNG header is checked independently of the CDP response.
func (p *Page) ScreenshotPNG(ctx context.Context, o ScreenshotOptions) ([]byte, error) {
	ctx, done, opErr := p.operation(ctx)
	if opErr != nil {
		return nil, opErr
	}
	defer done()
	if o.Width == 0 {
		o.Width = 1280
	}
	if o.Height == 0 {
		o.Height = 720
	}
	if o.Width < 1 || o.Width > 1920 || o.Height < 1 || o.Height > 1080 {
		return nil, ErrInvalid
	}
	if err := p.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": o.Width, "height": o.Height, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		return nil, err
	}
	var r struct {
		Data string `json:"data"`
	}
	if err := p.call(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "fromSurface": true, "captureBeyondViewport": false, "clip": map[string]any{"x": 0, "y": 0, "width": o.Width, "height": o.Height, "scale": 1}}, &r); err != nil {
		return nil, err
	}
	if base64.StdEncoding.DecodedLen(len(r.Data)) > maxPNG {
		return nil, ErrLimit
	}
	b, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		return nil, errors.New("invalid screenshot encoding")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width != o.Width || cfg.Height != o.Height {
		return nil, errors.New("invalid screenshot png dimensions")
	}
	if _, err := png.Decode(bytes.NewReader(b)); err != nil {
		return nil, errors.New("invalid screenshot png data")
	}
	return b, nil
}
