// wba-fixture is the dev-only Signing Broker and HTTPS verifier.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kevinmartin/sidekicks/internal/webbotauth"
)

const existingHeader = "X-Sidekicks-Existing"

type observation struct {
	key   string
	event string
}

type tabReadiness struct {
	expires time.Time
	retired bool
}

type fixture struct {
	config config
	port   string
	signer *webbotauth.Signer
	public ed25519.PublicKey
	mu     sync.Mutex
	recent []observation
	ready  map[string]tabReadiness
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runFixture(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}

func runFixture(ctx context.Context, cfg config) error {
	key, err := webbotauth.GenerateEd25519KeyPair()
	if err != nil {
		return fmt.Errorf("generate fixture signing key: %w", err)
	}
	signer, err := webbotauth.NewSigner(key.PublicKey, key.PrivateKey)
	if err != nil {
		return fmt.Errorf("create fixture signer: %w", err)
	}
	f := &fixture{
		config: cfg,
		port:   strconv.Itoa(int(cfg.Port)),
		signer: signer,
		public: key.PublicKey,
		ready:  make(map[string]tabReadiness),
	}
	// An emptyDir survives a container restart within the same pod.
	if err := os.Remove(cfg.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove old broker socket: %w", err)
	}
	var workers sync.WaitGroup
	defer workers.Wait()
	socket, err := (&net.ListenConfig{}).Listen(ctx, "unix", cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on broker socket: %w", err)
	}
	defer socket.Close()
	if err := os.Chmod(cfg.SocketPath, 0660); err != nil {
		return fmt.Errorf("set broker socket permissions: %w", err)
	}
	observer := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", strconv.Itoa(int(cfg.ObserverPort))),
		Handler:           f.observer(),
		ReadHeaderTimeout: 3 * time.Second,
	}
	defer observer.Close()
	origin := &http.Server{
		Addr:              net.JoinHostPort("", f.port),
		Handler:           f.origin(),
		ReadHeaderTimeout: 3 * time.Second,
	}
	defer origin.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	failures := make(chan error, 4)
	failFixture := func(err error) { failures <- err }
	start := func(name string, report func(error), work func() error) {
		workers.Go(func() {
			defer func() {
				if panicValue := recover(); panicValue != nil {
					report(fmt.Errorf("%s panic: %v\n%s", name, panicValue, debug.Stack()))
				}
			}()
			if err := work(); err != nil && ctx.Err() == nil {
				report(fmt.Errorf("%s: %w", name, err))
			}
		})
	}
	start("readiness cleanup", failFixture, func() error {
		ticker := time.NewTicker(cfg.ReadinessLease)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case now := <-ticker.C:
				f.expireReadiness(now)
			}
		}
	})
	start("broker listener", failFixture, func() error {
		for {
			conn, err := socket.Accept()
			if err != nil {
				return err
			}
			start("broker connection", func(err error) { log.Print(err) }, func() error {
				return f.serveBroker(ctx, conn)
			})
		}
	})
	start("fixture observer", failFixture, observer.ListenAndServe)
	start("fixture HTTPS origin", failFixture, func() error {
		return origin.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
	})
	select {
	case <-ctx.Done():
		return nil
	case err := <-failures:
		return err
	}
}

func (f *fixture) expireReadiness(now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	maps.DeleteFunc(f.ready, func(_ string, state tabReadiness) bool {
		return !now.Before(state.expires)
	})
}

func (f *fixture) record(key, event string) {
	if key == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.recent) == 512 {
		copy(f.recent, f.recent[1:])
		f.recent = f.recent[:len(f.recent)-1]
	}
	f.recent = append(f.recent, observation{
		key:   key,
		event: event,
	})
}

func (f *fixture) observer() http.Handler {
	expected := []byte("Bearer " + f.config.Token)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /origins", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"signed":      "https://" + net.JoinHostPort(f.config.SignedHost, f.port),
			"blockedHost": f.config.BlockedHost,
		})
	})
	mux.HandleFunc("GET /readiness", func(w http.ResponseWriter, r *http.Request) {
		targetID := r.URL.Query().Get("targetId")
		f.mu.Lock()
		state := f.ready[targetID]
		f.mu.Unlock()
		current := time.Now().Before(state.expires)
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]bool{
			"ready":   current && !state.retired,
			"retired": current && state.retired,
		})
	})
	mux.HandleFunc("GET /observations", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		var events []string
		f.mu.Lock()
		for _, entry := range f.recent {
			if entry.key == key {
				events = append(events, entry.event)
			}
		}
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(events)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), expected) != 1 {
			http.Error(w, "fixture authorization required", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mux.ServeHTTP(w, r)
	})
}

