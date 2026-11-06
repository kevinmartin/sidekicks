package x11

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/kevinmartin/sidekicks/internal/compute/control"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop"
)

type recorded struct {
	kind, detail byte
	x, y         int16
}
type fakeState struct {
	mu    sync.Mutex
	state State
	extra map[byte]bool
}
type fakeBackend struct {
	shared                                     *fakeState
	mapping                                    map[uint32]Stroke
	calls                                      []recorded
	mu                                         sync.Mutex
	closed                                     chan struct{}
	once                                       sync.Once
	stateErr, errorFake, errorSync, errorImage error
	sticky                                     bool
	blockAt                                    int
	entered                                    chan struct{}
	image                                      RawImage
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{shared: &fakeState{extra: map[byte]bool{}}, closed: make(chan struct{}), entered: make(chan struct{}), mapping: map[uint32]Stroke{
		'a': {Code: 38}, 'A': {Code: 38, Shift: true}, 'b': {Code: 56}, 'B': {Code: 56, Shift: true}, '1': {Code: 10}, '!': {Code: 10, Shift: true}, ' ': {Code: 65},
		';': {Code: 47}, '=': {Code: 21}, '`': {Code: 49}, '[': {Code: 34}, ']': {Code: 35}, '\\': {Code: 51}, '/': {Code: 61}, ',': {Code: 59}, '.': {Code: 60}, '\'': {Code: 48},
		0xffe1: {Code: 50}, 0xffe3: {Code: 37}, 0xffe9: {Code: 64}, 0xffeb: {Code: 133}, 0xff0d: {Code: 36}, 0xff09: {Code: 23},
	}, image: RawImage{Width: 2, Height: 1, Depth: 24, BitsPerPixel: 32, ScanlinePad: 32, RedMask: 0xff0000, GreenMask: 0xff00, BlueMask: 0xff, Data: []byte{0, 0, 255, 0, 0, 255, 0, 0}}}
}

// Size returns the current display dimensions.
func (f *fakeBackend) Size() (int, int) {
	return 1280, 720
}

// Mapping returns the X11 key mapping.
func (f *fakeBackend) Mapping() map[uint32]Stroke {
	return f.mapping
}

// State returns the current input state.
func (f *fakeBackend) State() (State, error) {
	f.shared.mu.Lock()
	defer f.shared.mu.Unlock()
	return f.shared.state, f.stateErr
}

// Fake sends an X11 input event.
func (f *fakeBackend) Fake(kind, detail byte, x, y int16) error {
	f.mu.Lock()
	f.calls = append(f.calls, recorded{kind, detail, x, y})
	count := len(f.calls)
	f.mu.Unlock()
	if f.errorFake != nil {
		return f.errorFake
	}
	f.shared.mu.Lock()
	if !f.sticky {
		switch kind {
		case xproto.KeyPress:
			f.shared.state.Keys[int(detail)/8] |= 1 << uint(detail%8)
		case xproto.KeyRelease:
			f.shared.state.Keys[int(detail)/8] &= ^(1 << uint(detail%8))
		case xproto.ButtonPress:
			if detail <= 5 {
				f.shared.state.Buttons |= 1 << uint(7+detail)
			}
			f.shared.extra[detail] = true
		case xproto.ButtonRelease:
			if detail <= 5 {
				f.shared.state.Buttons &= ^(1 << uint(7+detail))
			}
			delete(f.shared.extra, detail)
		case xproto.MotionNotify:
			f.shared.state.X = int(x)
			f.shared.state.Y = int(y)
		}
	}
	f.shared.mu.Unlock()
	if f.blockAt == count {
		close(f.entered)
		<-f.closed
		return errors.New("backend closed")
	}
	return nil
}

// Sync synchronizes pending X11 requests.
func (f *fakeBackend) Sync() error {
	return f.errorSync
}

// Image returns a captured X11 image.
func (f *fakeBackend) Image() (RawImage, error) {
	return f.image, f.errorImage
}

