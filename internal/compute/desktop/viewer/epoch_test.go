package viewer

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func epochRequest(service *Service, path, body string, cookie *http.Cookie, epochs ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Host = authority
	r.Header.Set("Origin", "http://"+authority)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(cookie)
	for _, epoch := range epochs {
		r.Header.Add(EpochHeader, epoch)
	}
	w := httptest.NewRecorder()
	service.ServeHTTP(w, r)
	return w
}
func take(t *testing.T, service *Service, cookie *http.Cookie) uint64 {
	t.Helper()
	response := request(service, http.MethodPost, "/control", `{"action":"acquire"}`, cookie)
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	body := state(t, response)
	if !body.Owned || body.Epoch == 0 {
		t.Fatal("current owner lacks a non-secret generation")
	}
	return body.Epoch
}

// TestEpochBindsQueuedReleaseRenewAndInputToExactGeneration verifies epoch binds queued release renew and input to exact generation.
func TestEpochBindsQueuedReleaseRenewAndInputToExactGeneration(t *testing.T) {
	service, gate, driver := setup(t, nil)
	cookie := authenticate(t, service)
	old := take(t, service, cookie)
	if renewed := take(t, service, cookie); renewed != old {
		t.Fatal("idempotent acquisition changed epoch")
	}
	if response := epochRequest(service, "/control", `{"action":"release"}`, cookie, strconv.FormatUint(old, 10)); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	fresh := take(t, service, cookie)
	if fresh == old {
		t.Fatal("reacquisition reused old epoch")
	}
	before := driver.resets.Load()
	owner := gate.View().Session
	headers := [][]string{nil, {"0"}, {"01"}, {"-1"}, {"+1"}, {" 1"}, {"abc"}, {"18446744073709551616"}, {strconv.FormatUint(old, 10)}, {strconv.FormatUint(fresh, 10), strconv.FormatUint(fresh, 10)}}
	for _, epochs := range headers {
		for _, action := range []string{"release", "stop", "renew"} {
			response := epochRequest(service, "/control", fmt.Sprintf(`{"action":%q}`, action), cookie, epochs...)
			if response.Code != http.StatusConflict {
				t.Errorf("%s epochs%q accepted: %d", action, epochs, response.Code)
			}
		}
		response := epochRequest(service, "/input", `{"events":[{"type":"eval"}]}`, cookie, epochs...)
		if response.Code != http.StatusConflict {
			t.Errorf("bad input epoch%q reached batch rejection: %d", epochs, response.Code)
		}
	}
	if gate.View().Session != owner || driver.resets.Load() != before || driver.inputs.Load() != 0 {
		t.Fatal("stale/missing/malformed epoch disturbed fresh owner")
	}
	response := epochRequest(service, "/input", `{"events":[{"type":"text","text":"current"}]}`, cookie, strconv.FormatUint(fresh, 10))
	if response.Code != http.StatusOK || driver.inputs.Load() != 1 {
		t.Fatal("current epoch input failed")
	}
	response = epochRequest(service, "/input", `{"events":[{"type":"eval"}]}`, cookie, strconv.FormatUint(fresh, 10))
	if response.Code != http.StatusBadRequest || gate.View().Owner != "" {
		t.Fatal("valid-epoch invalid batch did not release its own current input")
	}
}

type observedReader struct{ calls atomic.Int64 }

// Read records the call and returns an unexpected end-of-file error.
func (r *observedReader) Read([]byte) (int, error) {
	r.calls.Add(1)
	return 0, io.ErrUnexpectedEOF
}

// TestMissingInputEpochIsRejectedBeforeReadingBody verifies missing input epoch is rejected before reading body.
func TestMissingInputEpochIsRejectedBeforeReadingBody(t *testing.T) {
	service, gate, driver := setup(t, nil)
	cookie := authenticate(t, service)
	take(t, service, cookie)
	reader := &observedReader{}
	r := httptest.NewRequest(http.MethodPost, "/input", reader)
	r.Host = authority
	r.Header.Set("Origin", "http://"+authority)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(cookie)
	before := driver.resets.Load()
	w := httptest.NewRecorder()
	service.ServeHTTP(w, r)
	if w.Code != http.StatusConflict || reader.calls.Load() != 0 || gate.View().Owner == "" || driver.resets.Load() != before {
		t.Fatal("missing epoch parsed/rejected body against current owner")
	}
}

type delayedBody struct {
	reader           *strings.Reader
	entered, proceed chan struct{}
	once             sync.Once
}

// Read reads data into the supplied buffer.
func (b *delayedBody) Read(data []byte) (int, error) {
	b.once.Do(func() {
		close(b.entered)
		<-b.proceed
	})
	return b.reader.Read(data)
}

// TestInputBodyCompletionCannotAdoptNewServerHeldCredentials verifies input body completion cannot adopt new server held credentials.
func TestInputBodyCompletionCannotAdoptNewServerHeldCredentials(t *testing.T) {
	for _, body := range []string{`not-json`, `{"events":[{"type":"text","text":"stale"}]}`} {
		t.Run(body, func(t *testing.T) {
			service, gate, driver := setup(t, nil)
			cookie := authenticate(t, service)
			old := take(t, service, cookie)
			delayed := &delayedBody{reader: strings.NewReader(body), entered: make(chan struct{}), proceed: make(chan struct{})}
			t.Cleanup(func() {
				select {
				case <-delayed.proceed:
				default:
					close(delayed.proceed)
				}
			})
			r := httptest.NewRequest(http.MethodPost, "/input", delayed)
			r.Host = authority
			r.Header.Set("Origin", "http://"+authority)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set(EpochHeader, strconv.FormatUint(old, 10))
			r.AddCookie(cookie)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				w := httptest.NewRecorder()
				service.ServeHTTP(w, r)
				done <- w
			}()
			select {
			case <-delayed.entered: // The old epoch was accepted and its exact credentials captured.
			case <-time.After(time.Second):
				t.Fatal("delayed request did not begin")
			}
			if response := epochRequest(service, "/control", `{"action":"release"}`, cookie, strconv.FormatUint(old, 10)); response.Code != http.StatusOK {
				t.Fatal(response.Body.String())
			}
			fresh := take(t, service, cookie)
			if fresh == old {
				t.Fatal("new generation not established")
			}
			before := driver.resets.Load()
			owner := gate.View().Session
			close(delayed.proceed)
			var response *httptest.ResponseRecorder
			select {
			case response = <-done:
			case <-time.After(time.Second):
				t.Fatal("delayed request did not complete")
			}
			expected := http.StatusConflict
			if body == `not-json` {
				expected = http.StatusBadRequest
			}
			if response.Code != expected {
				t.Fatalf("delayed body status %d want %d", response.Code, expected)
			}
			current := state(t, request(service, http.MethodGet, "/state", "", cookie))
			if !current.Owned || current.Epoch != fresh || gate.View().Session != owner || driver.resets.Load() != before || driver.inputs.Load() != 0 {
				t.Fatal("old request adopted or released fresh generation")
			}
		})
	}
}
