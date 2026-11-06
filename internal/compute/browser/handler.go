package browser

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"
)

// NewHandler serves the private machine API at POST /browser. The caller MUST
// wrap it in guest authentication; it is never a public CDP or browser proxy.
// Nonempty browser Origin headers are rejected, even when an outer layer allows
// an origin. Returned page IDs name only targets created through this handler.
func NewHandler(client *Client) http.Handler {
	return &handler{client: client, pages: make(map[string]*Page), unclaimed: make(map[string]*unclaimedPage)}
}

const (
	maxHandlerPNG      = 1 << 20
	maxHandlerResponse = 1400 << 10
	unclaimedPageTTL   = 2 * time.Minute
)

var errHandlerScreenshotTooLarge = errors.New("screenshot exceeds private api response bound")

type handler struct {
	client    *Client
	mu        sync.Mutex
	pages     map[string]*Page
	unclaimed map[string]*unclaimedPage
}

type unclaimedPage struct {
	page  *Page
	timer *time.Timer
}
type request struct {
	Action   string  `json:"action"`
	PageID   string  `json:"pageId,omitempty"`
	URL      string  `json:"url,omitempty"`
	Selector string  `json:"selector,omitempty"`
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
	Text     string  `json:"text,omitempty"`
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
}

// ServeHTTP serves the HTTP request.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path != "/browser" || r.URL.RawQuery != "" {
		h.error(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.error(w, http.StatusMethodNotAllowed, "post required")
		return
	}
	for _, origin := range r.Header.Values("Origin") {
		if origin != "" {
			h.error(w, http.StatusForbidden, "machine api does not accept browser origins")
			return
		}
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		h.error(w, http.StatusUnsupportedMediaType, "application/json required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req request
	if err = decoder.Decode(&req); err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			h.error(w, http.StatusRequestEntityTooLarge, "request too large")
		} else {
			h.error(w, http.StatusBadRequest, "invalid browser request")
		}
		return
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			h.error(w, http.StatusRequestEntityTooLarge, "request too large")
		} else {
			h.error(w, http.StatusBadRequest, "one json request required")
		}
		return
	}
	if h.client == nil {
		h.error(w, http.StatusServiceUnavailable, "browser workload unavailable")
		return
	}
	result, err := h.execute(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalid):
			h.error(w, http.StatusBadRequest, "invalid browser request")
		case errors.Is(err, ErrClosed):
			h.error(w, http.StatusNotFound, "browser page not found")
		case errors.Is(err, errHandlerScreenshotTooLarge):
			h.error(w, http.StatusRequestEntityTooLarge, "screenshot exceeds private api response limit")
		case errors.Is(err, ErrLimit):
			h.error(w, http.StatusTooManyRequests, "browser resource limit reached")
		case errors.Is(err, ErrCreationUncertain):
			h.error(w, http.StatusServiceUnavailable, "browser target creation unconfirmed; workload teardown required")
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			h.error(w, http.StatusGatewayTimeout, "browser request timed out")
		default:
			h.error(w, http.StatusBadGateway, "browser operation failed")
		}
		return
	}
	body, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		h.error(w, http.StatusInternalServerError, "browser response encoding failed")
		return
	}
	if len(body)+1 > maxHandlerResponse {
		h.error(w, http.StatusRequestEntityTooLarge, "browser response exceeds private api limit")
		return
	}
	body = append(body, '\n')
	n, err := w.Write(body)
	if (err != nil || n != len(body)) && req.Action == "new" {
		if id, ok := result["pageId"].(string); ok {
			h.expireUnclaimed(id, nil)
		}
	}
}

