package webbotauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kevinmartin/sidekicks/internal/webbotauth"
)

func TestSignedDirectoryHandlerJSON(t *testing.T) {
	const agent = "https://example.test"
	keyPair, err := webbotauth.GenerateEd25519KeyPair()
	if err != nil {
		t.Fatal(err)
	}
	handler, err := webbotauth.SignedDirectoryHandler(agent, "<ai>&\u2028\u2029\xff", []webbotauth.Ed25519KeyPair{keyPair})
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, agent+"/.well-known/http-message-signatures-directory", nil)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	const wantPurpose = `"purpose":"\u003cai\u003e\u0026\u2028\u2029` + "\uFFFD\""
	if !strings.Contains(response.Body.String(), wantPurpose) {
		t.Fatalf("body = %s, want %s", response.Body, wantPurpose)
	}

	digest := sha256.Sum256(response.Body.Bytes())
	wantDigest := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
	if got := response.Header().Get("Content-Digest"); got != wantDigest {
		t.Fatalf("Content-Digest = %q, want %q", got, wantDigest)
	}
}

func TestSignedDirectoryHandlerInvalidAgentURL(t *testing.T) {
	keyPair, err := webbotauth.GenerateEd25519KeyPair()
	if err != nil {
		t.Fatal(err)
	}

	_, err = webbotauth.SignedDirectoryHandler("https://[invalid", "ai", []webbotauth.Ed25519KeyPair{keyPair})
	if err == nil {
		t.Fatal("expected an error for a malformed signature agent URL")
	}
}
