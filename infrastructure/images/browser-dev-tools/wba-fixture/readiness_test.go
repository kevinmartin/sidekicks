package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"
)

func TestTabReadinessLease(t *testing.T) {
	synctest.Test(t, testTabReadinessLease)
}

func testTabReadinessLease(t *testing.T) {
	f := &fixture{ready: make(map[string]tabReadiness)}
	observer := f.observer("test-token")

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
		if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil ||
			recorder.Code != http.StatusOK || status.Ready == nil || *status.Ready != step.ready ||
			status.Retired == nil || *status.Retired != step.retired {
			t.Fatalf("%s readiness = HTTP %d %s; want ready=%t, retired=%t",
				step.message, recorder.Code, recorder.Body, step.ready, step.retired)
		}
	}

	staleReady := time.Now()
	time.Sleep(readinessLease)
	f.expireReadiness(time.Now())
	if len(f.ready) != 0 {
		t.Fatal("expired retirement is still stored")
	}
	checkReadinessMessage(t, f, "ready", staleReady, false)
	if len(f.ready) != 0 {
		t.Fatal("stale ready message recreated an expired retirement")
	}

	checkReadinessMessage(t, f, "ready", time.Now(), true)
	time.Sleep(readinessLease)
	f.expireReadiness(time.Now())
	if len(f.ready) != 0 {
		t.Fatal("expired active lease is still stored")
	}
}

func checkReadinessMessage(t *testing.T, f *fixture, message string, sentAt time.Time, wantOK bool) {
	t.Helper()
	client, server := net.Pipe()
	defer client.Close()
	go f.serveBroker(server)
	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(client).Encode(map[string]any{
		"id":       "request",
		"type":     message,
		"targetId": "tab",
		"sentAtMs": sentAt.UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	var response struct {
		OK      bool  `json:"ok"`
		LeaseMS int64 `json:"leaseMs"`
	}
	if err := json.NewDecoder(client).Decode(&response); err != nil || response.OK != wantOK {
		t.Fatalf("%s response = %+v, %v; want ok=%t", message, response, err, wantOK)
	}
	if message == "ready" && wantOK && response.LeaseMS != readinessLease.Milliseconds() {
		t.Fatalf("ready lease = %dms; want %dms", response.LeaseMS, readinessLease.Milliseconds())
	}
}