func (f *fixture) serveBroker(ctx context.Context, conn net.Conn) error {
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if err := conn.SetDeadline(time.Now().Add(f.config.BrokerTimeout)); err != nil {
		return fmt.Errorf("set broker connection deadline: %w", err)
	}
	var message struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Method   string `json:"method"`
		URL      string `json:"url"`
		TargetID string `json:"targetId"`
		SentAtMS int64  `json:"sentAtMs"`
	}
	if err := json.NewDecoder(io.LimitReader(conn, 32<<10)).Decode(&message); err != nil || message.ID == "" {
		return nil
	}
	response := map[string]any{
		"id": message.ID,
		"ok": false,
	}
	signedAuthority := net.JoinHostPort(f.config.SignedHost, f.port)
	blockedAuthority := net.JoinHostPort(f.config.BlockedHost, f.port)
	readinessLease := f.config.ReadinessLease
	switch message.Type {
	case "configure":
		response["ok"] = true
		response["origins"] = []string{"https://" + signedAuthority, "https://" + blockedAuthority}
	case "ready", "retire":
		if message.TargetID == "" {
			break
		}
		f.mu.Lock()
		now := time.Now()
		state := f.ready[message.TargetID]
		sentAt := time.UnixMilli(message.SentAtMS)
		if message.Type == "retire" {
			f.ready[message.TargetID] = tabReadiness{
				expires: now.Add(readinessLease),
				retired: true,
			}
			response["ok"] = true
		} else if !sentAt.After(now) && now.Before(sentAt.Add(readinessLease)) &&
			!(state.retired && now.Before(state.expires)) {
			// Ready messages sent before retirement expire by the time the
			// retirement record can be removed. Both processes use the pod clock.
			f.ready[message.TargetID] = tabReadiness{expires: now.Add(readinessLease)}
			response["ok"] = true
			response["leaseMs"] = readinessLease.Milliseconds()
		}
		f.mu.Unlock()
	case "sign":
		req, err := http.NewRequestWithContext(ctx, message.Method, message.URL, nil)
		if err != nil || req.URL.Scheme != "https" || req.URL.User != nil ||
			(req.Host != signedAuthority && req.Host != blockedAuthority) ||
			(req.Method != http.MethodGet && req.Method != http.MethodHead) {
			break
		}
		if req.URL.Path == "/timeout" {
			f.record(req.URL.Query().Get("run"), "broker timeout")
			timer := time.NewTimer(1500 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
			return nil
		}
		headers, err := f.signer.Sign(
			req,
			webbotauth.WithAuthority(false),
			webbotauth.WithMethod(false),
			webbotauth.WithPath(false),
			webbotauth.WithQuery(false),
			webbotauth.WithSignatureAgent(f.config.Directory),
			webbotauth.WithTag("web-bot-auth"),
			webbotauth.WithNonce(16),
		)
		if err == nil {
			response["ok"] = true
			response["headers"] = map[string]string{
				"Signature":       headers.Get("Signature"),
				"Signature-Input": headers.Get("Signature-Input"),
				"Signature-Agent": headers.Get("Signature-Agent"),
			}
		}
	}
	if err := json.NewEncoder(conn).Encode(response); err != nil {
		return fmt.Errorf("write broker response: %w", err)
	}
	return nil
}

func (f *fixture) origin() http.Handler {
	signedHost, unsignedHost, blockedHost := f.config.SignedHost, f.config.UnsignedHost, f.config.BlockedHost
	mux := http.NewServeMux()
	mux.HandleFunc(signedHost+"/favicon.ico", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc(signedHost+"/frame", f.unsignedSubresource)
	mux.HandleFunc(signedHost+"/asset", f.unsignedSubresource)
	mux.HandleFunc(signedHost+"/redirect", f.signed(f.redirectTo(unsignedHost, "/unsigned")))
	mux.HandleFunc(signedHost+"/blocked-redirect", f.signed(f.redirectTo(blockedHost, "/blocked")))
	mux.HandleFunc(signedHost+"/", f.signed(signedPage))
	mux.HandleFunc(unsignedHost+"/", unsignedPage)
	mux.HandleFunc("/", unexpectedDestination)

	routes := http.NewServeMux()
	// Kubelet probes the pod IP directly, outside the fixture's signing hosts.
	routes.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	routes.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.URL.Query().Get("run"), "origin "+r.URL.Path)
		w.Header().Set("Cache-Control", "no-store")
		// ServeMux matches the hostname after stripping its port.
		if !strings.HasSuffix(r.Host, ":"+f.port) {
			unexpectedDestination(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
	return routes
}

func unexpectedDestination(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "unexpected destination", http.StatusForbidden)
}

func (f *fixture) unsignedSubresource(w http.ResponseWriter, r *http.Request) {
	if hasWBAHeaders(r.Header) {
		http.Error(w, "subresource was signed", http.StatusBadRequest)
		return
	}
	f.record(r.URL.Query().Get("run"), "unsigned "+r.URL.Path)
	w.WriteHeader(http.StatusNoContent)
}

func (f *fixture) signed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := verifyWBA(r, f.public, f.config.Directory); err != nil {
			log.Print(err)
			http.Error(w, "invalid Web Bot Auth signature", http.StatusUnauthorized)
			return
		}
		if r.Header.Get(existingHeader) != "present" {
			http.Error(w, "existing request header was lost", http.StatusBadRequest)
			return
		}
		next(w, r)
	}
}