// Close releases the receiver's resources.
func (f *fakeBackend) Close() error {
	f.once.Do(func() {
		close(f.closed)
	})
	return nil
}
func integer(n int) *int {
	return &n
}
func assertClear(t *testing.T, f *fakeBackend) {
	t.Helper()
	state, _ := f.State()
	for _, value := range state.Keys {
		if value != 0 {
			t.Fatal("held key remains")
		}
	}
	if state.Buttons&0x1f00 != 0 || len(f.shared.extra) != 0 {
		t.Fatal("held pointer button remains")
	}
}

// TestAutomaticShiftRemainsHeldForOverlappingKeys verifies shared modifier ownership.
func TestAutomaticShiftRemainsHeldForOverlappingKeys(t *testing.T) {
	f := newFakeBackend()
	f.mapping['+'] = Stroke{Code: 21, Shift: true}
	f.mapping['-'] = Stroke{Code: 20, Shift: true}
	d := New(f)
	for _, event := range []desktop.Event{{Type: "key", Key: "+", Action: "down"}, {Type: "key", Key: "-", Action: "down"}, {Type: "key", Key: "+", Action: "up"}} {
		if err := d.Input(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	state, err := f.State()
	if err != nil || state.Keys[50/8]&(1<<uint(50%8)) == 0 {
		t.Fatal("shift released while another shifted key remains held")
	}
	if err := d.Input(context.Background(), desktop.Event{Type: "key", Key: "-", Action: "up"}); err != nil {
		t.Fatal(err)
	}
	assertClear(t, f)
}

// TestResetUsesActualServerStateAndAllSupportedButtons verifies reset uses actual server state and all supported buttons.
func TestResetUsesActualServerStateAndAllSupportedButtons(t *testing.T) {
	f := newFakeBackend()
	f.shared.state.Keys[37/8] |= 1 << uint(37%8)
	f.shared.state.Keys[255/8] |= 1 << uint(255%8)
	f.shared.state.Buttons = xproto.ButtonMask1 | xproto.ButtonMask3
	f.shared.extra[8] = true
	d := New(f)
	if err := d.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertClear(t, f)
	seen := map[byte]bool{}
	for _, call := range f.calls {
		if call.kind == xproto.ButtonRelease {
			seen[call.detail] = true
		}
	}
	if len(seen) != 8 {
		t.Fatal("reset relied only on core five-button mask")
	}
}

// TestResetFailureIsFailClosed verifies reset failure is fail closed.
func TestResetFailureIsFailClosed(t *testing.T) {
	for _, reason := range []string{"state", "release", "sync", "still-held"} {
		t.Run(reason, func(t *testing.T) {
			f := newFakeBackend()
			f.shared.state.Keys[38/8] |= 1 << uint(38%8)
			switch reason {
			case "state":
				f.stateErr = errors.New("state unavailable")
			case "release":
				f.errorFake = errors.New("release failed")
			case "sync":
				f.errorSync = errors.New("sync failed")
			case "still-held":
				f.sticky = true
			}
			d := New(f)
			if d.Reset(context.Background()) == nil {
				t.Fatal("failed reset acknowledged")
			}
			select {
			case <-f.closed:
			default:
				t.Fatal("failed backend stayed open")
			}
			if _, err := d.State(context.Background()); !errors.Is(err, ErrUnavailable) {
				t.Fatal("failed backend still accepted operations")
			}
		})
	}
}

// TestNormalizedKeyPointerClickAndWheel verifies normalized key pointer click and wheel.
func TestNormalizedKeyPointerClickAndWheel(t *testing.T) {
	f := newFakeBackend()
	d := New(f)
	events := []desktop.Event{
		{Type: "key", Key: "Control", Action: "down"}, {Type: "key", Key: "Control", Action: "up"},
		{Type: "pointer", Action: "down", Button: "right", X: integer(1279), Y: integer(719)},
		{Type: "pointer", Action: "up", Button: "right", X: integer(1200), Y: integer(700)},
		{Type: "click", Count: 2, Button: "middle", X: integer(50), Y: integer(60)},
		{Type: "scroll", DeltaX: -120, DeltaY: 240, X: integer(50), Y: integer(60)},
		{Type: "pointer", Action: "move", X: integer(15), Y: integer(16)},
	}
	for _, event := range events {
		if err := d.Input(context.Background(), event); err != nil {
			t.Fatalf("%+v: %v", event, err)
		}
	}
	state, err := d.State(context.Background())
	if err != nil || state.X != 15 || state.Y != 16 {
		t.Fatalf("pointer: %+v %v", state, err)
	}
	assertClear(t, f)
	var middle, wheelX, wheelY int
	for _, call := range f.calls {
		if call.kind == xproto.ButtonPress {
			switch call.detail {
			case 2:
				middle++
			case 6:
				wheelX++
			case 5:
				wheelY++
			}
		}
	}
	if middle != 2 || wheelX != 1 || wheelY != 2 {
		t.Fatalf("input mapping middle=%d wheelX=%d wheelY=%d", middle, wheelX, wheelY)
	}
}

// TestTextPreflightAndExactShiftSequence verifies text preflight and exact shift sequence.
func TestTextPreflightAndExactShiftSequence(t *testing.T) {
	f := newFakeBackend()
	d := New(f)
	if err := d.Input(context.Background(), desktop.Event{Type: "text", Text: "Aa!\n\t"}); err != nil {
		t.Fatal(err)
	}
	assertClear(t, f)
	var shiftDown, shiftUp int
	for _, call := range f.calls {
		if call.detail == 50 {
			if call.kind == xproto.KeyPress {
				shiftDown++
			}
			if call.kind == xproto.KeyRelease {
				shiftUp++
			}
		}
	}
	if shiftDown != 2 || shiftUp != 2 {
		t.Fatalf("exact-case shift sequence: %d/%d", shiftDown, shiftUp)
	}
	f = newFakeBackend()
	d = New(f)
	if err := d.Input(context.Background(), desktop.Event{Type: "text", Text: "abz"}); !errors.Is(err, desktop.ErrInput) {
		t.Fatalf("unmapped text: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatal("text partially injected before layout preflight failed")
	}
	f = newFakeBackend()
	f.shared.state.Buttons = xproto.ModMaskLock
	d = New(f)
	if err := d.Input(context.Background(), desktop.Event{Type: "text", Text: "a"}); !errors.Is(err, desktop.ErrInput) {
		t.Fatal("Caps Lock silently changed exact text")
	}
	if len(f.calls) != 0 {
		t.Fatal("Caps Lock rejection emitted input")
	}
}

// TestInvalidEventRejectedBeforeAnyBackendAction verifies invalid event rejected before any backend action.
func TestInvalidEventRejectedBeforeAnyBackendAction(t *testing.T) {
	f := newFakeBackend()
	d := New(f)
	if err := d.Input(context.Background(), desktop.Event{Type: "pointer", Action: "move", X: integer(1280), Y: integer(0)}); !errors.Is(err, desktop.ErrInput) {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatal("invalid event emitted input")
	}
	if err := d.Input(context.Background(), desktop.Event{Type: "text", Text: "✓"}); !errors.Is(err, desktop.ErrInput) {
		t.Fatal("unsupported Unicode accepted")
	}
}

// TestCanceledCheckedInputRemainsUnreadyAndCannotReconnect verifies canceled checked input remains unready and cannot reconnect.
func TestCanceledCheckedInputRemainsUnreadyAndCannotReconnect(t *testing.T) {
	f := newFakeBackend()
	f.blockAt = 1
	driver := New(f)
	var reconnects atomic.Int64
	next := newFakeBackend()
	next.shared = f.shared
	driver.reconnect = func(context.Context) (Backend, error) {
		reconnects.Add(1)
		return next, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- driver.Input(ctx, desktop.Event{Type: "key", Key: "Control", Action: "down"})
	}()
	select {
	case <-f.entered:
	case <-time.After(time.Second):
		t.Fatal("checked input never paused")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled I/O remained blocked")
	}
	if err := driver.Reset(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("uncertain old input was acknowledged after a new-connection reset")
	}
	if err := driver.Input(context.Background(), desktop.Event{Type: "key", Key: "a", Action: "down"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("uncertain driver accepted new input")
	}
	if _, err := driver.State(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("uncertain driver reported ready state")
	}
	if reconnects.Load() != 0 {
		t.Fatal("uncertain input caused automatic cross-connection recovery")
	}
}

// TestPausedInputPreemptionCannotAcknowledgeNewLease verifies paused input preemption cannot acknowledge new lease.
func TestPausedInputPreemptionCannotAcknowledgeNewLease(t *testing.T) {
	f := newFakeBackend()
	driver := New(f)
	gate := control.New(driver)
	defer gate.Close(context.Background())
	lease, err := gate.Acquire(context.Background(), control.Request{Owner: control.Agent, Session: "old-agent", Mode: control.Gated, Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	f.blockAt = len(f.calls) + 1
	var reconnects atomic.Int64
	driver.reconnect = func(context.Context) (Backend, error) {
		reconnects.Add(1)
		return newFakeBackend(), nil
	}
	done := make(chan error, 1)
	go func() {
		done <- gate.DispatchGated(context.Background(), control.Credentials{Session: lease.Session, Token: lease.Token}, func(ctx context.Context) error {
			return driver.Input(ctx, desktop.Event{Type: "key", Key: "Control", Action: "down"})
		})
	}()
	select {
	case <-f.entered:
	case <-time.After(time.Second):
		t.Fatal("checked input was not paused before server acknowledgement")
	}
	if err := gate.Stop(context.Background()); err == nil {
		t.Fatal("uncertain in-flight input stop falsely acknowledged readiness")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("uncertain old dispatch reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("uncertain old dispatch did not exit")
	}
	if gate.View().Ready || gate.View().Owner != "" {
		t.Fatal("uncertain driver left a ready or owned desktop")
	}
	if _, err := gate.Acquire(context.Background(), control.Request{Owner: control.Human, Session: "new-human", Mode: control.Gated, Duration: time.Minute}); err == nil {
		t.Fatal("new human lease acknowledged before guest restart")
	}
	if reconnects.Load() != 0 {
		t.Fatal("preemption reset opened another connection ahead of uncertain old input")
	}
}

// TestCloseInterruptsBlockedBackendOutsideIOLock verifies close interrupts blocked backend outside io lock.
func TestCloseInterruptsBlockedBackendOutsideIOLock(t *testing.T) {
	f := newFakeBackend()
	f.blockAt = 1
	d := New(f)
	done := make(chan error, 1)
	go func() {
		done <- d.Input(context.Background(), desktop.Event{Type: "key", Key: "Control", Action: "down"})
	}()
	select {
	case <-f.entered:
	case <-time.After(time.Second):
		t.Fatal("input never blocked")
	}
	start := time.Now()
	err := d.Close()
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("Close failed to bound blocked reset: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close left blocked input goroutine")
	}
	if err := d.Reset(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed driver reopened")
	}
	if err := d.Close(); err != nil {
		t.Fatal("Close not idempotent")
	}
}

// TestCaptureFreshBoundedPNG verifies capture fresh bounded png.
func TestCaptureFreshBoundedPNG(t *testing.T) {
	f := newFakeBackend()
	d := New(f)
	data, err := d.CapturePNG(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 1 {
		t.Fatal("capture bounds changed")
	}
	if color.RGBAModel.Convert(img.At(0, 0)).(color.RGBA) != (color.RGBA{R: 255, A: 255}) {
		t.Fatal("red pixel decoded incorrectly")
	}
	if color.RGBAModel.Convert(img.At(1, 0)).(color.RGBA) != (color.RGBA{G: 255, A: 255}) {
		t.Fatal("green pixel decoded incorrectly")
	}
	f.image.Data[0], f.image.Data[2] = 255, 0
	second, err := d.CapturePNG(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(data, second) {
		t.Fatal("capture reused stale image")
	}
}

// TestOperationBoundsAndUnavailableBackend verifies operation bounds and unavailable backend.
func TestOperationBoundsAndUnavailableBackend(t *testing.T) {
	d := New(nil)
	if err := d.Reset(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.State(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	f := newFakeBackend()
	d = New(f)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Input(context.Background(), desktop.Event{Type: "key", Key: "Control", Action: "down"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed input accepted")
	}
	for _, display := range []string{"", ":1.1", "localhost:0", "tcp/host:1", "/tmp/other:0", "unix/:0", ":1000"} {
		if _, err := Open(context.Background(), display, ""); err == nil {
			t.Fatalf("non-local display %q accepted", display)
		}
	}
}

// TestPhysicalPunctuationUsesBaseKeysAndPreservesHeldShift verifies physical punctuation uses base keys and preserves held shift.
func TestPhysicalPunctuationUsesBaseKeysAndPreservesHeldShift(t *testing.T) {
	f := newFakeBackend()
	driver := New(f)
	if err := driver.Input(context.Background(), desktop.Event{Type: "key", Key: "Shift", Action: "down"}); err != nil {
		t.Fatal(err)
	}
	for _, symbol := range ";=`[]\\/,.'" {
		stroke := f.mapping[uint32(symbol)]
		if err := driver.Input(context.Background(), desktop.Event{Type: "key", Key: string(symbol), Action: "down"}); err != nil {
			t.Fatalf("physical punctuation %q: %v", symbol, err)
		}
		state, _ := driver.State(context.Background())
		if state.Keys[stroke.Code/8]&(1<<uint(stroke.Code%8)) == 0 || state.Keys[50/8]&(1<<uint(50%8)) == 0 {
			t.Fatalf("punctuation %q did not preserve physical Shift/base key", symbol)
		}
		if err := driver.Input(context.Background(), desktop.Event{Type: "key", Key: string(symbol), Action: "up"}); err != nil {
			t.Fatal(err)
		}
		state, _ = driver.State(context.Background())
		if state.Keys[stroke.Code/8]&(1<<uint(stroke.Code%8)) != 0 || state.Keys[50/8]&(1<<uint(50%8)) == 0 {
			t.Fatalf("punctuation %q released physical Shift or retained base key", symbol)
		}
	}
	if err := driver.Input(context.Background(), desktop.Event{Type: "key", Key: "Shift", Action: "up"}); err != nil {
		t.Fatal(err)
	}
	assertClear(t, f)
}

// TestReadyRemainsFalseAfterUncertainReadOnlyIO verifies ready remains false after uncertain read only io.
func TestReadyRemainsFalseAfterUncertainReadOnlyIO(t *testing.T) {
	f := newFakeBackend()
	driver := New(f)
	if !driver.Ready() {
		t.Fatal("fresh verified backend not ready")
	}
	f.errorImage = errors.New("read-only capture failed during x11 i/o")
	if _, err := driver.CapturePNG(context.Background()); err == nil {
		t.Fatal("failed capture returned success")
	}
	f.errorImage = nil
	if driver.Ready() {
		t.Fatal("poisoned driver reported ready without a guest restart")
	}
	if err := driver.Reset(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("uncertain driver recovered via new reset")
	}
	if New(nil).Ready() {
		t.Fatal("missing backend ready")
	}
	fresh := New(newFakeBackend())
	if err := fresh.Close(); err != nil {
		t.Fatal(err)
	}
	if fresh.Ready() {
		t.Fatal("closed backend ready")
	}
}
