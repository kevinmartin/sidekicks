package x11

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop"
)

var errCheckedFailure = errors.New("injected checked xtest failure")

type faultBackend struct {
	*fakeBackend
	match    func(recorded) bool
	after    bool
	attempts []recorded
	faultAt  int
}

// Fake sends an X11 input event.
func (f *faultBackend) Fake(kind, detail byte, x, y int16) error {
	call := recorded{kind, detail, x, y}
	f.attempts = append(f.attempts, call)
	if f.match != nil && f.match(call) {
		f.faultAt = len(f.attempts)
		if f.after {
			if err := f.fakeBackend.Fake(kind, detail, x, y); err != nil {
				return err
			}
		}
		return errCheckedFailure
	}
	return f.fakeBackend.Fake(kind, detail, x, y)
}
func assertPoisonedNoRecovery(t *testing.T, driver *Driver) {
	t.Helper()
	var reconnects atomic.Int64
	driver.reconnect = func(context.Context) (Backend, error) {
		reconnects.Add(1)
		return newFakeBackend(), nil
	}
	if driver.Ready() {
		t.Fatal("uncertain checked operation still ready")
	}
	if err := driver.Reset(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("uncertain reset acknowledged: %v", err)
	}
	if err := driver.Input(context.Background(), desktop.Event{Type: "key", Key: "a", Action: "down"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("uncertain driver accepted later input: %v", err)
	}
	if reconnects.Load() != 0 {
		t.Fatal("uncertain operation reconnected ahead of old server work")
	}
}

// TestPartialTextCheckedFailuresNeverContinueOrAcknowledgeRecovery verifies partial text checked failures never continue or acknowledge recovery.
func TestPartialTextCheckedFailuresNeverContinueOrAcknowledgeRecovery(t *testing.T) {
	cases := []struct {
		name       string
		kind, code byte
	}{
		{"shift-press", xproto.KeyPress, 50},
		{"letter-press", xproto.KeyPress, 38},
		{"letter-release", xproto.KeyRelease, 38},
		{"shift-release", xproto.KeyRelease, 50},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			backend := &faultBackend{fakeBackend: newFakeBackend(), after: true, match: func(call recorded) bool {
				return call.kind == test.kind && call.detail == test.code
			}}
			driver := New(backend)
			// Applying the event and then failing simulates an acknowledgement lost
			// after the server may have consumed input. Never assume it was dropped.
			err := driver.Input(context.Background(), desktop.Event{Type: "text", Text: "AB"})
			if !errors.Is(err, errCheckedFailure) {
				t.Fatalf("checked text failure hidden: %v", err)
			}
			if backend.faultAt == 0 || len(backend.attempts) != backend.faultAt {
				t.Fatal("text continued after uncertain checked operation")
			}
			assertPoisonedNoRecovery(t, driver)
		})
	}
}

// TestPartialPointerCheckedFailuresNeverContinue verifies partial pointer checked failures never continue.
func TestPartialPointerCheckedFailuresNeverContinue(t *testing.T) {
	cases := []struct {
		name         string
		event        desktop.Event
		kind, detail byte
	}{
		{"move", desktop.Event{Type: "pointer", Action: "move", X: integer(50), Y: integer(60)}, xproto.MotionNotify, 0},
		{"press", desktop.Event{Type: "pointer", Action: "down", Button: "right", X: integer(50), Y: integer(60)}, xproto.ButtonPress, 3},
		{"release", desktop.Event{Type: "pointer", Action: "up", Button: "right", X: integer(50), Y: integer(60)}, xproto.ButtonRelease, 3},
		{"click-press", desktop.Event{Type: "click", X: integer(50), Y: integer(60)}, xproto.ButtonPress, 1},
		{"click-release", desktop.Event{Type: "click", X: integer(50), Y: integer(60)}, xproto.ButtonRelease, 1},
		{"wheel-press", desktop.Event{Type: "scroll", DeltaX: -120, X: integer(50), Y: integer(60)}, xproto.ButtonPress, 6},
		{"wheel-release", desktop.Event{Type: "scroll", DeltaY: 120, X: integer(50), Y: integer(60)}, xproto.ButtonRelease, 5},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			backend := &faultBackend{fakeBackend: newFakeBackend(), after: true, match: func(call recorded) bool {
				return call.kind == test.kind && call.detail == test.detail
			}}
			driver := New(backend)
			if err := driver.Input(context.Background(), test.event); !errors.Is(err, errCheckedFailure) {
				t.Fatalf("pointer failure hidden: %v", err)
			}
			if backend.faultAt == 0 || len(backend.attempts) != backend.faultAt {
				t.Fatal("pointer batch continued after uncertain operation")
			}
			assertPoisonedNoRecovery(t, driver)
		})
	}
}

type firstClickBackend struct {
	*fakeBackend
	firstReleased chan struct{}
	proceed       chan struct{}
	once          sync.Once
}

// Fake sends an X11 input event.
func (f *firstClickBackend) Fake(kind, detail byte, x, y int16) error {
	if err := f.fakeBackend.Fake(kind, detail, x, y); err != nil {
		return err
	}
	if kind == xproto.ButtonRelease && detail == 1 {
		f.once.Do(func() {
			close(f.firstReleased)
			select {
			case <-f.proceed:
			case <-f.closed:
			}
		})
	}
	return nil
}

