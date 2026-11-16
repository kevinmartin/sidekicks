package browsergateway

import (
	"context"
	"encoding/json"
	"errors"
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

type cdpTarget struct {
	ID    string `json:"targetId"`
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func (c *cdpConnection) targets(ctx context.Context) ([]cdpTarget, error) {
	var targets struct {
		List []cdpTarget `json:"targetInfos"`
	}
	if err := c.call(ctx, "", "Target.getTargets", nil, &targets); err != nil {
		return nil, err
	}
	return targets.List, nil
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

type cdpMessage struct {
	ID      int             `json:"id"`
	Session string          `json:"sessionId"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *cdpError       `json:"error"`
}

func (c *cdpConnection) send(ctx context.Context, session, method string, params any) (int, error) {
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
		return 0, fmt.Errorf("send %s: %w", method, err)
	}
	return request.ID, nil
}

func (c *cdpConnection) receive(ctx context.Context) (cdpMessage, error) {
	var message cdpMessage
	err := wsjson.Read(ctx, c.socket, &message)
	return message, err
}

func (c *cdpConnection) call(ctx context.Context, session, method string, params, result any) error {
	id, err := c.send(ctx, session, method, params)
	if err != nil {
		return err
	}
	for {
		reply, err := c.receive(ctx)
		if err != nil {
			return fmt.Errorf("read %s: %w", method, err)
		}
		if reply.ID != id {
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

func (c *cdpConnection) attachTab(ctx context.Context, id string) (string, error) {
	var session struct {
		ID string `json:"sessionId"`
	}
	err := c.call(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": id,
		"flatten":  true,
	}, &session)
	var protocolErr *cdpError
	if errors.As(err, &protocolErr) && protocolErr.Message == "No target with given id found" {
		return "", fmt.Errorf("%w: %q", errUnknownTab, id)
	}
	if err != nil {
		return "", fmt.Errorf("attach to tab %q: %w", id, err)
	}
	return session.ID, nil
}
