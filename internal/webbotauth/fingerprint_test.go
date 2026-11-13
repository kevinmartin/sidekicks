package webbotauth_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"strings"
	"testing"
)

type FingerprintResponse struct {
	Status  string `json:"status"`
	Details struct {
		Method    string            `json:"method"`
		URL       string            `json:"url"`
		Headers   map[string]string `json:"headers"`
		Timestamp string            `json:"timestamp"`
		KeyID     string            `json:"keyId"`
		KeySource string            `json:"keySource"`
	} `json:"details"`
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func decodeFingerprintResponse(body io.Reader) (FingerprintResponse, error) {
	var response FingerprintResponse
	err := json.UnmarshalDecode(jsontext.NewDecoder(body), &response)
	return response, err
}

func TestDecodeFingerprintResponse(t *testing.T) {
	const stream = `{"status":"error","details":{"keyId":"example-key"},"errors":[{"code":"example-error"}]}
{"status":"success"}`

	response, err := decodeFingerprintResponse(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "error" || response.Details.KeyID != "example-key" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if len(response.Errors) != 1 || response.Errors[0].Code != "example-error" {
		t.Fatalf("unexpected errors: %+v", response.Errors)
	}
}
