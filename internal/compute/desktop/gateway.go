package desktop

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/compute/control"
)

// Config describes a single private desktop, never a client-selected target.
// Authenticate must validate the instance/viewer credential without placing
// secrets in URLs. Host and Origin are exact allowlists, not suffix patterns.
// This handler intentionally does not expose control acquisition or guest exec.
type Config struct {
	Target         string
	Path           string
	Gate           HumanGate
	Authenticate   func(*http.Request) (control.Credentials, error)
	AllowedHosts   []string
	AllowedOrigins []string
	Counters       *Counters
	// MaxConnections bounds simultaneous viewer buffers; zero defaults to eight.
	MaxConnections int
	// DialContext is an injectable dialer for deterministic tests. In production,
	// leave nil; targets are always validated as numeric loopback TCP endpoints.
	DialContext func(context.Context, string, string) (net.Conn, error)
}

// Gateway serves an authenticated desktop transport.
type Gateway struct {
	config      Config
	hosts       map[string]struct{}
	origins     map[string]struct{}
	connections chan struct{}
}

// NewGateway rejects incomplete or dangerously broad configurations.
func NewGateway(config Config) (*Gateway, error) {
	host, port, err := net.SplitHostPort(config.Target)
	if err != nil {
		return nil, errors.New("desktop target must be a numeric loopback tcp endpoint")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() || ip.Zone() != "" {
		return nil, errors.New("desktop target must be loopback")
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return nil, errors.New("desktop target port must be 1..65535")
	}
	if config.Gate == nil || config.Authenticate == nil {
		return nil, errors.New("desktop gate and authentication are required")
	}
	if config.Path == "" {
		config.Path = "/websockify"
	}
	if !strings.HasPrefix(config.Path, "/") || strings.ContainsAny(config.Path, "?#%") {
		return nil, errors.New("desktop path must be an exact absolute path")
	}
	if len(config.AllowedHosts) == 0 || len(config.AllowedOrigins) == 0 {
		return nil, errors.New("desktop host and origin allowlists are required")
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = 8
	}
	if config.MaxConnections < 1 || config.MaxConnections > 32 {
		return nil, errors.New("desktop connection limit must be 1..32")
	}
	gateway := &Gateway{config: config, hosts: map[string]struct{}{}, origins: map[string]struct{}{}, connections: make(chan struct{}, config.MaxConnections)}
	for _, host := range config.AllowedHosts {
		if strings.ContainsAny(host, "/\\?#@ \t\r\n*") || host == "" {
			return nil, errors.New("invalid desktop host allowlist")
		}
		u, err := url.Parse("http://" + host)
		if err != nil || u.Host != host || u.Hostname() == "" {
			return nil, errors.New("invalid desktop host allowlist")
		}
		gateway.hosts[host] = struct{}{}
	}
	for _, origin := range config.AllowedOrigins {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" {
			return nil, errors.New("invalid desktop origin allowlist")
		}
		if _, allowed := gateway.hosts[u.Host]; !allowed {
			return nil, errors.New("desktop origin must match an allowed host")
		}
		if origin != u.Scheme+"://"+u.Host {
			return nil, errors.New("desktop origin must be canonical")
		}
		gateway.origins[origin] = struct{}{}
	}
	if config.Counters == nil {
		gateway.config.Counters = &Counters{}
	}
	if config.DialContext == nil {
		gateway.config.DialContext = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	}
	return gateway, nil
}

// ServeHTTP serves the HTTP request.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != g.config.Path || r.URL.RawPath != "" || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := g.hosts[r.Host]; !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	origins := r.Header.Values("Origin")
	if len(origins) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if _, ok := g.origins[origins[0]]; !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	credentials, err := g.config.Authenticate(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	select {
	case g.connections <- struct{}{}:
		defer func() {
			<-g.connections
		}()
	default:
		http.Error(w, "desktop viewer limit reached", http.StatusServiceUnavailable)
		return
	}
	upstream, err := g.config.DialContext(r.Context(), "tcp", g.config.Target)
	if err != nil || upstream == nil {
		http.Error(w, "desktop unavailable", http.StatusServiceUnavailable)
		return
	}
	defer upstream.Close()
	// Host, exact Origin and authentication were checked above. The library's
	// suffix-pattern origin mechanism is intentionally bypassed in favor of those
	// exact checks; this does not disable the checks above.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, Subprotocols: []string{"binary"}, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	conn.SetReadLimit(MaxClientMessage)
	client := &websocketClient{connection: conn}
	_ = Bridge(r.Context(), client, upstream, g.config.Gate, credentials, g.config.Counters)
}

type websocketClient struct{ connection *websocket.Conn }

// Read reads data into the supplied buffer.
func (c *websocketClient) Read(ctx context.Context) ([]byte, error) {
	kind, data, err := c.connection.Read(ctx)
	if err != nil {
		return nil, err
	}
	if kind != websocket.MessageBinary {
		_ = c.connection.Close(websocket.StatusUnsupportedData, "binary RFB required")
		return nil, fmt.Errorf("%w: text websocket message", ErrProtocol)
	}
	return data, nil
}

// Write writes data from the supplied buffer.
func (c *websocketClient) Write(ctx context.Context, data []byte) error {
	return c.connection.Write(ctx, websocket.MessageBinary, data)
}

// Close releases the receiver's resources.
func (c *websocketClient) Close() error {
	return c.connection.CloseNow()
}
