package guest

import (
	"container/heap"
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const (
	// Reserve a tombstone for every active request. This retains replay and
	// cancel-before-start protection even if the registry reaches capacity.
	maxTrackedRequests = 65536
	maxActiveRequests  = 1024
	cancellationTTL    = 2 * time.Minute
	maxRequestFuture   = 2 * time.Minute
)

var requestIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var errRequestRejected = errors.New("tracked request is unavailable")
var errRequestCapacity = errors.New("tracked request capacity reached")

type trackedRequest struct {
	id      string
	cancel  context.CancelFunc
	done    chan struct{}
	active  bool
	expires time.Time
	index   int // Index is the heap position, or -1 while active.
}

// expiryQueue contains only inactive entries. Keeping a single heap item per
// tombstone bounds memory even when the same cancellation is retried.
type expiryQueue []*trackedRequest

// Len returns the number of queued expiry records.
func (q expiryQueue) Len() int {
	return len(q)
}

// Less reports whether the first expiry precedes the second.
func (q expiryQueue) Less(i, j int) bool {
	return q[i].expires.Before(q[j].expires)
}

// Swap exchanges two expiry records.
func (q expiryQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index, q[j].index = i, j
}

// Push adds an expiry record to the queue.
func (q *expiryQueue) Push(x any) {
	e := x.(*trackedRequest)
	e.index = len(*q)
	*q = append(*q, e)
}

// Pop removes the last expiry record from the queue.
func (q *expiryQueue) Pop() any {
	old := *q
	e := old[len(old)-1]
	old[len(old)-1] = nil
	*q = old[:len(old)-1]
	e.index = -1
	return e
}

var completedRequest = func() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

type requestStore struct {
	mu      sync.Mutex
	entries map[string]*trackedRequest
	expiry  expiryQueue
	active  int
	now     func() time.Time
	closed  bool
}

func newRequestStore() *requestStore {
	return &requestStore{entries: map[string]*trackedRequest{}, now: time.Now}
}
func validRequestMetadata(id string, deadlineMS int64, now time.Time) error {
	if !requestIDPattern.MatchString(id) || deadlineMS <= 0 {
		return errors.New("invalid tracked request metadata")
	}
	deadline := time.UnixMilli(deadlineMS)
	if !deadline.After(now) {
		return errRequestRejected
	}
	if deadline.After(now.Add(maxRequestFuture)) {
		return errors.New("request deadline exceeds maximum")
	}
	return nil
}
func (s *requestStore) pruneLocked() {
	now := s.now()
	for len(s.expiry) > 0 && !now.Before(s.expiry[0].expires) {
		entry := heap.Pop(&s.expiry).(*trackedRequest)
		delete(s.entries, entry.id)
	}
}
func (s *requestStore) begin(ctx context.Context, id string, deadlineMS int64) (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	if err := validRequestMetadata(id, deadlineMS, s.now()); err != nil {
		return nil, nil, err
	}
	if s.closed {
		return nil, nil, errRequestRejected
	}
	if _, exists := s.entries[id]; exists {
		return nil, nil, errRequestRejected
	}
	if len(s.entries) >= maxTrackedRequests || s.active >= maxActiveRequests {
		return nil, nil, errRequestCapacity
	}
	active, cancel := context.WithDeadline(ctx, time.UnixMilli(deadlineMS))
	entry := &trackedRequest{id: id, cancel: cancel, done: make(chan struct{}), active: true, index: -1}
	s.entries[id] = entry
	s.active++
	var once sync.Once
	complete := func() {
		once.Do(func() {
			cancel()
			s.mu.Lock()
			defer s.mu.Unlock()
			entry.active = false
			s.active--
			entry.cancel = nil
			entry.expires = s.now().Add(cancellationTTL)
			done := entry.done
			entry.done = completedRequest
			heap.Push(&s.expiry, entry)
			close(done)
		})
	}
	return active, complete, nil
}
func (s *requestStore) cancel(id string) (<-chan struct{}, bool, error) {
	if !requestIDPattern.MatchString(id) {
		return nil, false, errors.New("invalid cancellation id")
	}
	s.mu.Lock()
	s.pruneLocked()
	entry, exists := s.entries[id]
	if !exists {
		if s.closed {
			s.mu.Unlock()
			return nil, false, errRequestRejected
		}
		if len(s.entries) >= maxTrackedRequests {
			s.mu.Unlock()
			return nil, false, errRequestCapacity
		}
		entry = &trackedRequest{id: id, done: completedRequest, expires: s.now().Add(cancellationTTL), index: -1}
		s.entries[id] = entry
		heap.Push(&s.expiry, entry)
	}
	if !entry.active {
		entry.expires = s.now().Add(cancellationTTL)
		heap.Fix(&s.expiry, entry.index)
	}
	cancel := entry.cancel
	active := entry.active
	done := entry.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return done, active, nil
}
func (s *requestStore) close() {
	s.mu.Lock()
	s.closed = true
	var cancels []context.CancelFunc
	for _, entry := range s.entries {
		if entry.cancel != nil {
			cancels = append(cancels, entry.cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (s *Server) trackRequest(w http.ResponseWriter, r *http.Request) (*http.Request, func(), bool) {
	ids, deadlines := r.Header.Values("X-Sidekicks-Request-ID"), r.Header.Values("X-Sidekicks-Deadline")
	if len(ids) == 0 && len(deadlines) == 0 {
		return r, func() {

		}, true
	}
	if len(ids) != 1 || len(deadlines) != 1 {
		writeError(w, http.StatusBadRequest, "invalid tracked request metadata")
		return nil, nil, false
	}
	deadline, err := strconv.ParseInt(deadlines[0], 10, 64)
	if err != nil || strconv.FormatInt(deadline, 10) != deadlines[0] {
		writeError(w, http.StatusBadRequest, "invalid tracked request metadata")
		return nil, nil, false
	}
	ctx, complete, err := s.requests.begin(r.Context(), ids[0], deadline)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errRequestRejected) {
			status = http.StatusConflict
		}
		if errors.Is(err, errRequestCapacity) {
			status = http.StatusTooManyRequests
		}
		writeError(w, status, "tracked request is unavailable")
		return nil, nil, false
	}
	return r.WithContext(ctx), complete, true
}
func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	if r.URL.RawQuery != "" || r.URL.RawPath != "" {
		writeError(w, http.StatusBadRequest, "invalid cancellation route")
		return
	}
	var body struct {
		RequestID string `json:"requestId"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	done, active, err := s.requests.cancel(body.RequestID)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errRequestCapacity) {
			status = http.StatusTooManyRequests
		}
		if errors.Is(err, errRequestRejected) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, "request cancellation is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	select {
	case <-done:
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "canceled": active})
	case <-ctx.Done():
		writeError(w, http.StatusServiceUnavailable, "request cancellation could not be confirmed")
	}
}
