package browsergateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func (g *Gateway) browserSocket(ctx context.Context) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.discoveryURL, nil)
	if err != nil {
		return "", err
	}
	response, err := g.client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("browser discovery returned %s", response.Status)
	}
	var version struct {
		Socket string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&version); err != nil {
		return "", fmt.Errorf("decode Chromium discovery: %w", err)
	}
	endpoint, err := url.Parse(version.Socket)
	upstream, _ := url.Parse(g.discoveryURL)
	if err != nil || endpoint.Scheme != "ws" || endpoint.Host != upstream.Host || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" || !strings.HasPrefix(endpoint.Path, "/devtools/browser/") ||
		!browserSocketID.MatchString(strings.TrimPrefix(endpoint.Path, "/devtools/browser/")) {
		return "", fmt.Errorf("browser discovery returned an invalid socket")
	}
	return endpoint.String(), nil
}

type cdpConnection struct {
	socket   *websocket.Conn
	sequence int
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string {
	return fmt.Sprintf("(%d): %s", e.Code, e.Message)
}

func (c *cdpConnection) close() {
	_ = c.socket.CloseNow()
}

func openCDP(ctx context.Context, endpoint string) (*cdpConnection, error) {
	connection, _, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to Chromium CDP: %w", err)
	}
	connection.SetReadLimit(64 << 20)
	return &cdpConnection{
		socket: connection,
	}, nil
}

// CloseChromium asks Chromium to persist its profile and exit. Its socket can
// close before a reply arrives. The guest waits for the child process to exit.
func (g *Gateway) CloseChromium(ctx context.Context) {
	endpoint, err := g.browserSocket(ctx)
	if err != nil {
		return
	}
	connection, err := openCDP(ctx, endpoint)
	if err != nil {
		return
	}
	defer connection.close()
	_ = connection.call(ctx, "", "Browser.close", nil, nil)
}

func (c *cdpConnection) call(ctx context.Context, session, method string, params, result any) error {
	c.sequence++
	request := struct {
		ID      int    `json:"id"`
		Session string `json:"sessionId,omitempty"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		ID:      c.sequence,
		Session: session,
		Method:  method,
		Params:  params,
	}
	if err := wsjson.Write(ctx, c.socket, request); err != nil {
		return fmt.Errorf("send %s: %w", method, err)
	}
	for {
		var reply struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *cdpError       `json:"error"`
		}
		if err := wsjson.Read(ctx, c.socket, &reply); err != nil {
			return fmt.Errorf("read %s: %w", method, err)
		}
		if reply.ID != request.ID {
			continue
		}
		if reply.Error != nil {
			return fmt.Errorf("%s %w", method, reply.Error)
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(reply.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
		return nil
	}
}
