package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeDriver struct {
	resets  atomic.Int64
	fail    atomic.Bool
	resetFn func(context.Context) error
}

// Reset implements the test driver reset operation.
func (d *fakeDriver) Reset(ctx context.Context) error {
	d.resets.Add(1)
	if d.resetFn != nil {
		return d.resetFn(ctx)
	}
	if d.fail.Load() {
		return errors.New("driver reset failed")
	}
	return nil
}

type nativeDriver struct {
	fakeDriver
	giveFail atomic.Bool
	giveFn   func(context.Context, string) error
	mu       sync.Mutex
	viewer   string
}

// Give implements the test driver transfer operation.
func (d *nativeDriver) Give(ctx context.Context, id string) error {
	if d.giveFn != nil {
		return d.giveFn(ctx, id)
	}
	if d.giveFail.Load() {
		return errors.New("transfer failed")
	}
	d.mu.Lock()
	d.viewer = id
	d.mu.Unlock()
	return nil
}
func req(owner Owner, session string) Request {
	return Request{Owner: owner, Session: session, Duration: time.Second}
}
func creds(l Lease) Credentials {
	return Credentials{Session: l.Session, Token: l.Token}
}
func acquire(t *testing.T, m *Manager, r Request) Lease {
	t.Helper()
	l, err := m.Acquire(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for !fn() {
		if time.Now().After(until) {
			t.Fatal("condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}
func noWrite(t *testing.T) func(context.Context) error {
	t.Helper()
	return func(context.Context) error {
		t.Error("unauthorized dispatch executed")
		return nil
	}
}

// TestLeaseLifecycleAndCredentialPrivacy verifies the lease lifecycle and credential privacy.
func TestLeaseLifecycleAndCredentialPrivacy(t *testing.T) {
	d := &fakeDriver{}
	m := New(d)
	defer m.Close(context.Background())
	l := acquire(t, m, req(Agent, "agent-1"))
	if l.Token == "" || len(l.Token) < 40 || l.Owner != Agent || l.Mode != Gated || !l.Ready {
		t.Fatal(l)
	}
	if d.resets.Load() != 1 {
		t.Fatal("reset before grant missing")
	}
	data, _ := json.Marshal(m.View())
	if strings.Contains(string(data), l.Token) || strings.Contains(string(data), "token") {
		t.Fatal("public view leaked secret")
	}
	bad := req(Agent, "agent-1")
	bad.Token = "wrong"
	if _, err := m.Acquire(context.Background(), bad); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	renewed := req(Agent, "agent-1")
	renewed.Token = l.Token
	renewed.Duration = 2 * time.Second
	l2 := acquire(t, m, renewed)
	if l2.Token != l.Token || l2.Generation != l.Generation || !l2.ExpiresAt.After(l.ExpiresAt) {
		t.Fatal(l2)
	}
	if d.resets.Load() != 1 {
		t.Fatal("renewal reset active input")
	}
	for _, r := range []Request{req(Agent, "other"), req(Human, "human-a")} {
		if r.Owner == Human {
			continue
		}
		if _, err := m.Acquire(context.Background(), r); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if err := m.Dispatch(context.Background(), creds(l), func(context.Context) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.DispatchHuman(context.Background(), creds(l), noWrite(t)); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := m.Release(context.Background(), Credentials{l.Session, "bad"}); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if err := m.Release(context.Background(), creds(l)); err != nil {
		t.Fatal(err)
	}
	if m.View().Owner != "" || !m.View().Ready {
		t.Fatal(m.View())
	}
	if _, err := m.Validate(creds(l)); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if err := m.Dispatch(context.Background(), creds(l), noWrite(t)); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if err := m.Release(context.Background(), creds(l)); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
}

// TestValidationAndClosedManager verifies validation and closed-manager behavior.
func TestValidationAndClosedManager(t *testing.T) {
	d := &fakeDriver{}
	m := New(d, WithResetTimeout(time.Millisecond), WithResetTimeout(0), WithClock(nil), nil)
	cases := []Request{
		{Owner: "bad", Session: "ok"}, {Owner: Agent, Session: ""}, {Owner: Agent, Session: strings.Repeat("a", 129)},
		{Owner: Human, Session: "space space"}, {Owner: Agent, Session: "ok", Mode: "bad"},
		{Owner: Agent, Session: "ok", Duration: 99 * time.Millisecond}, {Owner: Agent, Session: "ok", Duration: 61 * time.Second},
		{Owner: Agent, Session: "ok", Mode: Native, ViewerID: "viewer"}, {Owner: Human, Session: "ok", Mode: Native},
		{Owner: Human, Session: "ok", Mode: Native, ViewerID: "bad viewer"}, {Owner: Human, Session: "ok", Mode: Gated, ViewerID: "viewer"},
	}
	for _, r := range cases {
		if _, err := m.Acquire(context.Background(), r); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v: %v", r, err)
		}
	}
	if _, err := m.Acquire(context.Background(), Request{Owner: Human, Session: "human", Mode: Native, ViewerID: "viewer"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	l := acquire(t, m, Request{Owner: Human, Session: "human"})
	if time.Until(l.ExpiresAt) < 29*time.Second {
		t.Fatal("default duration")
	}
	if _, err := m.Acquire(context.Background(), req(Agent, "agent")); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := m.Acquire(context.Background(), req(Human, "other")); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := m.Dispatch(context.Background(), creds(l), nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.View().Ready {
		t.Fatal("closed marked ready")
	}
	if _, err := m.Acquire(context.Background(), req(Human, "human")); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := m.Validate(creds(l)); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	nilManager := New(nil)
	if nilManager.View().Ready {
		t.Fatal("nil driver ready")
	}
	if _, err := nilManager.Acquire(context.Background(), req(Agent, "a")); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err := nilManager.Stop(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

// TestHumanPreemptionCancelsInFlightAndQueuedWrites verifies that human preemption cancels in-flight and queued writes.
func TestHumanPreemptionCancelsInFlightAndQueuedWrites(t *testing.T) {
	d := &fakeDriver{}
	m := New(d)
	defer m.Close(context.Background())
	a := acquire(t, m, req(Agent, "a"))
	started := make(chan struct{})
	cancelled := make(chan error, 1)
	go func() {
		cancelled <- m.Dispatch(context.Background(), creds(a), func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	<-started
	queued := make(chan error, 1)
	go func() {
		queued <- m.Dispatch(context.Background(), creds(a), noWrite(t))
	}()
	h := acquire(t, m, req(Human, "h"))
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := <-queued; !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if h.Owner != Human || d.resets.Load() != 2 {
		t.Fatal(h, d.resets.Load())
	}
	if _, err := m.Validate(creds(a)); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if err := m.DispatchHumanGated(context.Background(), creds(h), func(context.Context) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestPendingAcquisitionCanBePreemptedAndStopped verifies that pending acquisition can be preempted and stopped.
func TestPendingAcquisitionCanBePreemptedAndStopped(t *testing.T) {
	var calls atomic.Int64
	entered := make(chan struct{})
	resume := make(chan struct{})
	d := &fakeDriver{resetFn: func(context.Context) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-resume
		}
		return nil
	}}
	m := New(d)
	defer m.Close(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := m.Acquire(context.Background(), req(Agent, "a"))
		first <- err
	}()
	<-entered
	if _, err := m.Acquire(context.Background(), req(Agent, "b")); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	humanResult := make(chan Lease, 1)
	humanErr := make(chan error, 1)
	go func() {
		l, err := m.Acquire(context.Background(), req(Human, "h"))
		humanResult <- l
		humanErr <- err
	}()
	eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.pending != nil && m.pending.owner == Human
	})
	if _, err := m.Acquire(context.Background(), req(Human, "other")); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	close(resume)
	if err := <-first; !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := <-humanErr; err != nil {
		t.Fatal(err)
	}
	h := <-humanResult
	if _, err := m.Validate(creds(h)); err != nil {
		t.Fatal(err)
	}
	// Stop immediately invalidates the current credentials before reset.
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Validate(creds(h)); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
}

// TestNativeTransferAndModeSpecificDispatch verifies native transfer and mode-specific dispatch.
func TestNativeTransferAndModeSpecificDispatch(t *testing.T) {
	d := &nativeDriver{}
	m := New(d)
	defer m.Close(context.Background())
	r := req(Human, "h")
	r.Mode = Native
	r.ViewerID = "viewer-1"
	h := acquire(t, m, r)
	if d.viewer != "viewer-1" {
		t.Fatal("native viewer not granted")
	}
	if err := m.DispatchGated(context.Background(), creds(h), noWrite(t)); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := m.DispatchHumanGated(context.Background(), creds(h), noWrite(t)); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := m.DispatchHuman(context.Background(), creds(h), func(context.Context) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Release(context.Background(), creds(h)); err != nil {
		t.Fatal(err)
	}
	d.giveFail.Store(true)
	if _, err := m.Acquire(context.Background(), r); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if m.View().Owner != "" {
		t.Fatal("failed native transfer published owner")
	}
	eventually(t, func() bool {
		return m.View().Ready
	})
	d.giveFail.Store(false)
	a := acquire(t, m, req(Agent, "agent"))
	if err := m.DispatchGated(context.Background(), creds(a), func(context.Context) error {
		return errors.New("write failed")
	}); err == nil || err.Error() != "write failed" {
		t.Fatal(err)
	}
}

// TestResetFailureIsFailClosedAndRecoverable verifies that reset failures fail closed and are recoverable.
func TestResetFailureIsFailClosedAndRecoverable(t *testing.T) {
	d := &fakeDriver{}
	d.fail.Store(true)
	m := New(d)
	defer m.Close(context.Background())
	if _, err := m.Acquire(context.Background(), req(Agent, "a")); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if m.View().Owner != "" || m.View().Ready {
		t.Fatal(m.View())
	}
	if err := m.Stop(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	d.fail.Store(false)
	l := acquire(t, m, req(Human, "h"))
	// A delayed cleanup from the failed generation must never reset this owner.
	before := d.resets.Load()
	time.Sleep(10 * time.Millisecond)
	if _, err := m.Validate(creds(l)); err != nil {
		t.Fatal(err)
	}
	if after := d.resets.Load(); after != before {
		t.Fatalf("old cleanup reset new owner %d -> %d", before, after)
	}
	d.fail.Store(true)
	if err := m.Release(context.Background(), creds(l)); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if m.View().Owner != "" || m.View().Ready {
		t.Fatal(m.View())
	}
	d.fail.Store(false)
}

// TestExplicitAndAutomaticExpiry verifies explicit and automatic lease expiry.
func TestExplicitAndAutomaticExpiry(t *testing.T) {
	var nanos atomic.Int64
	nanos.Store(time.Now().UnixNano())
	now := func() time.Time {
		return time.Unix(0, nanos.Load())
	}
	d := &fakeDriver{}
	m := New(d, WithClock(now))
	defer m.Close(context.Background())
	a := acquire(t, m, req(Agent, "a"))
	nanos.Add(int64(time.Second))
	if err := m.Expire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.View().Owner != "" || d.resets.Load() != 2 {
		t.Fatal(m.View(), d.resets.Load())
	}
	if _, err := m.Validate(creds(a)); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if err := m.Expire(context.Background()); err != nil {
		t.Fatal(err)
	}
	a = acquire(t, m, req(Agent, "a"))
	nanos.Add(int64(time.Second))
	if _, err := m.Validate(creds(a)); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return m.View().Ready
	})
	_ = acquire(t, m, req(Agent, "a"))
	nanos.Add(int64(time.Second))
	if m.View().Owner != "" {
		t.Fatal("expired View")
	}
	eventually(t, func() bool {
		return m.View().Ready
	})
	d2 := &fakeDriver{}
	m2 := New(d2)
	defer m2.Close(context.Background())
	r := req(Agent, "a")
	r.Duration = 100 * time.Millisecond
	_ = acquire(t, m2, r)
	eventually(t, func() bool {
		return d2.resets.Load() >= 2 && m2.View().Owner == ""
	})
}

// TestCancelledRequestsDoNotRevokeOrResetNewerOwner verifies that canceled requests do not revoke or reset a newer owner.
func TestCancelledRequestsDoNotRevokeOrResetNewerOwner(t *testing.T) {
	d := &fakeDriver{}
	m := New(d)
	defer m.Close(context.Background())
	a := acquire(t, m, req(Agent, "a"))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Acquire(cancelled, req(Human, "h")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := m.Validate(creds(a)); err != nil {
		t.Fatal("already canceled request revoked owner", err)
	}
	started := make(chan struct{})
	resume := make(chan struct{})
	writing := make(chan error, 1)
	go func() {
		writing <- m.Dispatch(context.Background(), creds(a), func(context.Context) error {
			close(started)
			<-resume
			return nil
		})
	}()
	<-started
	short, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	if _, err := m.Acquire(short, req(Human, "cancelled")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	next := make(chan Lease, 1)
	nextErr := make(chan error, 1)
	go func() {
		l, err := m.Acquire(context.Background(), req(Human, "new"))
		next <- l
		nextErr <- err
	}()
	eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.pending != nil && m.pending.owner == Human
	})
	close(resume)
	if err := <-writing; !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if err := <-nextErr; err != nil {
		t.Fatal(err)
	}
	h := <-next
	if _, err := m.Validate(creds(h)); err != nil {
		t.Fatal(err)
	}
	before := d.resets.Load()
	time.Sleep(10 * time.Millisecond)
	if d.resets.Load() != before {
		t.Fatal("old canceled cleanup reset new owner")
	}
}

// TestDispatchContextCancellationAndLateModeCheck verifies dispatch cancellation and the final mode check.
func TestDispatchContextCancellationAndLateModeCheck(t *testing.T) {
	d := &fakeDriver{}
	m := New(d)
	defer m.Close(context.Background())
	l := acquire(t, m, req(Human, "h"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Dispatch(ctx, creds(l), noWrite(t)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Queue a legitimate request, then expire its generation before dispatch.
	<-m.serial
	out := make(chan error, 1)
	go func() {
		out <- m.DispatchGated(context.Background(), creds(l), noWrite(t))
	}()
	time.Sleep(time.Millisecond)
	m.mu.Lock()
	m.revokeLocked()
	m.mu.Unlock()
	m.serial <- struct{}{}
	if err := <-out; !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
}

// TestStopCancellationLeavesNoOwnerAndEventuallyReleases verifies that a canceled stop leaves no owner and eventually releases input.
func TestStopCancellationLeavesNoOwnerAndEventuallyReleases(t *testing.T) {
	d := &fakeDriver{}
	m := New(d)
	defer m.Close(context.Background())
	l := acquire(t, m, req(Human, "h"))
	entered := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- m.Dispatch(context.Background(), creds(l), func(context.Context) error {
			close(entered)
			<-resume
			return nil
		})
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if m.View().Owner != "" || m.View().Ready {
		t.Fatal("canceled stop claimed released state")
	}
	close(resume)
	if err := <-done; !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return m.View().Ready
	})
}

// TestConcurrentRenewDispatchAndView verifies concurrent renewal, dispatch and view operations.
func TestConcurrentRenewDispatchAndView(t *testing.T) {
	d := &fakeDriver{}
	m := New(d)
	defer m.Close(context.Background())
	l := acquire(t, m, req(Human, "h"))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := req(Human, "h")
			r.Token = l.Token
			if _, err := m.Acquire(context.Background(), r); err != nil {
				t.Error(err)
			}
			_ = m.View()
			if err := m.DispatchHumanGated(context.Background(), creds(l), func(context.Context) error {
				return nil
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

// TestStopResetAcknowledgementDoesNotRelyOnMerelyPendingAcquisition verifies that stop acknowledgement does not rely on a merely pending acquisition.
func TestStopResetAcknowledgementDoesNotRelyOnMerelyPendingAcquisition(t *testing.T) {
	d := &fakeDriver{}
	m := New(d)
	defer m.Close(context.Background())
	_ = acquire(t, m, req(Agent, "a"))
	m.mu.Lock()
	m.revokeLocked()
	stopTicket := m.generation
	// Simulate a new intent reserved while Stop waits for the serialized driver.
	m.generation++
	m.pending = &intent{ticket: m.generation, owner: Human}
	m.mu.Unlock()
	if err := m.resetAt(context.Background(), stopTicket); err != nil {
		t.Fatal(err)
	}
	if d.resets.Load() != 2 {
		t.Fatal("stop acknowledged without actual reset")
	}
	if m.View().Ready {
		t.Fatal("pending acquisition marked ready")
	}
	m.mu.Lock()
	m.pending = nil
	m.mu.Unlock()
}

// TestWriteContextCancellationIsReportedEvenIfDriverReturnsNil verifies that cancellation is reported when the driver returns nil.
func TestWriteContextCancellationIsReportedEvenIfDriverReturnsNil(t *testing.T) {
	m := New(&fakeDriver{})
	defer m.Close(context.Background())
	l := acquire(t, m, req(Human, "h"))
	ctx, cancel := context.WithCancel(context.Background())
	if err := m.Dispatch(ctx, creds(l), func(context.Context) error {
		cancel()
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// TestStopDuringNativeTransferRetakesBeforeAcknowledging verifies that stop retakes native control before acknowledgement.
func TestStopDuringNativeTransferRetakesBeforeAcknowledging(t *testing.T) {
	entered := make(chan struct{})
	resume := make(chan struct{})
	d := &nativeDriver{giveFn: func(context.Context, string) error {
		close(entered)
		<-resume
		return nil
	}}
	m := New(d)
	defer m.Close(context.Background())
	r := req(Human, "h")
	r.Mode = Native
	r.ViewerID = "v"
	acquired := make(chan error, 1)
	go func() {
		_, err := m.Acquire(context.Background(), r)
		acquired <- err
	}()
	<-entered
	stopped := make(chan error, 1)
	go func() {
		stopped <- m.Stop(context.Background())
	}()
	eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.pending == nil
	})
	close(resume)
	if err := <-acquired; !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if d.resets.Load() < 2 || m.View().Owner != "" {
		t.Fatal("native transfer was not revoked")
	}
}

// TestStopCancelsPendingNativeTransfer verifies revocation interrupts an in-flight transfer.
func TestStopCancelsPendingNativeTransfer(t *testing.T) {
	entered := make(chan struct{})
	d := &nativeDriver{giveFn: func(ctx context.Context, _ string) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}}
	m := New(d)
	defer m.Close(context.Background())
	r := req(Human, "human")
	r.Mode, r.ViewerID = Native, "viewer"
	done := make(chan error, 1)
	go func() {
		_, err := m.Acquire(context.Background(), r)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("native transfer did not begin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("stop did not cancel the transfer: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("revoked acquisition: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("revoked acquisition did not finish")
	}
}

// TestRealExpiryTimerHonorsRenewedDeadline verifies that the expiry timer honors the renewed deadline.
func TestRealExpiryTimerHonorsRenewedDeadline(t *testing.T) {
	d := &fakeDriver{}
	m := New(d)
	defer m.Close(context.Background())
	r := req(Human, "h")
	r.Duration = 100 * time.Millisecond
	l := acquire(t, m, r)
	r.Token = l.Token
	r.Duration = 300 * time.Millisecond
	_ = acquire(t, m, r)
	time.Sleep(150 * time.Millisecond)
	if _, err := m.Validate(creds(l)); err != nil {
		t.Fatal("old timer revoked renewed lease", err)
	}
	eventually(t, func() bool {
		return d.resets.Load() >= 2 && m.View().Owner == ""
	})
}

// TestPartialWriteFailureAndPanicRevokeAndReleaseActualState verifies that partial writes and panics revoke the lease and release input.
func TestPartialWriteFailureAndPanicRevokeAndReleaseActualState(t *testing.T) {
	for _, panicWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panicWrite], func(t *testing.T) {
			var held atomic.Bool
			d := &fakeDriver{resetFn: func(context.Context) error {
				held.Store(false)
				return nil
			}}
			m := New(d)
			defer m.Close(context.Background())
			l := acquire(t, m, req(Human, "h"))
			err := m.DispatchHumanGated(context.Background(), creds(l), func(context.Context) error {
				held.Store(true)
				if panicWrite {
					panic("possibly secret backend data")
				}
				return errors.New("write interrupted")
			})
			if err == nil {
				t.Fatal("write failure hidden")
			}
			if panicWrite && (!errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret")) {
				t.Fatal("panic details leaked", err)
			}
			if held.Load() || m.View().Owner != "" {
				t.Fatal("failed partial write left input or owner active")
			}
			if _, err := m.Validate(creds(l)); !errors.Is(err, ErrStale) {
				t.Fatal(err)
			}
		})
	}
}

// TestPartialWriteAndResetFailureRemainFailClosed verifies that partial-write and reset failures remain fail closed.
func TestPartialWriteAndResetFailureRemainFailClosed(t *testing.T) {
	d := &fakeDriver{}
	m := New(d)
	defer func() {
		d.fail.Store(false)
		_ = m.Close(context.Background())
	}()
	l := acquire(t, m, req(Human, "h"))
	err := m.Dispatch(context.Background(), creds(l), func(context.Context) error {
		d.fail.Store(true)
		return errors.New("partial write")
	})
	if !errors.Is(err, ErrUnavailable) || m.View().Owner != "" || m.View().Ready {
		t.Fatal(err, m.View())
	}
}

// TestFailedOldWriteCannotRevokeNewerPendingHuman verifies that an old failed write cannot revoke a newer pending human owner.
func TestFailedOldWriteCannotRevokeNewerPendingHuman(t *testing.T) {
	var held atomic.Bool
	d := &fakeDriver{resetFn: func(context.Context) error {
		held.Store(false)
		return nil
	}}
	m := New(d)
	defer m.Close(context.Background())
	a := acquire(t, m, req(Agent, "a"))
	entered := make(chan struct{})
	resume := make(chan struct{})
	oldResult := make(chan error, 1)
	go func() {
		oldResult <- m.DispatchGated(context.Background(), creds(a), func(context.Context) error {
			held.Store(true)
			close(entered)
			<-resume
			return errors.New("old partial write")
		})
	}()
	<-entered
	newLease := make(chan Lease, 1)
	newErr := make(chan error, 1)
	go func() {
		l, err := m.Acquire(context.Background(), req(Human, "h"))
		newLease <- l
		newErr <- err
	}()
	eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.pending != nil && m.pending.owner == Human
	})
	close(resume)
	if err := <-oldResult; err == nil || err.Error() != "old partial write" {
		t.Fatal(err)
	}
	if err := <-newErr; err != nil {
		t.Fatal(err)
	}
	h := <-newLease
	if held.Load() {
		t.Fatal("takeover acknowledged without physical release")
	}
	before := d.resets.Load()
	time.Sleep(10 * time.Millisecond)
	if _, err := m.Validate(creds(h)); err != nil {
		t.Fatal("old write error revoked new human", err)
	}
	if d.resets.Load() != before {
		t.Fatal("old failure reset newer human")
	}
}
