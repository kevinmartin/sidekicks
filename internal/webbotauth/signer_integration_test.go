//go:build integration
// +build integration

package webbotauth_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kevinmartin/sidekicks/internal/webbotauth"
)

const validCrawlTestError = "unknown public key or unknown verified bot ID for keyid"

func TestSignerWithCrawlTest(t *testing.T) {
	kp, err := webbotauth.GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Ed25519 key pair: %v", err)
	}

	tunnelURL, cleanup := newTestDirectoryServer(t, []webbotauth.Ed25519KeyPair{kp})
	defer cleanup()

	signer, err := webbotauth.NewSigner(kp.PublicKey, kp.PrivateKey)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	req, err := http.NewRequest("GET", "https://crawltest.com/cdn-cgi/web-bot-auth", nil)
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

	if resp.StatusCode != http.StatusOK {
		bodyBytes := make([]byte, 1024)
		n, _ := resp.Body.Read(bodyBytes)

		if resp.StatusCode != http.StatusUnauthorized || strings.TrimSpace(string(bodyBytes[:n])) != validCrawlTestError {
			//time.Sleep(5 * time.Minute) // Wait for 5 minutes to allow for any potential propagation or processing delays
			t.Logf("Status code: %v, signed-headers: %v, body: %v", resp.StatusCode, headers, string(bodyBytes[:n]))
			t.Fatalf("Unexpected response status: %v", resp.StatusCode)
		}
	}
}

func TestSignerWithFingerprint(t *testing.T) {
	kp, err := webbotauth.GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Ed25519 key pair: %v", err)
	}

	tunnelURL, cleanup := newTestDirectoryServer(t, []webbotauth.Ed25519KeyPair{kp})
	defer cleanup()

	signer, err := webbotauth.NewSigner(kp.PublicKey, kp.PrivateKey)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	req, err := http.NewRequest("GET", "https://api.fingerprint-webbotauth.com/api/verify", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	headers, err := signer.Sign(
		req,
		//webbotauth.WithHeader("Accept", false),
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
	defer resp.Body.Close()

	fingerprintResp, err := decodeFingerprintResponse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK || fingerprintResp.Status != "success" {
		t.Logf("Status code: %v, signed-headers: %v, body: %v", resp.StatusCode, headers, fingerprintResp)
		t.Fatalf("Unexpected response status: %v", resp.StatusCode)
	}
}
