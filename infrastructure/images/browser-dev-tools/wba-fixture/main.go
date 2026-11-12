// wba-fixture is the dev-only Signing Broker and HTTPS verifier.
package main

import (
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kevinmartin/sidekicks/internal/webbotauth"
)

const (
	signedHost     = "signed.sidekicks-dev.svc.cluster.local"
	unsignedHost   = "unsigned.sidekicks-dev.svc.cluster.local"
	blockedHost    = "blocked.sidekicks-dev.svc.cluster.local"
	wbaDirectory   = "https://wba.sidekicks.test"
	fixturePort    = ":8443"
	socketPath     = "/run/sidekicks/wba/broker.sock"
	existingHeader = "X-Sidekicks-Existing"
	readinessLease = 12 * time.Second
)

type observation struct {
	key   string
	event string
}

type fixture struct {
	signer *webbotauth.Signer
	public ed25519.PublicKey
	mu     sync.Mutex
	recent []observation
	ready  map[string]time.Time
}

func main() {
	token := os.Getenv("BROWSER_GATEWAY_TOKEN")
	if token == "" {
		log.Fatal("BROWSER_GATEWAY_TOKEN is required")
	}
	key, err := webbotauth.GenerateEd25519KeyPair()
	if err != nil {
		log.Fatal(err)
	}
	signer, err := webbotauth.NewSigner(key.PublicKey, key.PrivateKey)
	if err != nil {
		log.Fatal(err)
	}
	f := &fixture{
		signer: signer,
		public: key.PublicKey,
		ready:  make(map[string]time.Time),
	}
	// An emptyDir survives a container restart within the same pod.
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatal(err)
	}
	socket, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Chmod(socketPath, 0660); err != nil {
		log.Fatal(err)
	}
	go func() {
		for {
			conn, err := socket.Accept()
			if err != nil {
				log.Fatal(err)
			}
			go f.serveBroker(conn)
		}
	}()
	go func() {
		server := &http.Server{
			Addr:              "127.0.0.1:8081",
			Handler:           f.observer(token),
			ReadHeaderTimeout: 3 * time.Second,
		}
		log.Fatal(server.ListenAndServe())
	}()
	server := &http.Server{
		Addr:              fixturePort,
		Handler:           f.origin(),
		ReadHeaderTimeout: 3 * time.Second,
	}
	log.Fatal(server.ListenAndServeTLS("/etc/sidekicks/dev/tls.crt", "/etc/sidekicks/dev/tls.key"))
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

func (f *fixture) observer(token string) http.Handler {
	expected := []byte("Bearer " + token)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /origins", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"signed":      "https://" + signedHost + fixturePort,
			"blockedHost": blockedHost,
		})
	})
	mux.HandleFunc("GET /readiness", func(w http.ResponseWriter, r *http.Request) {
		targetID := r.URL.Query().Get("targetId")
		f.mu.Lock()
		expires, exists := f.ready[targetID]
		f.mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]bool{
			"ready":   time.Now().Before(expires),
			"retired": exists && expires.IsZero(),
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

func (f *fixture) serveBroker(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	var message struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Method   string `json:"method"`
		URL      string `json:"url"`
		TargetID string `json:"targetId"`
	}
	if err := json.NewDecoder(io.LimitReader(conn, 32<<10)).Decode(&message); err != nil || message.ID == "" {
		return
	}
	response := map[string]any{
		"id": message.ID,
		"ok": false,
	}
	signedAuthority, blockedAuthority := signedHost+fixturePort, blockedHost+fixturePort
	switch message.Type {
	case "configure":
		response["ok"] = true
		response["origins"] = []string{"https://" + signedAuthority, "https://" + blockedAuthority}
	case "ready", "retire":
		if message.TargetID == "" {
			break
		}
		f.mu.Lock()
		if message.Type == "retire" {
			f.ready[message.TargetID] = time.Time{}
			response["ok"] = true
		} else if expires, exists := f.ready[message.TargetID]; !exists || !expires.IsZero() {
			// Retain retired targets so a late ready message cannot resurrect them.
			f.ready[message.TargetID] = time.Now().Add(readinessLease)
			response["ok"] = true
			response["leaseMs"] = readinessLease.Milliseconds()
		}
		f.mu.Unlock()
	case "sign":
		req, err := http.NewRequest(message.Method, message.URL, nil)
		if err != nil || req.URL.Scheme != "https" || req.URL.User != nil ||
			(req.Host != signedAuthority && req.Host != blockedAuthority) ||
			(req.Method != http.MethodGet && req.Method != http.MethodHead) {
			break
		}
		if req.URL.Path == "/timeout" {
			f.record(req.URL.Query().Get("run"), "broker timeout")
			time.Sleep(1500 * time.Millisecond)
			return
		}
		headers, err := f.signer.Sign(
			req,
			webbotauth.WithAuthority(false),
			webbotauth.WithMethod(false),
			webbotauth.WithPath(false),
			webbotauth.WithQuery(false),
			webbotauth.WithSignatureAgent(wbaDirectory),
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
	_ = json.NewEncoder(conn).Encode(response)
}

func (f *fixture) origin() http.Handler {
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r.URL.Query().Get("run"), "origin "+r.URL.Path)
		w.Header().Set("Cache-Control", "no-store")
		// ServeMux matches the hostname after stripping its port.
		if !strings.HasSuffix(r.Host, fixturePort) {
			unexpectedDestination(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
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
		if err := verifyWBA(r, f.public); err != nil {
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
		if host == blockedHost {
			f.record(run, "verified blocked redirect")
		}
		location := "https://" + host + fixturePort + path + "?run=" + url.QueryEscape(run)
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
func verifyWBA(r *http.Request, publicKey ed25519.PublicKey) error {
	params, ok := strings.CutPrefix(r.Header.Get("Signature-Input"), "sig1=(")
	componentList, _, found := strings.Cut(params, ");")
	if !ok || !found || r.Header.Get("Signature-Agent") != fmt.Sprintf("%q", wbaDirectory) {
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
	return nil
}
