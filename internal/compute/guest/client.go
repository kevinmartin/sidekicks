package guest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MaxTransportRequestBytes limits the encoded local request body.
const MaxTransportRequestBytes = 32 << 10

// MaxTransportResponseBytes limits the local response body.
const MaxTransportResponseBytes = 2 << 20

// LocalRequest is the one-shot stdio transport invoked through the runtime's
// exec API. Body uses standard JSON []byte base64 encoding. No token is passed
// in argv, and no unauthenticated network listener is published to the host.
type LocalRequest struct {
	Method         string `json:"method"`
	Path           string `json:"path"`
	Body           []byte `json:"body,omitempty"`
	RequestID      string `json:"requestId,omitempty"`
	DeadlineUnixMS int64  `json:"deadlineUnixMs,omitempty"`
}

// LocalResponse contains the status and response body returned by the guest.
type LocalResponse struct {
	Status int    `json:"status"`
	Body   []byte `json:"body"`
}

// DecodeLocalRequest decodes and validates a bounded local transport request.
func DecodeLocalRequest(encoded string) (LocalRequest, error) {
	var req LocalRequest
	if len(encoded) > base64.StdEncoding.EncodedLen(MaxTransportRequestBytes) {
		return req, errors.New("encoded request exceeds transport limit")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return req, errors.New("invalid base64 request")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&req); err != nil {
		return req, errors.New("invalid request json")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return req, errors.New("one request value required")
	}
	if err = validateLocalRequest(req); err != nil {
		return req, err
	}
	return req, nil
}

func validateLocalRequest(req LocalRequest) error {
	if req.RequestID != "" || req.DeadlineUnixMS != 0 {
		if err := validRequestMetadata(req.RequestID, req.DeadlineUnixMS, time.Now()); err != nil {
			return err
		}
	}
	u, err := url.ParseRequestURI(req.Path)
	if err != nil || u.IsAbs() || u.Host != "" || u.RawPath != "" || strings.ContainsAny(req.Path, "\r\n\x00") || len(req.Path) > 2048 {
		return errors.New("invalid local request path")
	}
	switch {
	case u.Path == "/health" && req.Method == http.MethodGet && u.RawQuery == "":
	case (u.Path == "/exec" || u.Path == "/cancel") && req.Method == http.MethodPost && u.RawQuery == "":
	case u.Path == "/files" && (req.Method == http.MethodGet || req.Method == http.MethodPut):
	default:
		if u.RawQuery != "" {
			return errors.New("unsupported local request route")
		}
		for _, method := range extraMethods(u.Path) {
			if req.Method == method {
				return nil
			}
		}
		return errors.New("unsupported local request route")
	}
	return nil
}

// CallLocal sends an authenticated request to the local guest service.
func CallLocal(ctx context.Context, client *http.Client, token string, request LocalRequest) (LocalResponse, error) {
	var result LocalResponse
	if err := validateLocalRequest(request); err != nil {
		return result, err
	}
	if len(token) < 32 || len(token) > 256 || strings.ContainsAny(token, " \t\r\n") {
		return result, errors.New("invalid local agent configuration")
	}
	if len(request.Body) > MaxTransportRequestBytes {
		return result, errors.New("request body exceeds transport limit")
	}
	if client == nil {
		client = &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 65 * time.Second}
	}
	if request.RequestID != "" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, time.UnixMilli(request.DeadlineUnixMS))
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, request.Method, "http://127.0.0.1:8080"+request.Path, strings.NewReader(string(request.Body)))
	if err != nil {
		return result, errors.New("invalid local http request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if request.RequestID != "" {
		req.Header.Set("X-Sidekicks-Request-ID", request.RequestID)
		req.Header.Set("X-Sidekicks-Deadline", strconv.FormatInt(request.DeadlineUnixMS, 10))
	}
	if request.Method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	localClient := *client
	localClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := localClient.Do(req)
	if err != nil {
		return result, errors.New("local agent request failed")
	}
	defer response.Body.Close()
	result.Status = response.StatusCode
	result.Body, err = io.ReadAll(io.LimitReader(response.Body, MaxTransportResponseBytes+1))
	if err != nil {
		return LocalResponse{}, errors.New("local response read failed")
	}
	if len(result.Body) > MaxTransportResponseBytes {
		return LocalResponse{}, errors.New("local response exceeds transport limit")
	}
	return result, nil
}
