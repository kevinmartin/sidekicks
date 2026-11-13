package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kevinmartin/sidekicks/internal/webbotauth"
)

func TestVerifyWBAExpiration(t *testing.T) {
	const directory = "https://wba.example.test"
	synctest.Test(t, func(t *testing.T) {
		key, err := webbotauth.GenerateEd25519KeyPair()
		if err != nil {
			t.Fatal(err)
		}
		signer, err := webbotauth.NewSigner(key.PublicKey, key.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodGet, "/page?run=1", nil)
		request.Host = "signed.example.test:8443"
		request.Header, err = signer.Sign(
			request,
			webbotauth.WithAuthority(false),
			webbotauth.WithMethod(false),
			webbotauth.WithPath(false),
			webbotauth.WithQuery(false),
			webbotauth.WithSignatureAgent(directory),
		)
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(30*time.Second - time.Millisecond)
		if err := verifyWBA(request, key.PublicKey, directory); err != nil {
			t.Fatalf("verify before expiry: %v", err)
		}
		time.Sleep(time.Millisecond)
		if err := verifyWBA(request, key.PublicKey, directory); err == nil || err.Error() != "expired signature" {
			t.Fatalf("verify at expiry = %v, want expired signature", err)
		}
	})
}
