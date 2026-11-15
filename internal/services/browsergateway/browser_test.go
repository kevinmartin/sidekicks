package browsergateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCDPAttachFailuresMapToHTTP(t *testing.T) {
	for _, tc := range []struct {
		name       string
		reply      *cdpError
		wait       bool
		wantStatus int
	}{
		{
			name: "missing target",
			reply: &cdpError{
				Code:    -32602,
				Message: "No target with given id found",
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "other protocol error",
			reply: &cdpError{
				Code:    -32000,
				Message: "Target does not support attaching",
			},
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "connection closes after attach",
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "deadline expires during attach",
			wait:       true,
			wantStatus: http.StatusGatewayTimeout,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attach := make(chan string, 1)
			chromium := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				socket, err := websocket.Accept(w, r, nil)
				if !assert.NoError(t, err) {
					return
				}
				defer func() {
					_ = socket.CloseNow()
				}()
				var request struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
				}
				if !assert.NoError(t, wsjson.Read(r.Context(), socket, &request)) {
					return
				}
				attach <- request.Method
				if tc.wait {
					// Keep the connection open until the client times out and closes it.
					_, _, _ = socket.Read(r.Context())
				}
				if tc.reply != nil {
					assert.NoError(t, wsjson.Write(r.Context(), socket, map[string]any{
						"id":    request.ID,
						"error": tc.reply,
					}))
				}
			}))
			defer chromium.Close()
			browser := &Browser{
				endpoint: "ws" + strings.TrimPrefix(chromium.URL, "http"),
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			result, err := browser.cdp(ctx, browserapi.CDPCommand{
				Tab:    "selected-tab",
				Method: "Page.getFrameTree",
			})
			require.Error(t, err)
			select {
			case method := <-attach:
				require.Equal(t, "Target.attachToTarget", method)
			default:
				t.Fatal("Chromium did not receive the attach request")
			}
			response := httptest.NewRecorder()
			writeResult(response, result, err)
			require.Equal(t, tc.wantStatus, response.Code, "%v", err)
		})
	}
}
