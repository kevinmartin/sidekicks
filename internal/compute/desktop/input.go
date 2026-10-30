package desktop

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ErrInput indicates an invalid input event.
var ErrInput = errors.New("invalid or unsupported desktop input")

// Event is a normalized input request. Coordinates are desktop pixels, not CSS
// pixels. Text currently supports printable ASCII, LF and Tab; reject unsupported
// text before dispatch instead of silently changing keyboard layout or dropping it.
type Event struct {
	Type   string `json:"type"`
	Action string `json:"action,omitempty"`
	Key    string `json:"key,omitempty"`
	Text   string `json:"text,omitempty"`
	X      *int   `json:"x,omitempty"`
	Y      *int   `json:"y,omitempty"`
	Button string `json:"button,omitempty"`
	Count  int    `json:"count,omitempty"`
	DeltaX int    `json:"deltaX,omitempty"`
	DeltaY int    `json:"deltaY,omitempty"`
}

// ValidateEvents validates the entire bounded batch without emitting input.
func ValidateEvents(events []Event, width, height int) error {
	if len(events) < 1 || len(events) > 100 || width < 1 || height < 1 || width > 32767 || height > 32767 {
		return ErrInput
	}
	for _, event := range events {
		switch event.Type {
		case "releaseAll":
			if event.Action != "" || event.Key != "" || event.Text != "" || event.X != nil || event.Y != nil || event.Button != "" || event.Count != 0 || event.DeltaX != 0 || event.DeltaY != 0 {
				return ErrInput
			}
		case "text":
			if !utf8.ValidString(event.Text) || len(event.Text) > 4096 || event.Action != "" || event.Key != "" || event.X != nil || event.Y != nil || event.Button != "" || event.Count != 0 || event.DeltaX != 0 || event.DeltaY != 0 {
				return ErrInput
			}
			for _, r := range event.Text {
				if (r < 32 || r > 126) && r != '\n' && r != '\t' {
					return fmt.Errorf("%w: text supports printable ascii, lf and tab", ErrInput)
				}
			}
		case "key":
			if _, ok := KeySym(event.Key); !ok || (event.Action != "down" && event.Action != "up") || event.Text != "" || event.X != nil || event.Y != nil || event.Button != "" || event.Count != 0 || event.DeltaX != 0 || event.DeltaY != 0 {
				return ErrInput
			}
		case "pointer", "click", "scroll":
			if event.X == nil || event.Y == nil || *event.X < 0 || *event.Y < 0 || *event.X >= width || *event.Y >= height || event.Key != "" || event.Text != "" {
				return ErrInput
			}
			if event.Button != "" && event.Button != "left" && event.Button != "middle" && event.Button != "right" {
				return ErrInput
			}
			switch event.Type {
			case "pointer":
				if (event.Action != "move" && event.Action != "down" && event.Action != "up") || event.Count != 0 || event.DeltaX != 0 || event.DeltaY != 0 {
					return ErrInput
				}
			case "click":
				if event.Action != "" || (event.Count != 0 && event.Count != 1 && event.Count != 2) || event.DeltaX != 0 || event.DeltaY != 0 {
					return ErrInput
				}
			case "scroll":
				if event.Action != "" || event.Count != 0 || event.DeltaX < -2400 || event.DeltaX > 2400 || event.DeltaY < -2400 || event.DeltaY > 2400 || event.Button != "" {
					return ErrInput
				}
			}
		default:
			return ErrInput
		}
	}
	return nil
}

// KeySym maps a deliberately bounded named-key contract to an X11 keysym.
// Single letters name physical letter keys; use text for exact case/punctuation.
func KeySym(name string) (uint32, bool) {
	named := map[string]uint32{
		"Enter": 0xff0d, "Control": 0xffe3, "Shift": 0xffe1, "Alt": 0xffe9, "Meta": 0xffeb,
		"ArrowUp": 0xff52, "ArrowDown": 0xff54, "ArrowLeft": 0xff51, "ArrowRight": 0xff53,
		"Escape": 0xff1b, "Backspace": 0xff08, "Tab": 0xff09, "Delete": 0xffff, "Home": 0xff50, "End": 0xff57, "PageUp": 0xff55, "PageDown": 0xff56, " ": 0x20, "+": 0x2b, "-": 0x2d,
	}
	if value, ok := named[name]; ok {
		return value, true
	}
	if len(name) == 1 {
		c := name[0]
		if c >= 'A' && c <= 'Z' {
			return uint32(c + ('a' - 'A')), true
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || strings.ContainsRune(";=`[]\\/,.", rune(c)) || c == '\'' {
			return uint32(c), true
		}
	}
	for i := 1; i <= 12; i++ {
		if name == fmt.Sprintf("F%d", i) {
			return uint32(0xffbd + i), true
		}
	}
	return 0, false
}
