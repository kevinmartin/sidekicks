package webbotauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Signer creates HTTP message signatures with an Ed25519 key pair.
type Signer struct {
	pubKey  ed25519.PublicKey
	privKey ed25519.PrivateKey
	kid     string
	expiry  time.Duration
}

// NewSigner creates a signer whose key ID is the public key's JWK thumbprint.
func NewSigner(pubKey ed25519.PublicKey, privKey ed25519.PrivateKey) (*Signer, error) {
	kid, err := ComputeJWKThumbprint(pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute JWK thumbprint: %w", err)
	}

	return &Signer{
		pubKey:  pubKey,
		privKey: privKey,
		kid:     kid,
		expiry:  30 * time.Second,
	}, nil
}

type signerMessageComponent struct {
	value   string
	request bool
}

// SignerMessage holds the components and parameters for a signature.
type SignerMessage struct {
	label             string
	httpFields        map[string]signerMessageComponent
	derivedComponents map[string]signerMessageComponent
	parameters        map[string]string
}

// SignerMessageOption configures a signature from an HTTP request.
type SignerMessageOption func(sm *SignerMessage, req *http.Request)

// WithHeader covers the named header from the supplied request.
func WithHeader(key string, request bool) SignerMessageOption {
	return func(sm *SignerMessage, req *http.Request) {
		lowerKey := strings.ToLower(key)
		sm.httpFields[lowerKey] = signerMessageComponent{
			value:   req.Header.Get(key),
			request: request,
		}
	}
}

// WithAuthority covers the request authority.
func WithAuthority(request bool) SignerMessageOption {
	return func(sm *SignerMessage, req *http.Request) {
		sm.derivedComponents["@authority"] = signerMessageComponent{
			value:   req.Host,
			request: request,
		}
	}
}

// WithTargetURI covers the request target URI.
func WithTargetURI(request bool) SignerMessageOption {
	return func(sm *SignerMessage, req *http.Request) {
		sm.derivedComponents["@target-uri"] = signerMessageComponent{
			value:   req.URL.String(),
			request: request,
		}
	}
}

// WithMethod covers the HTTP method.
func WithMethod(request bool) SignerMessageOption {
	return func(sm *SignerMessage, req *http.Request) {
		sm.derivedComponents["@method"] = signerMessageComponent{
			value:   req.Method,
			request: request,
		}
	}
}

// WithPath covers the escaped request path, using "/" for an empty path.
func WithPath(request bool) SignerMessageOption {
	return func(sm *SignerMessage, req *http.Request) {
		path := req.URL.EscapedPath()
		if path == "" {
			path = "/"
		}
		sm.derivedComponents["@path"] = signerMessageComponent{
			value:   path,
			request: request,
		}
	}
}

// WithScheme covers the request URL scheme.
func WithScheme(request bool) SignerMessageOption {
	return func(sm *SignerMessage, req *http.Request) {
		sm.derivedComponents["@scheme"] = signerMessageComponent{
			value:   req.URL.Scheme,
			request: request,
		}
	}
}

// WithQuery covers the raw query string, including its leading question mark.
func WithQuery(request bool) SignerMessageOption {
	return func(sm *SignerMessage, req *http.Request) {
		sm.derivedComponents["@query"] = signerMessageComponent{
			value:   "?" + req.URL.RawQuery,
			request: request,
		}
	}
}

// WithNonce adds a base64url nonce generated from byteLength random bytes.
func WithNonce(byteLength int) SignerMessageOption {
	return func(sm *SignerMessage, req *http.Request) {
		b := make([]byte, byteLength)
		if _, err := rand.Read(b); err != nil {
			return
		}

		sm.parameters["nonce"] = base64.RawURLEncoding.EncodeToString(b)
	}
}

// WithTag sets the signature's application tag.
func WithTag(tag string) SignerMessageOption {
	return func(sm *SignerMessage, req *http.Request) {
		sm.parameters["tag"] = tag
	}
}

// WithSignatureLabel sets the label used in the signature headers.
func WithSignatureLabel(label string) SignerMessageOption {
	return func(sm *SignerMessage, req *http.Request) {
		sm.label = label
	}
}

// WithSignatureAgent sets and covers the Signature-Agent header.
func WithSignatureAgent(signatureAgent string) SignerMessageOption {
	return func(sm *SignerMessage, req *http.Request) {
		sm.derivedComponents["signature-agent"] = signerMessageComponent{
			value: fmt.Sprintf(`"%s"`, signatureAgent),
		}
	}
}

// Sign returns the signature headers for req using the selected components.
func (s *Signer) Sign(req *http.Request, sms ...SignerMessageOption) (http.Header, error) {
	sm := &SignerMessage{
		label:             "sig1",
		httpFields:        make(map[string]signerMessageComponent),
		derivedComponents: make(map[string]signerMessageComponent),
		parameters:        make(map[string]string),
	}
	for _, smsOption := range sms {
		smsOption(sm, req)
	}

	sm.parameters["created"] = strconv.FormatInt(time.Now().Unix(), 10)
	sm.parameters["expires"] = strconv.FormatInt(time.Now().Add(s.expiry).Unix(), 10)
	sm.parameters["keyid"] = s.kid
	sm.parameters["alg"] = "ed25519"

	var sigInputComponents, sigInputValues []string
	for k, v := range sm.derivedComponents {
		param := ""
		if v.request {
			param = ";req"
		}

		sigInputComponents = append(sigInputComponents, fmt.Sprintf(`"%s"%s`, k, param))
		sigInputValues = append(sigInputValues, fmt.Sprintf(`"%s"%s: %s`, k, param, v.value))
	}
	for k, v := range sm.httpFields {
		param := ""
		if v.request {
			param = ";req"
		}

		sigInputComponents = append(sigInputComponents, fmt.Sprintf(`"%s"%s`, k, param))
		sigInputValues = append(sigInputValues, fmt.Sprintf(`"%s"%s: %s`, k, param, v.value))
	}

	sigInput := fmt.Sprintf("(%s)", strings.Join(sigInputComponents, " "))
	for k, v := range sm.parameters {
		if k == "created" || k == "expires" {
			sigInput += fmt.Sprintf(";%s=%s", k, v)
		} else {
			sigInput += fmt.Sprintf(`;%s="%s"`, k, v)
		}
	}

	sigInputValues = append(sigInputValues, fmt.Sprintf(`"@signature-params": %s`, sigInput))

	signatureBytes := ed25519.Sign(s.privKey, []byte(strings.Join(sigInputValues, "\n")))
	signatureB64 := base64.StdEncoding.EncodeToString(signatureBytes)

	headers := http.Header{}
	headers.Set("Signature-Input", fmt.Sprintf("%s=%s", sm.label, sigInput))
	headers.Set("Signature", fmt.Sprintf("%s=:%s:", sm.label, signatureB64))
	if signatureAgent, ok := sm.derivedComponents["signature-agent"]; ok {
		headers.Set("Signature-Agent", signatureAgent.value)
	}

	return headers, nil
}
