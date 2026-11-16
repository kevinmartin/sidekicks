package browsergateway

import (
	"context"
	"encoding/json"
	"fmt"
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
	fixture := &stagehandTestFixture{}
	browser := fixture.browser(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	command := browserapi.Command{
		Operation: "snapshot",
	}
	before, err := browser.command(ctx, command)
	require.NoError(t, err)
	fixture.disconnect.Store(true)
	_, err = browser.command(ctx, command)
	require.ErrorIs(t, err, stagehand.ErrCDPConnectionClosed)
	require.EqualValues(t, 1, fixture.inits.Load())
	require.Zero(t, fixture.stops.Load())
	after, err := browser.command(ctx, command)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.EqualValues(t, 2, fixture.inits.Load())
	require.EqualValues(t, 1, fixture.stops.Load())
}

func TestStagehandCommandRetriesWorkerDiscoveryAndReattaches(t *testing.T) {
	fixture := &stagehandTestFixture{}
	fixture.missingWorker.Store(true)
	fixture.lostAttachment.Store(true)
	browser := fixture.browser(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := browser.command(ctx, browserapi.Command{
		Operation: "snapshot",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, fixture.missingWorker.Load())
	require.False(t, fixture.lostAttachment.Load())
	require.EqualValues(t, 2, fixture.connections.Load())
	require.EqualValues(t, 1, fixture.inits.Load())
	require.Zero(t, fixture.stops.Load())
}

func TestStagehandCommandRecoversWhenWorkerRestartsWithSocketOpen(t *testing.T) {
	for _, tc := range []struct {
		name       string
		noLoopback bool
		wantError  string
	}{
		{
			name:      "detached CDP session",
			wantError: "Session with given id not found",
		},
		{
			name:       "reattached worker without Stagehand session",
			noLoopback: true,
			wantError:  "Stagehand loopback CDP is not configured",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &stagehandTestFixture{
				noLoopback: tc.noLoopback,
			}
			browser := fixture.browser(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			command := browserapi.Command{
				Operation: "snapshot",
			}
			before, err := browser.command(ctx, command)
			require.NoError(t, err)
			for attempt := range 3 {
				fixture.restarts.Add(1)
				_, err = browser.command(ctx, command)
				require.ErrorContains(t, err, tc.wantError)
				require.Equal(t, int32(attempt+1), fixture.inits.Load())

				after, err := browser.command(ctx, command)
				require.NoError(t, err)
				require.Equal(t, before, after)
				require.Equal(t, int32(attempt+2), fixture.connections.Load())
				require.Zero(t, fixture.stops.Load(), "the replacement worker should stay alive")
				require.Eventually(t, func() bool {
					return fixture.activeSockets.Load() == 1
				}, time.Second, time.Millisecond, "only the current SDK socket should remain")
			}
		})
	}
}

func TestStagehandDisconnectPreservesBufferedReply(t *testing.T) {
	fixture := &stagehandTestFixture{
		snapshotWaiting: make(chan struct{}),
	}
	browser := fixture.browser(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	command := browserapi.Command{
		Operation: "snapshot",
	}
	first := make(chan error, 1)
	go func() {
		_, err := browser.command(ctx, command)
		first <- err
	}()
	select {
	case <-fixture.snapshotWaiting:
	case <-ctx.Done():
		t.Fatal("Chromium did not receive the first snapshot request")
	}
	_, err := browser.command(ctx, command)
	require.ErrorIs(t, err, stagehand.ErrCDPConnectionClosed)
	select {
	case err := <-first:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("the first snapshot did not receive its buffered reply")
	}
}

type stagehandTestFixture struct {
	disconnect      atomic.Bool
	missingWorker   atomic.Bool
	lostAttachment  atomic.Bool
	connections     atomic.Int32
	activeSockets   atomic.Int32
	inits           atomic.Int32
	stops           atomic.Int32
	restarts        atomic.Int32
	snapshotWaiting chan struct{}
	noLoopback      bool
}

func (fixture *stagehandTestFixture) browser(t *testing.T) *Browser {
	t.Helper()
	ctx, stopFixture := context.WithCancel(context.WithoutCancel(t.Context()))
	chromium := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if !assert.NoError(t, err) {
			return
		}
		defer func() {
			_ = socket.CloseNow()
		}()
		var sdkConnection bool
		var sdkGeneration int32
		var pendingSnapshot uint64
		defer func() {
			if sdkConnection {
				fixture.activeSockets.Add(-1)
			}
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
			if err := wsjson.Read(ctx, socket, &request); err != nil {
				return
			}
			response := map[string]any{
				"id":     request.ID,
				"result": json.RawMessage(`{}`),
			}
			switch request.Method {
			case "Extensions.getExtensions":
				sdkConnection = true
				sdkGeneration = fixture.restarts.Load()
				fixture.connections.Add(1)
				fixture.activeSockets.Add(1)
				response["result"] = json.RawMessage(`{"extensions":[{"id":"` + StagehandExtensionID + `","name":"Stagehand Runtime","version":"1.2.0","path":"/stagehand","enabled":true}]}`)
			case "Target.getTargets":
				response["result"] = json.RawMessage(`{"targetInfos":[{"targetId":"worker","url":"` + stagehandWorkerURL + `","type":"service_worker"},{"targetId":"primary","type":"page"}]}`)
				if !sdkConnection && fixture.missingWorker.Swap(false) {
					response["result"] = json.RawMessage(`{"targetInfos":[]}`)
				}
			case "Runtime.getIsolateId":
				// The replacement can reuse the original CDP target ID.
				response["result"] = map[string]any{
					"id": fmt.Sprintf("worker-isolate-%d", fixture.stops.Load()+fixture.restarts.Load()),
				}
			case "Target.attachToTarget":
				response["result"] = json.RawMessage(`{"sessionId":"worker-session"}`)
			case "Target.closeTarget":
				assert.Equal(t, "worker", request.Params.TargetID)
				fixture.stops.Add(1)
				response["result"] = json.RawMessage(`{"success":true}`)
			case "Runtime.enable", "Runtime.addBinding":
			case "Runtime.evaluate":
				if strings.Contains(request.Params.Expression, "__stagehand_runtime") {
					response["result"] = json.RawMessage(`{"result":{"value":{"hasReceiver":true,"marker":{"protocolVersion":"3.0.0","serverInfo":{"name":"stagehand","version":"1.2.0"}}}}}`)
					break
				}
				stale := sdkConnection && sdkGeneration != fixture.restarts.Load()
				initDetached := strings.Contains(request.Params.Expression, `\"method\":\"stagehand.init\"`) && fixture.lostAttachment.Swap(false)
				if initDetached || (stale && !fixture.noLoopback) {
					delete(response, "result")
					response["error"] = &cdpError{
						Code:    -32001,
						Message: "Session with given id not found",
					}
					break
				}
				if !fixture.reply(ctx, t, socket, request.Session, request.Params.Expression, &pendingSnapshot, stale) {
					return
				}
				response["result"] = json.RawMessage(`{"result":{"value":true}}`)
			default:
				t.Errorf("unexpected CDP method %s", request.Method)
				return
			}
			if !assert.NoError(t, wsjson.Write(ctx, socket, response)) {
				return
			}
		}
	}))
	t.Cleanup(func() {
		stopFixture()
		chromium.Close()
	})
	browser := &Browser{
		endpoint: "ws" + strings.TrimPrefix(chromium.URL, "http"),
		primary:  "primary",
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		assert.NoError(t, browser.Close(cleanup))
	})
	return browser
}

