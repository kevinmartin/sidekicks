package webbotauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type DirectoryResponse struct {
	Keys           []DirectoryKey `json:"keys"`
	SignatureAgent string         `json:"signature_agent"`
	Purpose        string         `json:"purpose"`
}

type DirectoryKey struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Nbf int64  `json:"nbf"`
	Exp int64  `json:"exp"`
}

func SignedDirectoryHandler(sigAgent, purpose string, keyPairs []Ed25519KeyPair) (http.HandlerFunc, error) {
	var signatures, sigInputs []string

	resp := DirectoryResponse{
		Keys:           []DirectoryKey{},
		SignatureAgent: sigAgent,
		Purpose:        purpose,
	}
	for _, kp := range keyPairs {
		kid, err := ComputeJWKThumbprint(kp.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("failed to compute JWK thumbprint: %w", err)
		}

		resp.Keys = append(resp.Keys, DirectoryKey{
			Kty: "OKP",
			Crv: "Ed25519",
			X:   base64.RawURLEncoding.EncodeToString(kp.PublicKey),
			Kid: kid,
			Use: "sig",
			Nbf: time.Now().Unix(),
			Exp: time.Now().Add(24 * time.Hour).Unix(),
		})
	}

	// Preserve the JSON encoding used for the signed content digest.
	jsonResp, err := json.Marshal(resp,
		jsontext.EscapeForHTML(true),
		jsontext.EscapeForJS(true),
		jsontext.AllowInvalidUTF8(true),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON response: %w", err)
	}

	contentDigest := sha256.Sum256(jsonResp)
	contentDigestHeader := fmt.Sprintf("sha-256=:%s:", base64.StdEncoding.EncodeToString(contentDigest[:]))

	for i, kp := range keyPairs {
		signer, err := NewSigner(kp.PublicKey, kp.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create signer for key %d: %w", i, err)
		}

		req, err := http.NewRequest(http.MethodGet, sigAgent+"/.well-known/http-message-signatures-directory", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create directory signature request: %w", err)
		}
		req.Header.Set("Content-Digest", contentDigestHeader)

		label := fmt.Sprintf("sig%d", i+1)
		headers, err := signer.Sign(
			req,
			WithAuthority(true),
			WithHeader("Content-Digest", false),
			WithSignatureLabel(label),
			WithTag("http-message-signatures-directory"),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to sign request for key %d: %w", i, err)
		}

		signatures = append(signatures, headers.Get("Signature"))
		sigInputs = append(sigInputs, headers.Get("Signature-Input"))
	}

	signatureHeader := strings.Join(signatures, ", ")
	signatureInputHeader := strings.Join(sigInputs, ", ")

	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Served %d keys with signature: %s, signature-input: %s", len(resp.Keys), signatureHeader, signatureInputHeader)

		w.Header().Set("Content-Type", "application/http-message-signatures-directory+json")
		w.Header().Set("Content-Digest", contentDigestHeader)
		w.Header().Set("Signature", signatureHeader)
		w.Header().Set("Signature-Input", signatureInputHeader)
		w.WriteHeader(http.StatusOK)
		w.Write(jsonResp)
	}, nil
}
