package x11

import (
	"bytes"
	"context"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop"
)

// TestRealGuestX11 is deliberately opt-in. Run only inside a disposable test
// workload; it creates a synthetic override-redirect window and injects input.
// Ordinary unit tests never open a host display or claim this check passed.
func TestRealGuestX11(t *testing.T) {
	display := os.Getenv("SIDEKICKS_X11_TEST_DISPLAY")
	if display == "" {
		t.Skip("requires explicit disposable guest X11 display")
	}
	if os.Getuid() != 1000 {
		t.Fatal("integration must run as the non-root workload user")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	driver, err := Open(ctx, display, "")
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	backend := driver.current.backend.(*backend)
	supportedVisual := false
	for _, depth := range backend.screen.AllowedDepths {
		for _, visual := range depth.Visuals {
			if visual.VisualId == backend.screen.RootVisual && depth.Depth == 24 && visual.Class == xproto.VisualClassTrueColor && visual.RedMask == 0xff0000 && visual.GreenMask == 0xff00 && visual.BlueMask == 0xff {
				supportedVisual = true
			}
		}
	}
	if !supportedVisual {
		t.Skip("synthetic color assertion requires a 24-bit RGB TrueColor root visual")
	}
	id, err := backend.conn.NewId()
	if err != nil {
		t.Fatal(err)
	}
	window := xproto.Window(id)
	attributes := []uint32{0x13579b, 1, xproto.EventMaskKeyPress | xproto.EventMaskKeyRelease | xproto.EventMaskButtonPress | xproto.EventMaskButtonRelease | xproto.EventMaskPointerMotion}
	if err := xproto.CreateWindowChecked(backend.conn, backend.screen.RootDepth, window, backend.screen.Root, 20, 20, 320, 160, 0, xproto.WindowClassInputOutput, backend.screen.RootVisual, xproto.CwBackPixel|xproto.CwOverrideRedirect|xproto.CwEventMask, attributes).Check(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = xproto.DestroyWindowChecked(backend.conn, window).Check()
	}()
	if err := xproto.MapWindowChecked(backend.conn, window).Check(); err != nil {
		t.Fatal(err)
	}
	if err := xproto.SetInputFocusChecked(backend.conn, xproto.InputFocusPointerRoot, window, 0).Check(); err != nil {
		t.Fatal(err)
	}
	if err := backend.Sync(); err != nil {
		t.Fatal(err)
	}

	// Hold through two routes: one normalized driver event and an independent
	// XTEST call through the backend. Reset must inspect actual server state.
	if err := driver.Input(ctx, desktop.Event{Type: "key", Key: "Control", Action: "down"}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Fake(xproto.MotionNotify, 0, 50, 50); err != nil {
		t.Fatal(err)
	}
	if err := backend.Fake(xproto.ButtonPress, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	state, err := driver.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	control := backend.Mapping()[0xffe3].Code
	if state.Keys[int(control)/8]&(1<<uint(control%8)) == 0 || state.Buttons&xproto.ButtonMask1 == 0 {
		t.Fatalf("actual mixed hold not established: %+v", state)
	}
	if err := driver.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = driver.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, bits := range state.Keys {
		if bits != 0 {
			t.Fatalf("actual held key remains: %x", state.Keys)
		}
	}
	if state.Buttons&0x1f00 != 0 {
		t.Fatalf("actual held button remains: %x", state.Buttons)
	}

	if err := driver.Input(ctx, desktop.Event{Type: "text", Text: "Aa!1\n"}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Sync(); err != nil {
		t.Fatal(err)
	}
	var presses []xproto.KeyPressEvent
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		event, err := backend.conn.PollForEvent()
		if err != nil {
			t.Fatal(err)
		}
		if event == nil {
			if len(presses) >= 5 {
				break
			}
			time.Sleep(time.Millisecond)
			continue
		}
		if key, ok := event.(xproto.KeyPressEvent); ok {
			presses = append(presses, key)
		}
	}
	a := backend.Mapping()['a'].Code
	digit := backend.Mapping()['1'].Code
	sawUpper, sawLower, sawBang, sawDigit := false, false, false, false
	for _, event := range presses {
		switch byte(event.Detail) {
		case a:
			if event.State&xproto.ModMaskShift != 0 {
				sawUpper = true
			} else {
				sawLower = true
			}
		case digit:
			if event.State&xproto.ModMaskShift != 0 {
				sawBang = true
			} else {
				sawDigit = true
			}
		}
	}
	if !sawUpper || !sawLower || !sawBang || !sawDigit {
		t.Fatalf("actual X11 exact-case/punctuation events missing: %+v", presses)
	}

	// A symbolic plus must not release an explicitly held Shift. This uses
	// actual server state rather than relying only on emitted XTEST calls.
	shift := backend.Mapping()[0xffe1].Code
	plus := backend.Mapping()['+'].Code
	for _, event := range []desktop.Event{{Type: "key", Key: "Shift", Action: "down"}, {Type: "key", Key: "+", Action: "down"}, {Type: "key", Key: "+", Action: "up"}} {
		if err := driver.Input(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	state, err = driver.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Keys[int(shift)/8]&(1<<uint(shift%8)) == 0 {
		t.Fatal("normalized plus keyup released actual explicitly held Shift")
	}
	if state.Keys[int(plus)/8]&(1<<uint(plus%8)) != 0 {
		t.Fatal("normalized plus base key remained held")
	}
	if err := driver.Input(ctx, desktop.Event{Type: "key", Key: "Shift", Action: "up"}); err != nil {
		t.Fatal(err)
	}
	state, err = driver.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, bits := range state.Keys {
		if bits != 0 {
			t.Fatal("explicit Shift release left an actual held key")
		}
	}
	// With no explicit Shift owner, symbolic plus must release only the
	// synthetic modifier it introduced.
	for _, action := range []string{"down", "up"} {
		if err := driver.Input(ctx, desktop.Event{Type: "key", Key: "+", Action: action}); err != nil {
			t.Fatal(err)
		}
	}
	state, err = driver.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, bits := range state.Keys {
		if bits != 0 {
			t.Fatal("synthetic-only plus left an actual held modifier/key")
		}
	}
	t.Log("real guest modifier ownership: normalized plus preserved explicit Shift until its own release; synthetic-only Shift cleared")

	data, err := driver.CapturePNG(ctx)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	width, height := driver.Size()
	if img.Bounds().Dx() != width || img.Bounds().Dy() != height {
		t.Fatalf("fresh root capture bounds: %v", img.Bounds())
	}
	want := color.RGBA{R: 0x13, G: 0x57, B: 0x9b, A: 255}
	for _, point := range [][2]int{{25, 25}, {335, 25}, {25, 175}, {335, 175}} {
		if got := color.RGBAModel.Convert(img.At(point[0], point[1])).(color.RGBA); got != want {
			t.Fatalf("capture pixel %v: %+v want %+v", point, got, want)
		}
	}
	if err := driver.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("real guest X11: actual mixed hold released, exact-case XTEST events, fresh %dx%d PNG with four synthetic color corners", width, height)
}