// TestCanceledDoubleClickCannotEmitQueuedSecondClick verifies canceled double click cannot emit queued second click.
func TestCanceledDoubleClickCannotEmitQueuedSecondClick(t *testing.T) {
	backend := &firstClickBackend{fakeBackend: newFakeBackend(), firstReleased: make(chan struct{}), proceed: make(chan struct{})}
	driver := New(backend)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- driver.Input(ctx, desktop.Event{Type: "click", Count: 2, X: integer(50), Y: integer(60)})
	}()
	select {
	case <-backend.firstReleased:
	case <-time.After(time.Second):
		t.Fatal("first click never completed")
	}
	cancel()
	select {
	case <-backend.closed:
	case <-time.After(time.Second):
		t.Fatal("backend was not closed after cancellation")
	}
	close(backend.proceed)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued click cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled double click did not exit")
	}
	presses := 0
	for _, call := range backend.calls {
		if call.kind == xproto.ButtonPress && call.detail == 1 {
			presses++
		}
	}
	if presses != 1 {
		t.Fatalf("canceled queued second click was emitted: %d presses", presses)
	}
	assertPoisonedNoRecovery(t, driver)
}

type verificationBackend struct {
	*fakeBackend
	queries int
}

// State returns the current input state.
func (f *verificationBackend) State() (State, error) {
	f.queries++
	if f.queries == 2 {
		return State{}, errors.New("verification query failed")
	}
	return f.fakeBackend.State()
}

// TestResetRequiresActualPostReleaseVerification verifies reset requires actual post release verification.
func TestResetRequiresActualPostReleaseVerification(t *testing.T) {
	backend := &verificationBackend{fakeBackend: newFakeBackend()}
	backend.shared.state.Buttons = xproto.ButtonMask1
	driver := New(backend)
	if err := driver.Reset(context.Background()); err == nil {
		t.Fatal("reset acknowledged without post-release query")
	}
	if backend.queries != 2 {
		t.Fatal("reset did not attempt verification")
	}
	assertPoisonedNoRecovery(t, driver)
}

// TestResetRejectsStickyButtonWithoutStickyKeys verifies reset rejects sticky button without sticky keys.
func TestResetRejectsStickyButtonWithoutStickyKeys(t *testing.T) {
	backend := newFakeBackend()
	backend.shared.state.Buttons = xproto.ButtonMask3
	backend.sticky = true
	driver := New(backend)
	if err := driver.Reset(context.Background()); err == nil {
		t.Fatal("actual sticky button was ignored because keys were clear")
	}
	assertPoisonedNoRecovery(t, driver)
}

// TestResetReleaseRequestFailureDoesNotAcknowledgeClearState verifies reset release request failure does not acknowledge clear state.
func TestResetReleaseRequestFailureDoesNotAcknowledgeClearState(t *testing.T) {
	backend := &faultBackend{fakeBackend: newFakeBackend(), match: func(call recorded) bool {
		return call.kind == xproto.ButtonRelease && call.detail == 1
	}}
	driver := New(backend)
	if err := driver.Reset(context.Background()); !errors.Is(err, errCheckedFailure) {
		t.Fatal("button release protocol failure hidden")
	}
	assertPoisonedNoRecovery(t, driver)
}

// TestReleaseAllInputUsesActualStateAndFailurePolicy verifies release all input uses actual state and failure policy.
func TestReleaseAllInputUsesActualStateAndFailurePolicy(t *testing.T) {
	backend := newFakeBackend()
	backend.shared.state.Keys[37/8] |= 1 << uint(37%8)
	backend.shared.state.Buttons = xproto.ButtonMask1
	driver := New(backend)
	if err := driver.Input(context.Background(), desktop.Event{Type: "releaseAll"}); err != nil {
		t.Fatal(err)
	}
	assertClear(t, backend)
	failing := &verificationBackend{fakeBackend: newFakeBackend()}
	failedDriver := New(failing)
	if err := failedDriver.Input(context.Background(), desktop.Event{Type: "releaseAll"}); err == nil {
		t.Fatal("releaseAll skipped failed actual verification")
	}
	assertPoisonedNoRecovery(t, failedDriver)
}