// expireUnclaimed retires only a page that has not been claimed by a later
// request. A failed cleanup stays in Client's ownership registry for Close.
// expected prevents an obsolete timer from acting on a replacement entry.
func (h *handler) expireUnclaimed(id string, expected *unclaimedPage) {
	h.mu.Lock()
	entry, ok := h.unclaimed[id]
	if !ok || (expected != nil && entry != expected) {
		h.mu.Unlock()
		return
	}
	delete(h.unclaimed, id)
	if h.pages[id] == entry.page {
		delete(h.pages, id)
	}
	entry.timer.Stop()
	h.mu.Unlock()
	_ = h.client.cleanupPage(entry.page)
}
func (h *handler) error(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
func (h *handler) execute(ctx context.Context, r request) (map[string]any, error) {
	if r.Action == "new" {
		if r.PageID != "" || r.URL != "" || r.Selector != "" || r.Text != "" || r.X != 0 || r.Y != 0 || r.Width != 0 || r.Height != 0 {
			return nil, ErrInvalid
		}
		p, err := h.client.NewPage(ctx)
		if err != nil {
			return nil, err
		}
		idBytes := make([]byte, 16)
		if _, err = rand.Read(idBytes); err != nil {
			return nil, errors.Join(err, h.client.cleanupPage(p))
		}
		id := hex.EncodeToString(idBytes)
		h.mu.Lock()
		if h.unclaimed == nil {
			h.unclaimed = make(map[string]*unclaimedPage)
		}
		if _, exists := h.pages[id]; exists {
			h.mu.Unlock()
			return nil, errors.Join(errors.New("browser page identity collision"), h.client.cleanupPage(p))
		}
		h.pages[id] = p
		entry := &unclaimedPage{page: p}
		h.unclaimed[id] = entry
		entry.timer = time.AfterFunc(unclaimedPageTTL, func() {
			h.expireUnclaimed(id, entry)
		})
		h.mu.Unlock()
		return map[string]any{"ok": true, "pageId": id, "claimWithinMs": unclaimedPageTTL.Milliseconds()}, nil
	}
	if r.Action != "navigate" && r.Action != "title" && r.Action != "text" && r.Action != "click" && r.Action != "type" && r.Action != "screenshot" && r.Action != "close" {
		return nil, ErrInvalid
	}
	if len(r.PageID) != 32 {
		return nil, ErrInvalid
	}
	if _, err := hex.DecodeString(r.PageID); err != nil {
		return nil, ErrInvalid
	}
	// Reject irrelevant parameters rather than silently accepting a misleading
	// request. Zero-valued numeric fields are harmless and omitted by JSON clients.
	if (r.URL != "" && r.Action != "navigate") || (r.Selector != "" && r.Action != "text") || (r.Text != "" && r.Action != "type") || ((r.X != 0 || r.Y != 0) && r.Action != "click") || ((r.Width != 0 || r.Height != 0) && r.Action != "screenshot") {
		return nil, ErrInvalid
	}
	h.mu.Lock()
	p, ok := h.pages[r.PageID]
	if entry := h.unclaimed[r.PageID]; ok && entry != nil {
		entry.timer.Stop()
		delete(h.unclaimed, r.PageID)
	}
	h.mu.Unlock()
	if !ok {
		return nil, ErrClosed
	}
	defer func() {
		p.mu.Lock()
		closed, retired := p.closed, p.retired
		p.mu.Unlock()
		// A failed socket is not a confirmed closed target. Attempt bounded cleanup
		// before forgetting its public handle; retain the handle for close retries.
		if closed && !retired && r.Action != "close" {
			_ = h.client.cleanupPage(p)
		}
		p.mu.Lock()
		retired = p.retired
		p.mu.Unlock()
		if retired {
			h.mu.Lock()
			delete(h.pages, r.PageID)
			h.mu.Unlock()
		}
	}()
	switch r.Action {
	case "navigate":
		if err := p.NavigateAndWaitReady(ctx, r.URL); err != nil {
			return nil, err
		}
	case "title":
		text, err := p.Title(ctx)
		return map[string]any{"ok": err == nil, "title": text}, err
	case "text":
		text, err := p.Text(ctx, r.Selector)
		return map[string]any{"ok": err == nil, "text": text}, err
	case "click":
		if err := p.Click(ctx, r.X, r.Y); err != nil {
			return nil, err
		}
	case "type":
		if err := p.Type(ctx, r.Text); err != nil {
			return nil, err
		}
	case "screenshot":
		png, err := p.ScreenshotPNG(ctx, ScreenshotOptions{r.Width, r.Height})
		if err == nil && len(png) > maxHandlerPNG {
			return nil, errHandlerScreenshotTooLarge
		}
		return map[string]any{"ok": err == nil, "mime": "image/png", "data": png}, err
	case "close":
		if err := p.Close(ctx); err != nil {
			return nil, err
		}
	}
	return map[string]any{"ok": true}, nil
}
