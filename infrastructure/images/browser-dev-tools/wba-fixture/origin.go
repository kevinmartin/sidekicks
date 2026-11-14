package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const existingHeader = "X-Sidekicks-Existing"

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
	routes.HandleFunc("GET "+f.config.PodIP.String()+"/healthz", func(w http.ResponseWriter, _ *http.Request) {
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
