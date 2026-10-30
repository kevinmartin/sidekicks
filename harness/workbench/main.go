package main

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFS embed.FS

const maxScreenshot = 4 << 20

var endpoints = map[string]string{"novnc": "http://127.0.0.1:7791", "neko": "http://127.0.0.1:7792"}

type intent struct {
	Bot       string `json:"bot"`
	Intent    string `json:"intent"`
	At        string `json:"at"`
	Execution string `json:"execution"`
}
type app struct {
	root, token string
	client      *http.Client
	mu          sync.Mutex
	thread      []intent
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "check the local workbench HTTP endpoint")
	root := flag.String("root", ".", "repository root")
	listen := flag.String("listen", "127.0.0.1:7790", "listen address (host must stay loopback; container may use :7790)")
	novncURL := flag.String("novnc-url", endpoints["novnc"], "noVNC adapter base URL")
	nekoURL := flag.String("neko-url", endpoints["neko"], "Neko adapter base URL")
	flag.Parse()
	if *healthcheck {
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://127.0.0.1:7790/health")
		if err != nil {
			log.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	endpoints["novnc"] = *novncURL
	endpoints["neko"] = *nekoURL
	abs, err := filepath.Abs(*root)
	if err != nil {
		log.Fatal(err)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(abs, ".run"), 0700); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(abs, ".run/workbench.pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		log.Fatal(err)
	}
	a := &app{root: abs, token: hex.EncodeToString(secret), client: &http.Client{Timeout: 5 * time.Second}}
	server := &http.Server{Addr: *listen, Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	log.Printf("Sidekicks workbench http://127.0.0.1:7790 (root %s)", abs)
	log.Fatal(server.ListenAndServe())
}
func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			fail(w, 405, "GET required")
			return
		}
		send(w, 200, map[string]string{"token": a.token})
	})
	mux.HandleFunc("/api/adapters", a.adapters)
	mux.HandleFunc("/api/adapters/", a.adapterRoute)
	mux.HandleFunc("/api/thread", a.threadRoute)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		send(w, 200, map[string]any{"ok": true, "id": "workbench", "modelIntegration": false})
	})
	mux.HandleFunc("/fixture/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fixture/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(a.root, "shared/desktop-fixture.html"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := "index.html"
		if r.URL.Path != "/" {
			name = strings.TrimPrefix(r.URL.Path, "/")
			if name != "app.js" && name != "style.css" {
				http.NotFound(w, r)
				return
			}
		}
		data, err := webFS.ReadFile("web/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ct := map[string]string{"index.html": "text/html; charset=utf-8", "app.js": "text/javascript; charset=utf-8", "style.css": "text/css; charset=utf-8"}
		w.Header().Set("Content-Type", ct[name])
		w.Write(data)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' blob:; connect-src 'self'; frame-src 'self'; object-src 'none'; base-uri 'none'")
		if !loopbackHost(r.Host) {
			fail(w, 403, "loopback Host required")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Scheme != "http" || u.Host != r.Host {
				fail(w, 403, "same-origin requests required")
				return
			}
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("Authorization") != "Bearer "+a.token {
				fail(w, 403, "workbench session token required")
				return
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				fail(w, 415, "application/json required")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func loopbackHost(host string) bool {
	h, p, err := net.SplitHostPort(host)
	return err == nil && p == "7790" && (h == "127.0.0.1" || h == "localhost" || h == "::1")
}
func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	send(w, status, map[string]any{"ok": false, "error": message})
}
func adapterRequest(method, endpoint string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, err
	}
	// Keep the public loopback authority while Docker Desktop routes the transport.
	req.Host = net.JoinHostPort("127.0.0.1", req.URL.Port())
	return req, nil
}
func (a *app) adapterGet(endpoint string) (*http.Response, error) {
	req, err := adapterRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	return a.client.Do(req)
}
func (a *app) fetchJSON(endpoint string) any {
	resp, err := a.adapterGet(endpoint)
	if err != nil {
		return map[string]any{"ok": false, "error": "adapter unavailable", "detail": err.Error()}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	var value any
	if json.Unmarshal(body, &value) != nil {
		return map[string]any{"ok": false, "error": "adapter returned non-JSON", "httpStatus": resp.StatusCode}
	}
	return map[string]any{"httpStatus": resp.StatusCode, "body": value}
}
func (a *app) adapters(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, 405, "GET required")
		return
	}
	results := make(map[string]any)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for id, base := range endpoints {
		wg.Add(1)
		go func(id, base string) {
			defer wg.Done()
			health := a.fetchJSON(base + "/health")
			caps := a.fetchJSON(base + "/capabilities")
			control := a.fetchJSON(base + "/control")
			var recorded any
			data, err := os.ReadFile(filepath.Join(a.root, "pocs", id, "status.json"))
			if err == nil && len(data) <= 256<<10 {
				json.Unmarshal(data, &recorded)
			}
			mu.Lock()
			results[id] = map[string]any{"health": health, "capabilities": caps, "control": control, "recorded": recorded}
			mu.Unlock()
		}(id, base)
	}
	wg.Wait()
	send(w, 200, results)
}
func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, (64<<10)+1))
	if err != nil {
		return err
	}
	if len(data) > 64<<10 {
		return errors.New("request exceeds 64 KiB")
	}
	if err = json.Unmarshal(data, v); err != nil {
		return errors.New("valid JSON required")
	}
	return nil
}
func (a *app) adapterRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/adapters/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	base, ok := endpoints[parts[0]]
	if !ok {
		http.NotFound(w, r)
		return
	}
	route := parts[1]
	if route == "screenshot" {
		if r.Method != "GET" {
			fail(w, 405, "GET required")
			return
		}
		a.screenshot(w, r, base)
		return
	}
	if route != "control" && route != "input" {
		http.NotFound(w, r)
		return
	}
	if route == "control" && r.Method == "GET" {
		send(w, 200, a.fetchJSON(base+"/control"))
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "POST required")
		return
	}
	var payload map[string]any
	if err := readJSON(r, &payload); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if err := validatePayload(route, payload); err != nil {
		fail(w, 400, err.Error())
		return
	}
	data, _ := json.Marshal(payload)
	req, _ := adapterRequest("POST", base+"/"+route, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		fail(w, 502, "adapter unavailable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (256<<10)+1))
	if err != nil || len(body) > 256<<10 {
		fail(w, 502, "adapter response too large")
		return
	}
	var decoded any
	if json.Unmarshal(body, &decoded) != nil {
		fail(w, 502, "adapter returned non-JSON")
		return
	}
	send(w, resp.StatusCode, decoded)
}
func field(p map[string]any, k string) string { s, _ := p[k].(string); return s }
func validCoord(v any) bool {
	n, ok := v.(float64)
	return ok && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= 16384
}
func in(v string, values ...string) bool {
	for _, x := range values {
		if v == x {
			return true
		}
	}
	return false
}
func validatePayload(route string, p map[string]any) error {
	session := field(p, "session")
	if len(session) < 1 || len(session) > 128 {
		return errors.New("session must contain 1-128 characters")
	}
	if route == "control" {
		if !in(field(p, "action"), "acquire", "release", "stop") {
			return errors.New("unknown control action")
		}
		if field(p, "action") == "acquire" {
			if !in(field(p, "owner"), "human", "agent") {
				return errors.New("owner must be human or agent")
			}
			if ms, ok := p["leaseMs"]; ok {
				n, valid := ms.(float64)
				if !valid || n < 1000 || n > 60000 {
					return errors.New("leaseMs must be 1000-60000")
				}
			}
		}
		return nil
	}
	if len(field(p, "token")) < 1 {
		return errors.New("lease token required")
	}
	events, ok := p["events"].([]any)
	if !ok || len(events) < 1 || len(events) > 100 {
		return errors.New("provide 1-100 events")
	}
	for _, value := range events {
		e, ok := value.(map[string]any)
		if !ok {
			return errors.New("event must be object")
		}
		kind := field(e, "type")
		switch kind {
		case "releaseAll":
		case "text":
			s, ok := e["text"].(string)
			if !ok || len(s) > 2048 {
				return errors.New("text limited to 2048 bytes")
			}
		case "key":
			if len(field(e, "key")) < 1 || len(field(e, "key")) > 64 || !in(field(e, "action"), "down", "up") {
				return errors.New("invalid key event")
			}
		case "pointer", "click", "scroll":
			if !validCoord(e["x"]) || !validCoord(e["y"]) {
				return errors.New("invalid desktop coordinates")
			}
			if kind == "pointer" {
				if !in(field(e, "action"), "move", "down", "up") {
					return errors.New("invalid pointer action")
				}
				if b := field(e, "button"); b != "" && !in(b, "left", "middle", "right") {
					return errors.New("invalid pointer button")
				}
			}
			if kind == "click" {
				if !in(field(e, "button"), "left", "right") {
					return errors.New("invalid click button")
				}
				if c, ok := e["count"].(float64); !ok || (c != 1 && c != 2) {
					return errors.New("click count must be 1 or 2")
				}
			}
			if kind == "scroll" {
				for _, axis := range []string{"deltaX", "deltaY"} {
					n, ok := e[axis].(float64)
					if !ok || math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 2000 {
						return errors.New("scroll delta exceeds 2000")
					}
				}
			}
		default:
			return fmt.Errorf("unsupported event type %q", kind)
		}
	}
	return nil
}
func (a *app) screenshot(w http.ResponseWriter, r *http.Request, base string) {
	width, height := 1280, 720
	for name, target := range map[string]*int{"maxWidth": &width, "maxHeight": &height} {
		if raw := r.URL.Query().Get(name); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 1 {
				fail(w, 400, "positive integer dimensions required")
				return
			}
			*target = v
		}
	}
	if width > 1920 || height > 1080 {
		fail(w, 400, "maximum screenshot bounds are 1920x1080")
		return
	}
	resp, err := a.adapterGet(fmt.Sprintf("%s/screenshot?maxWidth=%d&maxHeight=%d", base, width, height))
	if err != nil {
		fail(w, 502, "adapter unavailable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(w, resp.StatusCode, "adapter screenshot unavailable")
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxScreenshot+1))
	if err != nil || len(body) > maxScreenshot {
		fail(w, 502, "screenshot exceeds 4 MiB")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || !in(format, "png", "jpeg") {
		fail(w, 502, "adapter must return PNG or JPEG")
		return
	}
	if config.Width > width || config.Height > height || config.Width < 1 || config.Height < 1 {
		fail(w, 502, "adapter screenshot exceeds requested bounds")
		return
	}
	content := "image/png"
	if format == "jpeg" {
		content = "image/jpeg"
	}
	w.Header().Set("Content-Type", content)
	w.Header().Set("X-Screenshot-Width", strconv.Itoa(config.Width))
	w.Header().Set("X-Screenshot-Height", strconv.Itoa(config.Height))
	w.Header().Set("X-Screenshot-Bytes", strconv.Itoa(len(body)))
	for _, name := range []string{"X-Source-Width", "X-Source-Height"} {
		if value, err := strconv.Atoi(resp.Header.Get(name)); err == nil && value > 0 && value <= 16384 {
			w.Header().Set(name, strconv.Itoa(value))
		}
	}
	if source := resp.Header.Get("X-Capture-Source"); len(source) <= 120 {
		w.Header().Set("X-Capture-Source", source)
	}
	w.Write(body)
}
func (a *app) threadRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		a.mu.Lock()
		items := append([]intent{}, a.thread...)
		a.mu.Unlock()
		send(w, 200, items)
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "GET or POST required")
		return
	}
	var item intent
	if err := readJSON(r, &item); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !in(item.Bot, "Scout", "Builder", "Reviewer") || len(item.Intent) < 1 || len(item.Intent) > 500 {
		fail(w, 400, "known sample persona and 1-500 character intent required")
		return
	}
	item.At = time.Now().UTC().Format(time.RFC3339Nano)
	item.Execution = "intent-only"
	a.mu.Lock()
	a.thread = append(a.thread, item)
	if len(a.thread) > 100 {
		a.thread = a.thread[len(a.thread)-100:]
	}
	a.mu.Unlock()
	send(w, 201, item)
}
