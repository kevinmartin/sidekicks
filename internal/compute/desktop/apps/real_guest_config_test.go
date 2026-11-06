package apps

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
)

// TestWaitForActivationRequiresActualActiveAndViewableClient verifies wait for activation requires actual active and viewable client.
func TestWaitForActivationRequiresActualActiveAndViewableClient(t *testing.T) {
	steps := []struct {
		id       uint32
		viewable bool
	}{{7, true}, {42, false}, {42, true}}
	index := 0
	observe := func(context.Context) (uint32, bool, error) {
		step := steps[index]
		index++
		return step.id, step.viewable, nil
	}
	if err := waitForActivation(context.Background(), 42, time.Second, time.Millisecond, observe); err != nil || index != 3 {
		t.Fatalf("activation was not confirmed correctly: steps=%d error=%v", index, err)
	}
	if err := waitForActivation(context.Background(), 42, 5*time.Millisecond, time.Millisecond, func(context.Context) (uint32, bool, error) {
		return 7, true, nil
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("ignored request acknowledged: %v", err)
	}
	if err := waitForActivation(context.Background(), 42, time.Second, time.Millisecond, func(context.Context) (uint32, bool, error) {
		return 0, false, errors.New("x failure")
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("observation error acknowledged: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if err := waitForActivation(ctx, 42, time.Second, time.Millisecond, func(context.Context) (uint32, bool, error) {
		called = true
		return 42, true, nil
	}); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled activation observed/acknowledged: %v called=%t", err, called)
	}
	if err := waitForActivation(context.Background(), 42, 5*time.Millisecond, time.Millisecond, func(observeCtx context.Context) (uint32, bool, error) {
		<-observeCtx.Done()
		return 42, true, nil
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stalled observation acknowledged after deadline: %v", err)
	}
	if err := waitForActivation(context.Background(), 42, 5*time.Millisecond, time.Millisecond, func(context.Context) (uint32, bool, error) {
		time.Sleep(10 * time.Millisecond)
		return 42, true, nil
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("late successful observation acknowledged: %v", err)
	}
}

// TestRealActiveWindowAcceptsSingleValueOrXfwmTimestamp verifies real active window accepts single value or xfwm timestamp.
func TestRealActiveWindowAcceptsSingleValueOrXfwmTimestamp(t *testing.T) {
	for _, v := range [][]byte{{3, 0, 96, 1}, {3, 0, 96, 1, 1, 0, 0, 0}} {
		id, err := realDecodeActiveWindow(&xproto.GetPropertyReply{Format: 32, Type: xproto.AtomWindow, Value: v})
		if err != nil || id != 0x01600003 {
			t.Fatal(id, err)
		}
	}
	for _, p := range []*xproto.GetPropertyReply{nil, {Format: 32, Type: xproto.AtomWindow, Value: []byte{3, 0, 96, 1, 1, 0, 0, 0, 1, 0, 0, 0}}, {Format: 32, Type: xproto.AtomWindow, BytesAfter: 4, Value: []byte{3, 0, 96, 1}}, {Format: 8, Type: xproto.AtomWindow, Value: []byte{3, 0, 96, 1}}} {
		if _, err := realDecodeActiveWindow(p); err == nil {
			t.Fatal("invalid active-window property accepted")
		}
	}
}

// TestRealGuestBrowserSelectionRetainsChromiumDefaultAndFailsClosed verifies real guest browser selection retains chromium default and fails closed.
func TestRealGuestBrowserSelectionRetainsChromiumDefaultAndFailsClosed(t *testing.T) {
	for _, test := range []struct {
		input string
		want  BrowserKind
	}{{"", Chromium}, {"chromium", Chromium}, {"firefox", Firefox}} {
		got, err := realGuestBrowser(test.input)
		if err != nil || got != test.want {
			t.Fatal(test, got, err)
		}
	}
	for _, invalid := range []string{"Firefox", " firefox", "firefox ", "chrome", "browser-firefox", "firefox; sh", "../firefox"} {
		if browser, err := realGuestBrowser(invalid); err == nil || browser != "" {
			t.Fatal("integration browser silently fell back", invalid, browser)
		}
	}
}
