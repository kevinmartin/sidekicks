package webbotauth

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"os"
)

// Ed25519KeyPair holds the public and private keys used for signing.
type Ed25519KeyPair struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

// GenerateEd25519KeyPair creates a new Ed25519 key pair.
func GenerateEd25519KeyPair() (Ed25519KeyPair, error) {
	pubKey, privKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return Ed25519KeyPair{}, fmt.Errorf("failed to generate Ed25519 key pair: %w", err)
	}

	return Ed25519KeyPair{
		PublicKey:  pubKey,
		PrivateKey: privKey,
	}, nil
}

// ComputeJWKThumbprint returns the base64url-encoded RFC 7638 thumbprint of pubKey.
func ComputeJWKThumbprint(pubKey ed25519.PublicKey) (string, error) {
	// Keep these fields in lexicographic JSON name order for RFC 7638.
	canonicalJWK := struct {
		Crv string `json:"crv"`
		Kty string `json:"kty"`
		X   string `json:"x"`
	}{
		Crv: "Ed25519",
		Kty: "OKP",
		X:   base64.RawURLEncoding.EncodeToString(pubKey),
	}

	jwkJSON, err := json.Marshal(canonicalJWK)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JWK to JSON: %w", err)
	}

	hash := sha256.Sum256(jwkJSON)
	return base64.RawURLEncoding.EncodeToString(hash[:]), nil
}

// LoadPublicKeyFromFile reads a PEM-encoded Ed25519 public key.
func LoadPublicKeyFromFile(path string) (ed25519.PublicKey, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block: no valid PEM data found")
	}
	if block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("unexpected PEM block type: %s", block.Type)
	}

	parsedKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKIX public key: %w", err)
	}

	pubKey, ok := parsedKey.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is not an Ed25519 public key (got %T)", parsedKey)
	}

	return pubKey, nil
}

// LoadPrivateKeyFromFile reads a PEM-encoded Ed25519 private key.
func LoadPrivateKeyFromFile(path string) (ed25519.PrivateKey, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block: no valid PEM data found")
	}
	if block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("unexpected PEM block type: %s", block.Type)
	}

	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS#8 private key: %w", err)
	}

	privKey, ok := parsedKey.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not an Ed25519 private key (got %T)", parsedKey)
	}

	return privKey, nil
}
