package browsergateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	stagehand "github.com/browserbase/stagehand/packages/sdk-go/v4"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStagehandCommandRecoversAfterTransportDisconnect(t *testing.T) {
	fixture, stopFixture := context.WithCancel(t.Context())
	var disconnect atomic.Bool
	var inits, stops atomic.Int32
	chromium := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if !assert.NoError(t, err) {
			return
		}
		defer func() {
			_ = socket.CloseNow()
		}()
		for {
			var request struct {
				ID      int    `json:"id"`
				Method  string `json:"method"`
				Session string `json:"sessionId"`
				Params  struct {
					Expression string `json:"expression"`
					TargetID   string `json:"targetId"`
				} `json:"params"`
			}
			if err := wsjson.Read(fixture, socket, &request); err != nil {
				return
			}
			result := json.RawMessage(`{}`)
			switch request.Method {
			case "Extensions.getExtensions":
				result = json.RawMessage(`{"extensions":[{"id":"` + StagehandExtensionID + `","name":"Stagehand Runtime","version":"1.2.0","path":"/stagehand","enabled":true}]}`)
			case "Target.getTargets":
				result = json.RawMessage(`{"targetInfos":[{"targetId":"worker","url":"` + stagehandWorkerURL + `","type":"service_worker"},{"targetId":"primary","type":"page"}]}`)
			case "Runtime.getIsolateId":
				isolate := "original-isolate"
				if stops.Load() != 0 {
					isolate = "replacement-isolate"
				}
				// The replacement can reuse the original CDP target ID.
				result = json.RawMessage(`{"id":"` + isolate + `"}`)
			case "Target.attachToTarget":
				result = json.RawMessage(`{"sessionId":"worker-session"}`)
			case "Target.closeTarget":
				assert.Equal(t, "worker", request.Params.TargetID)
				stops.Add(1)
				result = json.RawMessage(`{"success":true}`)
			case "Runtime.enable", "Runtime.addBinding":
			case "Runtime.evaluate":
				if strings.Contains(request.Params.Expression, "__stagehand_runtime") {
					result = json.RawMessage(`{"result":{"value":{"hasReceiver":true,"marker":{"protocolVersion":"3.0.0","serverInfo":{"name":"stagehand","version":"1.2.0"}}}}}`)
					break
				}
				if !replyToStagehandRPC(fixture, t, socket, request.Session, request.Params.Expression, &disconnect, &inits, &stops) {
					return
				}
				result = json.RawMessage(`{"result":{"value":true}}`)
			default:
				t.Errorf("unexpected CDP method %s", request.Method)
				return
			}
			if !assert.NoError(t, wsjson.Write(fixture, socket, map[string]any{
				"id":     request.ID,
				"result": result,
			})) {
				return
			}
		}
	}))
	defer func() {
		stopFixture()
		chromium.Close()
	}()
	browser := &Browser{
		endpoint: "ws" + strings.TrimPrefix(chromium.URL, "http"),
		primary:  "primary",
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	defer func() {
		assert.NoError(t, browser.Close(ctx))
	}()
	command := browserapi.Command{
		Operation: "snapshot",
	}
	before, err := browser.command(ctx, command)
	require.NoError(t, err)
	disconnect.Store(true)
	_, err = browser.command(ctx, command)
	require.ErrorIs(t, err, stagehand.ErrCDPConnectionClosed)
	require.EqualValues(t, 1, inits.Load())
	require.Zero(t, stops.Load())
	after, err := browser.command(ctx, command)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.EqualValues(t, 2, inits.Load())
	require.EqualValues(t, 1, stops.Load())
}

func replyToStagehandRPC(
	ctx context.Context,
	t *testing.T,
	socket *websocket.Conn,
	session, expression string,
	disconnect *atomic.Bool,
	inits, stops *atomic.Int32,
) bool {
	t.Helper()
	encoded := strings.TrimSuffix(strings.TrimPrefix(expression, "void globalThis.__stagehandReceiveFromHost("), "); true")
	var payload string
	if !assert.NoError(t, json.Unmarshal([]byte(encoded), &payload)) {
		return false
	}
	var request struct {
		ID     uint64 `json:"id"`
		Method string `json:"method"`
	}
	if !assert.NoError(t, json.Unmarshal([]byte(payload), &request)) {
		return false
	}
	var result json.RawMessage
	switch request.Method {
	case "stagehand.init":
		assert.Equal(t, inits.Load(), stops.Load(), "the original worker must stop before reinitializing")
		inits.Add(1)
		result = json.RawMessage(`{"initialized":true,"pages":[]}`)
	case "context.pages":
		result = json.RawMessage(`[{"page_id":"primary"}]`)
	case "page.snapshot":
		if disconnect.Swap(false) {
			return false
		}
		result = json.RawMessage(`{"formatted_tree":"fixture","url_map":{},"xpath_map":{}}`)
	case "stagehand.close":
		result = json.RawMessage(`{"closed":true}`)
	default:
		t.Errorf("unexpected Stagehand method %s", request.Method)
		return false
	}
	data, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      request.ID,
		"result":  result,
	})
	if !assert.NoError(t, err) {
		return false
	}
	return assert.NoError(t, wsjson.Write(ctx, socket, map[string]any{
		"method":    "Runtime.bindingCalled",
		"sessionId": session,
		"params": map[string]any{
			"name":               "__stagehandSendToHost",
			"payload":            string(data),
			"executionContextId": 1,
		},
	}))
}
