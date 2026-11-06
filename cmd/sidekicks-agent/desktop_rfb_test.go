package main

import (
	"context"
	"testing"
)

// TestRFBServerArgumentsForceVideoOnlyAndGuestLoopback verifies rfb server arguments force video only and guest loopback.
func TestRFBServerArgumentsForceVideoOnlyAndGuestLoopback(t *testing.T) {
	arguments := rfbServerArgs()
	flags := map[string]int{}
	values := map[string]string{}
	for i, argument := range arguments {
		if argument == "-display" || argument == "-rfbport" {
			if i+1 >= len(arguments) {
				t.Fatal("missing fixed RFB argument value")
			}
			values[argument] = arguments[i+1]
		}
		flags[argument]++
	}
	if flags["-viewonly"] != 1 {
		t.Fatal("raw RFB server input must stay disabled independently of the Go bridge")
	}
	if flags["-localhost"] != 1 || values["-display"] != ":99" || values["-rfbport"] != "5900" {
		t.Fatal("RFB server must remain on the private guest display/loopback endpoint")
	}
	if flags["-shared"] != 1 || flags["-forever"] != 1 || flags["-nowireframe"] != 1 {
		t.Fatal("view-only server must retain shared persistent live-content video behavior")
	}
}

// TestDesktopHealthIncludesPoisonedX11WithoutGateDispatch verifies desktop health includes poisoned x11 without gate dispatch.
func TestDesktopHealthIncludesPoisonedX11WithoutGateDispatch(t *testing.T) {
	desktop, backend, _, _ := newUnitDesktop(t)
	if err := desktop.ready(); err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	backend.fail = true
	backend.mu.Unlock()
	if _, err := desktop.driver.CapturePNG(context.Background()); err == nil {
		t.Fatal("capture failure not observed")
	}
	backend.mu.Lock()
	backend.fail = false
	backend.mu.Unlock()
	if !desktop.gate.View().Ready {
		t.Fatal("test must cover driver failure before any gate dispatch")
	}
	if desktop.driver.Ready() {
		t.Fatal("poisoned X11 backend became ready")
	}
	if err := desktop.ready(); err == nil {
		t.Fatal("desktop health ignored poisoned X11 after read-only failure")
	}
}
