package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kevinmartin/sidekicks/internal/lib/webbotauth"
	"github.com/stretchr/testify/require"
)

func TestVerifyWBAExpiration(t *testing.T) {
	const directory = "https://wba.example.test"
	synctest.Test(t, func(t *testing.T) {
		key, err := webbotauth.GenerateEd25519KeyPair()
		require.NoError(t, err)
		signer, err := webbotauth.NewSigner(key.PublicKey, key.PrivateKey)
		require.NoError(t, err)
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
		require.NoError(t, err)

		time.Sleep(30*time.Second - time.Millisecond)
		require.NoError(t, verifyWBA(request, key.PublicKey, directory), "verify before expiry")
		time.Sleep(time.Millisecond)
		require.EqualError(t, verifyWBA(request, key.PublicKey, directory), "expired signature")
	})
}
