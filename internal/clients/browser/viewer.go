package browser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
)

// OpenViewer opens an authenticated display and input stream for a tab.
func (c *Client) OpenViewer(ctx context.Context, tab string) (*websocket.Conn, error) {
	dial, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// The caller owns the stream lifetime. Bound only the HTTP upgrade.
	client := *c.http
	client.Timeout = 0
	address := c.endpoint + browserapi.ViewerPath + "?tab=" + url.QueryEscape(tab)
	socket, response, err := websocket.Dial(dial, address, &websocket.DialOptions{
		HTTPClient: &client,
		HTTPHeader: http.Header{
			"Authorization": {"Bearer " + c.token},
		},
	})
	if err != nil {
		if response != nil {
			defer func() {
				_ = response.Body.Close()
			}()
			message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
			if response.StatusCode == http.StatusNotFound && strings.TrimSpace(string(message)) == "404 page not found" {
				return nil, fmt.Errorf("browser guest does not support the viewer; start an instance with a viewer-enabled image")
			}
			return nil, fmt.Errorf("open guest viewer (%s): %s", response.Status, strings.TrimSpace(string(message)))
		}
		return nil, fmt.Errorf("open guest viewer: %w", err)
	}
	socket.SetReadLimit(browserapi.ViewerFrameLimit)
	return socket, nil
}
