package guest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const cancellationID = "0123456789abcdef0123456789abcdef"

func trackedHTTP(s *Server, method, path string, body []byte, id string, deadline int64) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Sidekicks-Request-ID", id)
	request.Header.Set("X-Sidekicks-Deadline", strconv.FormatInt(deadline, 10))
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	return response
}
func cancellationBody(id string) []byte {
	raw, _ := json.Marshal(map[string]string{"requestId": id})
	return raw
}

// TestTransportCancellationAcknowledgesCommandGroupCleanup verifies that transport cancellation acknowledges command-group cleanup.
func TestTransportCancellationAcknowledgesCommandGroupCleanup(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{Workspace: dir, Token: testToken, ExecGate: func(_ context.Context, run func(context.Context) error) error {
		return run(context.Background())
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- trackedHTTP(s, "POST", "/exec", []byte(`{"argv":["sh","-c","sleep 30 & echo $! > child.pid; wait"],"timeoutMs":30000}`), cancellationID, time.Now().Add(time.Minute).UnixMilli())
	}()
	pid := waitPID(t, filepath.Join(dir, "child.pid"))
	response := call(s, "POST", "/cancel", cancellationBody(cancellationID))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"ok":true`) || !strings.Contains(response.Body.String(), `"canceled":true`) {
		t.Fatal("cancellation was not confirmed", response.Code, response.Body)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel acknowledged before handler cleanup")
	}
	assertNotRunning(t, pid)
	if response = call(s, "POST", "/cancel", cancellationBody(cancellationID)); response.Code != 200 {
		t.Fatal("repeated cancellation was not idempotent", response.Code)
	}
}

// TestCancelBeforeStartAndCompletedReplayCannotExecute verifies that cancellation before start and completed replay cannot execute.
func TestCancelBeforeStartAndCompletedReplayCannotExecute(t *testing.T) {
	s, dir := newServer(t)
	deadline := time.Now().Add(time.Minute).UnixMilli()
	if response := call(s, "POST", "/cancel", cancellationBody(cancellationID)); response.Code != 200 {
		t.Fatal(response.Code)
	}
	body := []byte(`{"argv":["sh","-c","printf forbidden > proof"]}`)
	if response := trackedHTTP(s, "POST", "/exec", body, cancellationID, deadline); response.Code != 409 {
		t.Fatal("pre-canceled command started", response.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "proof")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pre-canceled command executed")
	}
	next := "1123456789abcdef0123456789abcdef"
	if response := trackedHTTP(s, "POST", "/exec", []byte(`{"argv":["sh","-c","printf x >> once"]}`), next, deadline); response.Code != 200 {
		t.Fatal(response.Code, response.Body)
	}
	if response := trackedHTTP(s, "POST", "/exec", []byte(`{"argv":["sh","-c","printf x >> once"]}`), next, deadline); response.Code != 409 {
		t.Fatal("completed request replay accepted")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "once"))
	if string(raw) != "x" {
		t.Fatal("replay executed more than once")
	}
}

// TestDelayedRequestExpiresAfterTombstonePruning verifies that a delayed request expires after its tombstone is pruned.
func TestDelayedRequestExpiresAfterTombstonePruning(t *testing.T) {
	s, _ := newServer(t)
	now := time.Now()
	s.requests.now = func() time.Time {
		return now
	}
	oldDeadline := now.Add(time.Minute).UnixMilli()
	if response := call(s, "POST", "/cancel", cancellationBody(cancellationID)); response.Code != 200 {
		t.Fatal(response.Code)
	}
	now = now.Add(cancellationTTL + time.Second)
	if response := trackedHTTP(s, "POST", "/health", nil, cancellationID, oldDeadline); response.Code != 409 {
		t.Fatal("expired delayed transport was accepted", response.Code)
	}
	if len(s.requests.entries) != 0 {
		t.Fatal("expired tombstone was not pruned")
	}
}

// TestCancellationStoreBoundsAndExistingActiveCancellation verifies cancellation-store bounds and cancellation of an active request.
func TestCancellationStoreBoundsAndExistingActiveCancellation(t *testing.T) {
	store := newRequestStore()
	now := time.Now()
	store.now = func() time.Time {
		return now
	}
	ctx, complete, err := store.begin(context.Background(), cancellationID, now.Add(time.Minute).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	for index := range maxTrackedRequests - 1 {
		if _, _, err = store.cancel(fmt.Sprintf("%032x", index)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = store.cancel(strings.Repeat("f", 32)); !errors.Is(err, errRequestCapacity) {
		t.Fatal("tombstone store is unbounded")
	}
	if _, _, err = store.begin(context.Background(), strings.Repeat("e", 32), now.Add(time.Minute).UnixMilli()); !errors.Is(err, errRequestCapacity) {
		t.Fatal("active registry is unbounded")
	}
	done, active, err := store.cancel(cancellationID)
	if err != nil || !active || ctx.Err() == nil {
		t.Fatal("full registry lost an existing cancellation")
	}
	select {
	case <-done:
		t.Fatal("acknowledged before active handler completed")
	default:
	}
	complete()
	select {
	case <-done:
	default:
		t.Fatal("completion was not acknowledged")
	}
	now = now.Add(cancellationTTL + time.Second)
	if _, _, err = store.cancel(strings.Repeat("f", 32)); err != nil || len(store.entries) != 1 {
		t.Fatal("expired capacity not reclaimed", err, len(store.entries))
	}
	store.close()
	if _, _, err = store.begin(context.Background(), strings.Repeat("d", 32), now.Add(time.Minute).UnixMilli()); !errors.Is(err, errRequestRejected) {
		t.Fatal("closed store accepted work")
	}
}

// TestCompletedRequestsRemainBoundedWithoutLowThroughputCeiling verifies bounded completed-request storage without the earlier low throughput ceiling.
func TestCompletedRequestsRemainBoundedWithoutLowThroughputCeiling(t *testing.T) {
	store := newRequestStore()
	now := time.Now()
	store.now = func() time.Time {
		return now
	}
	deadline := now.Add(time.Minute).UnixMilli()
	// This exceeds the old total capacity by several multiples without advancing
	// time, so none of the replay-protection tombstones have expired.
	for i := 0; i < 5000; i++ {
		_, complete, err := store.begin(context.Background(), fmt.Sprintf("%032x", i), deadline)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		complete()
	}
	if len(store.entries) != 5000 || len(store.expiry) != 5000 || store.active != 0 {
		t.Fatalf("unexpected registry accounting: entries=%d heap=%d active=%d", len(store.entries), len(store.expiry), store.active)
	}
	if _, _, err := store.begin(context.Background(), fmt.Sprintf("%032x", 0), deadline); !errors.Is(err, errRequestRejected) {
		t.Fatalf("completed request replay accepted: %v", err)
	}
	now = now.Add(cancellationTTL + time.Second)
	if _, _, err := store.begin(context.Background(), fmt.Sprintf("%032x", 0), deadline); !errors.Is(err, errRequestRejected) {
		t.Fatalf("expired original envelope accepted after pruning: %v", err)
	}
	if len(store.entries) != 0 || len(store.expiry) != 0 {
		t.Fatal("expired completed requests were not reclaimed")
	}
}

// TestActiveLimitAndRepeatedCancellationAreIndependentlyBounded verifies that the active limit and repeated cancellation are independently bounded.
func TestActiveLimitAndRepeatedCancellationAreIndependentlyBounded(t *testing.T) {
	store := newRequestStore()
	now := time.Now()
	store.now = func() time.Time {
		return now
	}
	deadline := now.Add(time.Minute).UnixMilli()
	completions := make([]func(), 0, maxActiveRequests)
	for i := 0; i < maxActiveRequests; i++ {
		_, complete, err := store.begin(context.Background(), fmt.Sprintf("%032x", i), deadline)
		if err != nil {
			t.Fatalf("active request %d: %v", i, err)
		}
		completions = append(completions, complete)
	}
	defer func() {
		for _, complete := range completions {
			complete()
		}
	}()
	if _, _, err := store.begin(context.Background(), strings.Repeat("f", 32), deadline); !errors.Is(err, errRequestCapacity) {
		t.Fatalf("active limit not enforced: %v", err)
	}
	completions[0]()
	_, complete, err := store.begin(context.Background(), strings.Repeat("f", 32), deadline)
	if err != nil {
		t.Fatalf("active slot not recovered: %v", err)
	}
	complete()
	for i := 0; i < 10000; i++ {
		if _, _, err := store.cancel(strings.Repeat("f", 32)); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.expiry) != 2 {
		t.Fatalf("retrying cancellation grew expiry heap: %d", len(store.expiry))
	}
}

// TestCancellationTombstoneExtensionReordersExpiryHeap verifies that extending a tombstone reorders the expiry heap.
func TestCancellationTombstoneExtensionReordersExpiryHeap(t *testing.T) {
	store := newRequestStore()
	now := time.Now()
	store.now = func() time.Time {
		return now
	}
	first, second := strings.Repeat("a", 32), strings.Repeat("b", 32)
	if _, _, err := store.cancel(first); err != nil {
		t.Fatal(err)
	}
	now = now.Add(20 * time.Second)
	if _, _, err := store.cancel(second); err != nil {
		t.Fatal(err)
	}
	now = now.Add(20 * time.Second)
	if _, _, err := store.cancel(first); err != nil {
		t.Fatal(err)
	}
	now = now.Add(cancellationTTL - 20*time.Second + time.Second)
	store.mu.Lock()
	store.pruneLocked()
	_, firstPresent := store.entries[first]
	_, secondPresent := store.entries[second]
	store.mu.Unlock()
	if !firstPresent || secondPresent {
		t.Fatal("expiry heap did not preserve the extended tombstone")
	}
	now = now.Add(20 * time.Second)
	store.mu.Lock()
	store.pruneLocked()
	remaining := len(store.entries)
	store.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("extended tombstones not reclaimed: %d", remaining)
	}
}

// TestConcurrentCancelAndCompletionRetainSingleTombstones verifies that concurrent cancellation and completion retain one tombstone.
func TestConcurrentCancelAndCompletionRetainSingleTombstones(t *testing.T) {
	store := newRequestStore()
	deadline := time.Now().Add(time.Minute).UnixMilli()
	const count = 256
	var workers sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("%032x", i)
		_, complete, err := store.begin(context.Background(), id, deadline)
		if err != nil {
			t.Fatal(err)
		}
		workers.Add(2)
		go func() {
			defer workers.Done()
			done, _, err := store.cancel(id)
			if err != nil {
				errs <- err
				return
			}
			<-done
		}()
		go func() {
			defer workers.Done()
			complete()
		}()
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if len(store.entries) != count || len(store.expiry) != count || store.active != 0 {
		t.Fatalf("unexpected concurrent accounting: entries=%d heap=%d active=%d", len(store.entries), len(store.expiry), store.active)
	}
}

// TestActiveEntriesAreNeverPrunedAndCloseCancelsThem verifies that active entries are not pruned and closing cancels them.
func TestActiveEntriesAreNeverPrunedAndCloseCancelsThem(t *testing.T) {
	store := newRequestStore()
	now := time.Now()
	store.now = func() time.Time {
		return now
	}
	ctx, complete, err := store.begin(context.Background(), cancellationID, now.Add(time.Minute).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	defer complete()
	now = now.Add(10 * time.Minute)
	store.mu.Lock()
	store.pruneLocked()
	count := len(store.entries)
	store.mu.Unlock()
	if count != 1 {
		t.Fatal("live cancellation handle was pruned")
	}
	store.close()
	if ctx.Err() == nil {
		t.Fatal("store shutdown left active operation running")
	}
	if _, _, err = store.cancel(strings.Repeat("f", 32)); !errors.Is(err, errRequestRejected) {
		t.Fatal("closed store created new tombstone")
	}
}

// TestCancelAuthenticationMetadataAndRouteValidation verifies cancellation authentication, metadata and route validation.
func TestCancelAuthenticationMetadataAndRouteValidation(t *testing.T) {
	s, _ := newServer(t)
	request := httptest.NewRequest("POST", "/cancel", bytes.NewReader(cancellationBody(cancellationID)))
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 401 || len(s.requests.entries) != 0 {
		t.Fatal("unauthenticated cancellation changed state")
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Origin", "http://evil.test")
	response = httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 403 || len(s.requests.entries) != 0 {
		t.Fatal("browser origin canceled request")
	}
	for _, test := range []struct {
		method, path string
		body         []byte
		status       int
	}{{"GET", "/cancel", nil, 405}, {"POST", "/cancel?x=1", cancellationBody(cancellationID), 400}, {"POST", "/can%63el", cancellationBody(cancellationID), 400}, {"POST", "/cancel", cancellationBody("invalid"), 400}, {"POST", "/cancel", []byte(`{"requestId":"` + cancellationID + `","unknown":true}`), 400}} {
		response := call(s, test.method, test.path, test.body)
		if response.Code != test.status {
			t.Fatal(test.method, test.path, response.Code, response.Body)
		}
	}
	for _, headers := range []map[string][]string{{"X-Sidekicks-Request-ID": {cancellationID}}, {"X-Sidekicks-Deadline": {strconv.FormatInt(time.Now().Add(time.Minute).UnixMilli(), 10)}}, {"X-Sidekicks-Request-ID": {cancellationID, cancellationID}, "X-Sidekicks-Deadline": {"1"}}, {"X-Sidekicks-Request-ID": {cancellationID}, "X-Sidekicks-Deadline": {"+1"}}, {"X-Sidekicks-Request-ID": {"invalid"}, "X-Sidekicks-Deadline": {"1"}}} {
		request = httptest.NewRequest("GET", "/health", nil)
		request.Header.Set("Authorization", "Bearer "+testToken)
		for key, values := range headers {
			for _, value := range values {
				request.Header.Add(key, value)
			}
		}
		response = httptest.NewRecorder()
		s.ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatal("invalid metadata accepted", response.Code, response.Body)
		}
	}
	if _, err := New(Config{Workspace: t.TempDir(), Token: testToken, ExtraRoutes: map[string]http.Handler{"/cancel": http.NotFoundHandler()}}); err == nil {
		t.Fatal("reserved cancellation route was overridden")
	}
}

// TestCancelDoesNotAcknowledgeIgnoringHandler verifies that cancellation does not acknowledge an unresponsive handler.
func TestCancelDoesNotAcknowledgeIgnoringHandler(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() {
		close(release)
	})
	s, err := New(Config{Workspace: t.TempDir(), Token: testToken, ExtraRoutes: map[string]http.Handler{"/browser": http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	finished := make(chan struct{})
	go func() {
		trackedHTTP(s, "POST", "/browser", []byte(`{}`), cancellationID, time.Now().Add(time.Minute).UnixMilli())
		close(finished)
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest("POST", "/cancel", bytes.NewReader(cancellationBody(cancellationID))).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 503 || strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatal("ignored cancellation was falsely confirmed", response.Code, response.Body)
	}
	once.Do(func() {
		close(release)
	})
	<-finished
}

// TestLocalTransportCarriesIDDeadlineAndRejectsDelayedEnvelope verifies request IDs, deadlines and delayed envelopes on the local transport.
func TestLocalTransportCarriesIDDeadlineAndRejectsDelayedEnvelope(t *testing.T) {
	deadline := time.Now().Add(time.Minute).UnixMilli()
	request := LocalRequest{Method: "GET", Path: "/health", RequestID: cancellationID, DeadlineUnixMS: deadline}
	raw, _ := json.Marshal(request)
	decoded, err := DecodeLocalRequest(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Sidekicks-Request-ID") != cancellationID || r.Header.Get("X-Sidekicks-Deadline") != strconv.FormatInt(deadline, 10) {
			t.Fatal("transport metadata lost")
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})}
	if _, err = CallLocal(context.Background(), client, testToken, decoded); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []LocalRequest{{Method: "GET", Path: "/health", RequestID: cancellationID}, {Method: "GET", Path: "/health", DeadlineUnixMS: deadline}, {Method: "GET", Path: "/health", RequestID: cancellationID, DeadlineUnixMS: time.Now().Add(-time.Second).UnixMilli()}, {Method: "GET", Path: "/health", RequestID: cancellationID, DeadlineUnixMS: time.Now().Add(3 * time.Minute).UnixMilli()}} {
		raw, _ = json.Marshal(bad)
		if _, err = DecodeLocalRequest(base64.StdEncoding.EncodeToString(raw)); err == nil {
			t.Fatal("invalid or delayed envelope accepted")
		}
	}
	raw, _ = json.Marshal(LocalRequest{Method: "POST", Path: "/cancel", Body: cancellationBody(cancellationID)})
	if _, err = DecodeLocalRequest(base64.StdEncoding.EncodeToString(raw)); err != nil {
		t.Fatal("fixed cancellation route denied", err)
	}
}
