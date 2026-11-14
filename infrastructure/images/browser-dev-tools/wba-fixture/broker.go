package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"time"

	"github.com/kevinmartin/sidekicks/internal/lib/webbotauth"
)

type tabReadiness struct {
	expires time.Time
	retired bool
}

func (f *fixture) expireReadiness(now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	maps.DeleteFunc(f.ready, func(_ string, state tabReadiness) bool {
		return !now.Before(state.expires)
	})
}

func (f *fixture) serveBroker(ctx context.Context, conn net.Conn) error {
	closeConnection := func() {
		_ = conn.Close()
	}
	defer closeConnection()
	stop := context.AfterFunc(ctx, closeConnection)
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
	if err := json.NewDecoder(io.LimitReader(conn, 32<<10)).Decode(&message); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("decode broker request: %w", err)
	}
	if message.ID == "" {
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
			(!state.retired || !now.Before(state.expires)) {
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
		if err != nil {
			break
		}
		response["ok"] = true
		response["headers"] = map[string]string{
			"Signature":       headers.Get("Signature"),
			"Signature-Input": headers.Get("Signature-Input"),
			"Signature-Agent": headers.Get("Signature-Agent"),
		}
	}
	if err := json.NewEncoder(conn).Encode(response); err != nil {
		return fmt.Errorf("write broker response: %w", err)
	}
	return nil
}