func (f *fixture) redirectTo(host, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		run := r.URL.Query().Get("run")
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value != run {
			http.Error(w, "existing cookie was lost", http.StatusBadRequest)
			return
		}
		if host == f.config.BlockedHost {
			f.record(run, "verified blocked redirect")
		}
		location := "https://" + net.JoinHostPort(host, f.port) + path + "?run=" + url.QueryEscape(run)
		http.Redirect(w, r, location, http.StatusFound)
	}
}

func signedPage(w http.ResponseWriter, r *http.Request) {
	run := r.URL.Query().Get("run")
	query := "?run=" + url.QueryEscape(run)
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    run,
		Secure:   true,
		HttpOnly: true,
		Path:     "/",
	})
	_, _ = fmt.Fprintf(
		w,
		"<title>Signed request verified</title><iframe src='/frame%s'></iframe><img src='/asset%s'>",
		query,
		query,
	)
}

func unsignedPage(w http.ResponseWriter, r *http.Request) {
	if hasWBAHeaders(r.Header) {
		http.Error(w, "signature leaked across redirect", http.StatusBadRequest)
		return
	}
	_, _ = io.WriteString(w, "<title>Unsigned redirect verified</title>")
}

func hasWBAHeaders(headers http.Header) bool {
	for _, name := range []string{"Signature", "Signature-Input", "Signature-Agent"} {
		if len(headers.Values(name)) != 0 {
			return true
		}
	}
	return false
}

// verifyWBA reconstructs the signature base from the received HTTP message.
// The fixture accepts only the components and label used in this test.
func verifyWBA(r *http.Request, publicKey ed25519.PublicKey, directory string) error {
	params, ok := strings.CutPrefix(r.Header.Get("Signature-Input"), "sig1=(")
	componentList, metadata, found := strings.Cut(params, ");")
	if !ok || !found || r.Header.Get("Signature-Agent") != fmt.Sprintf("%q", directory) {
		return errors.New("unexpected signature input or agent")
	}
	path, query, _ := strings.Cut(r.RequestURI, "?")
	components := map[string]string{
		`"@authority"`:      r.Host,
		`"@method"`:         r.Method,
		`"@path"`:           path,
		`"@query"`:          "?" + query,
		`"signature-agent"`: r.Header.Get("Signature-Agent"),
	}
	lines := make([]string, 0, len(components)+1)
	for _, component := range strings.Fields(componentList) {
		value, ok := components[component]
		if !ok {
			return errors.New("unexpected signature component")
		}
		delete(components, component)
		lines = append(lines, component+": "+value)
	}
	if len(components) != 0 {
		return errors.New("missing signature components")
	}
	lines = append(lines, `"@signature-params": (`+params)
	sig, ok := strings.CutPrefix(r.Header.Get("Signature"), "sig1=:")
	sig, ended := strings.CutSuffix(sig, ":")
	raw, err := base64.StdEncoding.DecodeString(sig)
	if !ok || !ended || err != nil || !ed25519.Verify(publicKey, []byte(strings.Join(lines, "\n")), raw) {
		return errors.New("invalid signature")
	}
	var expiry time.Time
	for parameter := range strings.SplitSeq(metadata, ";") {
		value, found := strings.CutPrefix(parameter, "expires=")
		if !found {
			continue
		}
		unix, err := strconv.ParseInt(value, 10, 64)
		if err != nil || !expiry.IsZero() {
			return errors.New("invalid signature expiration")
		}
		expiry = time.Unix(unix, 0)
	}
	if expiry.IsZero() {
		return errors.New("missing signature expiration")
	}
	if !time.Now().Before(expiry) {
		return errors.New("expired signature")
	}
	return nil
}
