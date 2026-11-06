package desktop

import (
	"errors"
	"testing"
)

func coordinate(n int) *int {
	return &n
}

// TestNormalizedInputContract verifies normalized input contract.
func TestNormalizedInputContract(t *testing.T) {
	good := []Event{
		{Type: "text", Text: "Hello, WORLD!\n\t123"}, {Type: "key", Action: "down", Key: "Control"},
		{Type: "key", Action: "up", Key: "a"}, {Type: "pointer", Action: "move", X: coordinate(0), Y: coordinate(0)},
		{Type: "pointer", Action: "down", Button: "right", X: coordinate(1279), Y: coordinate(719)},
		{Type: "click", Count: 2, X: coordinate(50), Y: coordinate(100)},
		{Type: "scroll", DeltaX: -2400, DeltaY: 2400, X: coordinate(50), Y: coordinate(100)},
		{Type: "releaseAll"},
	}
	if err := ValidateEvents(good, 1280, 720); err != nil {
		t.Fatal(err)
	}
	invalid := []Event{
		{}, {Type: "eval"}, {Type: "text", Text: "✓"}, {Type: "text", Text: "\x00"}, {Type: "text", Text: string([]byte{0xff})},
		{Type: "text", Text: "safe", Key: "x"}, {Type: "text", Text: "safe", X: coordinate(0)},
		{Type: "key", Action: "press", Key: "a"}, {Type: "key", Action: "down", Key: "ctrl+s"},
		{Type: "pointer", Action: "move", X: coordinate(0)}, {Type: "pointer", Action: "move", X: coordinate(1280), Y: coordinate(0)},
		{Type: "pointer", Action: "move", X: coordinate(-1), Y: coordinate(0)},
		{Type: "pointer", Action: "move", X: coordinate(1), Y: coordinate(720)},
		{Type: "pointer", Action: "drag", X: coordinate(1), Y: coordinate(1)},
		{Type: "pointer", Action: "down", Button: "unknown", X: coordinate(1), Y: coordinate(1)},
		{Type: "click", Count: 3, X: coordinate(1), Y: coordinate(1)},
		{Type: "scroll", DeltaY: 2401, X: coordinate(1), Y: coordinate(1)},
		{Type: "scroll", DeltaX: -2401, X: coordinate(1), Y: coordinate(1)},
		{Type: "scroll", Button: "left", X: coordinate(1), Y: coordinate(1)},
		{Type: "releaseAll", Key: "x"},
	}
	for i, event := range invalid {
		if err := ValidateEvents([]Event{good[0], event}, 1280, 720); !errors.Is(err, ErrInput) {
			t.Errorf("invalid %d accepted: %+v", i, event)
		}
	}
	for _, events := range [][]Event{nil, make([]Event, 101)} {
		if ValidateEvents(events, 1280, 720) == nil {
			t.Fatal("invalid event count accepted")
		}
	}
	for _, size := range [][2]int{{0, 720}, {1280, 0}, {32768, 720}, {1280, 32768}} {
		if ValidateEvents(good, size[0], size[1]) == nil {
			t.Fatal("invalid desktop bounds accepted")
		}
	}
	tooLong := Event{Type: "text", Text: string(make([]byte, 4097))}
	if ValidateEvents([]Event{tooLong}, 1280, 720) == nil {
		t.Fatal("oversized text accepted")
	}
}

// TestNamedKeySymbols verifies named key symbols.
func TestNamedKeySymbols(t *testing.T) {
	for _, name := range []string{"Enter", "Control", "Shift", "Alt", "Meta", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Escape", "Backspace", "Tab", "Delete", "Home", "End", "PageUp", "PageDown", " ", "+", "-", "F1", "F12", "a", "Z", "0", "9", ";", "=", "`", "[", "]", "\\", "/", ",", ".", "'"} {
		if symbol, ok := KeySym(name); !ok || symbol == 0 {
			t.Fatalf("valid key %q missing", name)
		}
	}
	for _, name := range []string{"", "F0", "F13", "Ctrl+a", "✓", ";touch /tmp/x", "!"} {
		if _, ok := KeySym(name); ok {
			t.Fatalf("unsupported key %q accepted", name)
		}
	}
	lower, _ := KeySym("a")
	upper, _ := KeySym("A")
	if lower != upper {
		t.Fatal("letter keys should identify physical key independent of case")
	}
}
