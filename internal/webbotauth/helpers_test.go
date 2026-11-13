package webbotauth_test

import (
	"encoding/base64"
	"testing"

	"github.com/kevinmartin/sidekicks/internal/webbotauth"
)

func TestComputeJWKThumbprint(t *testing.T) {
	// RFC 8037 Appendix A.3 provides the Ed25519 test vector.
	publicKey, err := base64.RawURLEncoding.DecodeString("11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo")
	if err != nil {
		t.Fatal(err)
	}

	got, err := webbotauth.ComputeJWKThumbprint(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	const want = "kPrK_qmxVWaYVA9wwBF6Iuo3vVzz7TxHCTwXBygrS4k"
	if got != want {
		t.Fatalf("thumbprint = %q, want %q", got, want)
	}
}