func (fixture *stagehandTestFixture) reply(
	ctx context.Context,
	t *testing.T,
	socket *websocket.Conn,
	session, expression string,
	pendingSnapshot *uint64,
	stale bool,
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
	if stale {
		return replyStagehandRPC(ctx, t, socket, session, request.ID, "error", &cdpError{
			Code:    -32603,
			Message: "Stagehand loopback CDP is not configured",
		})
	}
	var result json.RawMessage
	switch request.Method {
	case "stagehand.init":
		assert.Equal(t, fixture.inits.Load(), fixture.stops.Load()+fixture.restarts.Load(), "the original worker must stop before reinitializing")
		fixture.inits.Add(1)
		result = json.RawMessage(`{"initialized":true,"pages":[]}`)
	case "context.pages":
		result = json.RawMessage(`[{"page_id":"primary"}]`)
	case "page.snapshot":
		if fixture.disconnect.Swap(false) {
			return false
		}
		result = json.RawMessage(`{"formatted_tree":"fixture","url_map":{},"xpath_map":{}}`)
		if fixture.snapshotWaiting != nil && fixture.stops.Load() == 0 {
			if *pendingSnapshot == 0 {
				// The first call receives its CDP acknowledgment immediately.
				// Withhold its RPC reply until a second call is in flight.
				*pendingSnapshot = request.ID
				close(fixture.snapshotWaiting)
				return true
			}
			replyStagehandRPC(ctx, t, socket, session, *pendingSnapshot, "result", result)
			return false
		}
	case "stagehand.close":
		result = json.RawMessage(`{"closed":true}`)
	default:
		t.Errorf("unexpected Stagehand method %s", request.Method)
		return false
	}
	return replyStagehandRPC(ctx, t, socket, session, request.ID, "result", result)
}

func replyStagehandRPC(ctx context.Context, t *testing.T, socket *websocket.Conn, session string, id uint64, kind string, value any) bool {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		kind:      value,
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