// TestPreflightLayoutFailureMayRecoverOnlyWithoutEmittingInput verifies preflight layout failure may recover only without emitting input.
func TestPreflightLayoutFailureMayRecoverOnlyWithoutEmittingInput(t *testing.T) {
	for _, failReconnect := range []bool{false, true} {
		t.Run(map[bool]string{false: "known-no-input", true: "reconnect-failed"}[failReconnect], func(t *testing.T) {
			backend := newFakeBackend()
			driver := New(backend)
			if err := driver.Input(context.Background(), desktop.Event{Type: "key", Key: "F12", Action: "down"}); !errors.Is(err, desktop.ErrInput) {
				t.Fatalf("layout preflight error: %v", err)
			}
			if len(backend.calls) != 0 {
				t.Fatal("layout validation emitted input")
			}
			var reconnects int
			fresh := newFakeBackend()
			driver.reconnect = func(context.Context) (Backend, error) {
				reconnects++
				if failReconnect {
					return nil, errors.New("connection unavailable")
				}
				return fresh, nil
			}
			err := driver.Reset(context.Background())
			if failReconnect {
				if !errors.Is(err, ErrUnavailable) || driver.Ready() {
					t.Fatal("failed reconnect acknowledged ready")
				}
			} else {
				if err != nil || !driver.Ready() {
					t.Fatalf("known preflight-only recovery failed: %v", err)
				}
				assertClear(t, fresh)
			}
			if reconnects != 1 {
				t.Fatal("preflight recovery retried or skipped the one explicit reset")
			}
		})
	}
}

// TestLegacyShiftedKeyContractReleasesItsOwnModifier verifies legacy shifted key contract releases its own modifier.
func TestLegacyShiftedKeyContractReleasesItsOwnModifier(t *testing.T) {
	backend := newFakeBackend()
	backend.mapping['+'] = Stroke{Code: 21, Shift: true}
	driver := New(backend)
	for _, action := range []string{"down", "up"} {
		if err := driver.Input(context.Background(), desktop.Event{Type: "key", Key: "+", Action: action}); err != nil {
			t.Fatal(err)
		}
	}
	assertClear(t, backend)
	backend = newFakeBackend()
	backend.mapping['+'] = Stroke{Code: 21, Shift: true}
	delete(backend.mapping, 0xffe1)
	driver = New(backend)
	if err := driver.Input(context.Background(), desktop.Event{Type: "key", Key: "+", Action: "down"}); !errors.Is(err, desktop.ErrInput) {
		t.Fatal("missing modifier map was not rejected")
	}
	if len(backend.calls) != 0 {
		t.Fatal("unsupported shifted key partially injected")
	}
}

// TestCanceledRequestBeforeInputLeavesHealthyDriverUsable verifies canceled request before input leaves healthy driver usable.
func TestCanceledRequestBeforeInputLeavesHealthyDriverUsable(t *testing.T) {
	backend := newFakeBackend()
	driver := New(backend)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := driver.Input(ctx, desktop.Event{Type: "key", Key: "a", Action: "down"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(backend.calls) != 0 || !driver.Ready() {
		t.Fatal("request canceled before I/O poisoned a healthy driver")
	}
	if width, height := driver.Size(); width != 1280 || height != 720 {
		t.Fatal("validated desktop bounds changed")
	}
	if err := driver.Input(context.Background(), desktop.Event{Type: "scroll", DeltaX: 2400, X: integer(50), Y: integer(60)}); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, call := range backend.calls {
		if call.kind == xproto.ButtonPress && call.detail == 7 {
			count++
		}
	}
	if count != 20 {
		t.Fatalf("bounded single-axis scroll emitted %d wheel presses", count)
	}
	assertClear(t, backend)
}

// TestCaptureRejectsMalformedPixelsWithoutReturningImage verifies capture rejects malformed pixels without returning image.
func TestCaptureRejectsMalformedPixelsWithoutReturningImage(t *testing.T) {
	backend := newFakeBackend()
	backend.image.BitsPerPixel = 8
	driver := New(backend)
	data, err := driver.CapturePNG(context.Background())
	if err == nil || len(data) != 0 {
		t.Fatal("malformed capture payload produced an image")
	}
	assertPoisonedNoRecovery(t, driver)
	for _, channel := range []string{"green", "blue"} {
		raw := newFakeBackend().image
		if channel == "green" {
			raw.GreenMask = 0xa000
		} else {
			raw.BlueMask = 0xa0
		}
		if _, err := decodeImage(raw); err == nil {
			t.Fatalf("noncontiguous %s mask accepted", channel)
		}
	}
}

// TestLegacyPlusPreservesExplicitShiftOwnership verifies legacy plus preserves explicit shift ownership.
func TestLegacyPlusPreservesExplicitShiftOwnership(t *testing.T) {
	for _, order := range []string{"shift-before-plus", "shift-claims-after-plus"} {
		t.Run(order, func(t *testing.T) {
			backend := newFakeBackend()
			backend.mapping['+'] = Stroke{Code: 21, Shift: true}
			driver := New(backend)
			defer driver.Close()
			input := func(key, action string) {
				t.Helper()
				if err := driver.Input(context.Background(), desktop.Event{Type: "key", Key: key, Action: action}); err != nil {
					t.Fatal(err)
				}
			}
			if order == "shift-before-plus" {
				input("Shift", "down")
			}
			input("+", "down")
			if order == "shift-claims-after-plus" {
				input("Shift", "down")
			}
			input("+", "up")
			current, err := driver.State(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if current.Keys[50/8]&(1<<uint(50%8)) == 0 {
				t.Fatal("symbol keyup released independently held explicit Shift")
			}
			if current.Keys[21/8]&(1<<uint(21%8)) != 0 {
				t.Fatal("plus base key remained held")
			}
			input("Shift", "up")
			assertClear(t, backend)
		})
	}
}
