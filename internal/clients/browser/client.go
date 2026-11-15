// Package browser provides a client for the Sidekicks guest browser API.
package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
)

var guestHTTP = &http.Client{
	Timeout:   30 * time.Second,
	Transport: &http.Transport{},
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// Client makes authenticated requests to a running browser guest.
type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

// New configures a client for the guest endpoint and its bearer token.
func New(endpoint, token string) *Client {
	client := guestHTTP
	if socket, ok := strings.CutPrefix(endpoint, "unix://"); ok {
		dialer := &net.Dialer{}
		client = &http.Client{
			Timeout:       guestHTTP.Timeout,
			CheckRedirect: guestHTTP.CheckRedirect,
			Transport: &http.Transport{
				DisableKeepAlives: true,
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return dialer.DialContext(ctx, "unix", socket)
				},
			},
		}
		endpoint = "http://sidekicks-guest"
	}
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		token:    token,
		http:     client,
	}
}

// Tabs returns the current page targets and the primary tab ID.
func (c *Client) Tabs(ctx context.Context) (browserapi.Tabs, error) {
	var result browserapi.Tabs
	data, err := c.request(ctx, http.MethodGet, "/v1/tabs", nil)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("decode browser tabs: %w", err)
	}
	if result.Primary == "" {
		return result, fmt.Errorf("guest returned no primary tab ID")
	}
	return result, nil
}

// Command runs the requested typed browser operation.
func (c *Client) Command(ctx context.Context, command browserapi.Command) (json.RawMessage, error) {
	return c.request(ctx, http.MethodPost, "/v1/command", command)
}

// CDP executes one CDP command and returns its result.
func (c *Client) CDP(ctx context.Context, command browserapi.CDPCommand) (json.RawMessage, error) {
	return c.request(ctx, http.MethodPost, "/v1/cdp", command)
}

// Screenshot returns PNG image bytes for the selected tab.
func (c *Client) Screenshot(ctx context.Context, tab string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, "/v1/screenshot?tab="+url.QueryEscape(tab), nil)
}

func (c *Client) request(ctx context.Context, method, path string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode browser request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to browser guest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		message, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		if err != nil {
			return nil, fmt.Errorf("read browser guest error (%s): %w", resp.Status, err)
		}
		return nil, fmt.Errorf("browser guest returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	const maxResponse = 64 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read browser guest response: %w", err)
	}
	if len(data) > maxResponse {
		return nil, fmt.Errorf("browser guest response exceeds %d bytes", maxResponse)
	}
	return data, nil
}
