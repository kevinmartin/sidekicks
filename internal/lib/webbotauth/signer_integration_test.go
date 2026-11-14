//go:build integration
// +build integration

package webbotauth_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kevinmartin/sidekicks/internal/lib/webbotauth"
)

const validCrawlTestError = "unknown public key or unknown verified bot ID for keyid"

func TestSignerWithCrawlTest(t *testing.T) {
	kp, err := webbotauth.GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Ed25519 key pair: %v", err)
	}

	tunnelURL := newTestDirectoryServer(t.Context(), t, []webbotauth.Ed25519KeyPair{kp})

	signer, err := webbotauth.NewSigner(kp.PublicKey, kp.PrivateKey)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), verifierTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://crawltest.com/cdn-cgi/web-bot-auth",
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	headers, err := signer.Sign(
		req,
		webbotauth.WithHeader("Accept", false),
		webbotauth.WithAuthority(false),
		webbotauth.WithMethod(false),
		webbotauth.WithNonce(16),
		webbotauth.WithSignatureAgent(tunnelURL),
		webbotauth.WithTag("web-bot-auth"),
	)
	if err != nil {
		t.Fatalf("Failed to sign request: %v", err)
	}

	for key, value := range headers {
		req.Header.Set(key, value[0])
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusOK {
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		t.Fatalf("read CrawlTest response: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized || strings.TrimSpace(string(body)) != validCrawlTestError {
		t.Fatalf("unexpected CrawlTest status %d: %s", resp.StatusCode, body)
	}
}

func TestSignerWithFingerprint(t *testing.T) {
	kp, err := webbotauth.GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Ed25519 key pair: %v", err)
	}

	tunnelURL := newTestDirectoryServer(t.Context(), t, []webbotauth.Ed25519KeyPair{kp})

	signer, err := webbotauth.NewSigner(kp.PublicKey, kp.PrivateKey)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), verifierTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://api.fingerprint-webbotauth.com/api/verify",
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	headers, err := signer.Sign(
		req,
		webbotauth.WithAuthority(false),
		webbotauth.WithMethod(false),
		webbotauth.WithNonce(16),
		webbotauth.WithSignatureAgent(tunnelURL),
		webbotauth.WithTag("web-bot-auth"),
	)
	if err != nil {
		t.Fatalf("Failed to sign request: %v", err)
	}

	for key, value := range headers {
		req.Header.Set(key, value[0])
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	fingerprintResp, err := decodeFingerprintResponse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK || fingerprintResp.Status != "success" {
		t.Logf("Status code: %v, signed-headers: %v, body: %v", resp.StatusCode, headers, fingerprintResp)
		t.Fatalf("Unexpected response status: %v", resp.StatusCode)
	}
}
