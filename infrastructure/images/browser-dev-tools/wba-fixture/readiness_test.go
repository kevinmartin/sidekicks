package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

const readinessLease = 12 * time.Second

func TestTabReadinessLease(t *testing.T) {
	synctest.Test(t, testTabReadinessLease)
}

func testTabReadinessLease(t *testing.T) {
	f := &fixture{
		config: config{
			Token:          "test-token",
			ReadinessLease: readinessLease,
			BrokerTimeout:  2 * time.Second,
		},
		ready: make(map[string]tabReadiness),
	}
	observer := f.observer()

	for _, step := range []struct {
		message string
		after   time.Duration
		ok      bool
		ready   bool
		retired bool
	}{
		{
			message: "ready",
			ok:      true,
			ready:   true,
		},
		{
			message: "ready",
			after:   readinessLease * 3 / 4,
			ok:      true,
			ready:   true,
		},
		{
			after: readinessLease / 2,
			ready: true,
		},
		{
			after: readinessLease / 2,
			ready: false,
		},
		{
			message: "ready",
			ok:      true,
			ready:   true,
		},
		{
			message: "retire",
			ok:      true,
			ready:   false,
			retired: true,
		},
		{
			message: "ready",
			ok:      false,
			ready:   false,
			retired: true,
		},
		{
			message: "retire",
			ok:      true,
			ready:   false,
			retired: true,
		},
	} {
		// Advance the synctest clock to exercise lease boundaries.
		time.Sleep(step.after)
		f.expireReadiness(time.Now())
		if step.message != "" {
			checkReadinessMessage(t, f, step.message, time.Now(), step.ok)
		}

		request := httptest.NewRequest(http.MethodGet, "/readiness?targetId=tab", nil)
		request.Header.Set("Authorization", "Bearer test-token")
		recorder := httptest.NewRecorder()
		observer.ServeHTTP(recorder, request)
		var status struct {
			Ready   *bool `json:"ready"`
			Retired *bool `json:"retired"`
		}
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &status))
		require.NotNil(t, status.Ready, "readiness response must include ready")
		require.NotNil(t, status.Retired, "readiness response must include retired")
		require.Equal(t, step.ready, *status.Ready, "ready after %q", step.message)
		require.Equal(t, step.retired, *status.Retired, "retired after %q", step.message)
	}

	staleReady := time.Now()
	time.Sleep(readinessLease)
	f.expireReadiness(time.Now())
	require.Empty(t, f.ready, "expired retirement is still stored")
	checkReadinessMessage(t, f, "ready", staleReady, false)
	require.Empty(t, f.ready, "stale ready message recreated an expired retirement")

	checkReadinessMessage(t, f, "ready", time.Now(), true)
	time.Sleep(readinessLease)
	f.expireReadiness(time.Now())
	require.Empty(t, f.ready, "expired active lease is still stored")
}

func checkReadinessMessage(t *testing.T, f *fixture, message string, sentAt time.Time, wantOK bool) {
	t.Helper()
	client, server := net.Pipe()
	ctx := t.Context()
	var broker sync.WaitGroup
	defer broker.Wait()
	defer client.Close()
	broker.Go(func() {
		defer func() {
			if panicValue := recover(); panicValue != nil {
				t.Errorf("broker panic: %v\n%s", panicValue, debug.Stack())
			}
		}()
		if err := f.serveBroker(ctx, server); err != nil {
			t.Errorf("serve broker: %v", err)
		}
	})
	require.NoError(t, client.SetDeadline(time.Now().Add(time.Second)))
	require.NoError(t, json.NewEncoder(client).Encode(map[string]any{
		"id":       "request",
		"type":     message,
		"targetId": "tab",
		"sentAtMs": sentAt.UnixMilli(),
	}))
	var response struct {
		OK      bool  `json:"ok"`
		LeaseMS int64 `json:"leaseMs"`
	}
	require.NoError(t, json.NewDecoder(client).Decode(&response), "decode %s reply", message)
	require.Equal(t, wantOK, response.OK, "ok in %s reply", message)
	if message == "ready" && wantOK {
		require.Equal(t, readinessLease.Milliseconds(), response.LeaseMS)
	}
}
