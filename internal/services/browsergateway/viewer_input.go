package browsergateway

import (
	"context"
	"math"

	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
)

func (v *viewerCDP) input(ctx context.Context, input browserapi.ViewerInput) error {
	if input.Modifiers < 0 || input.Modifiers > 15 {
		return errViewerInput
	}
	switch input.Type {
	case "pointerDown", "pointerUp", "pointerMove", "wheel":
		return v.pointer(ctx, input)
	case "keyDown", "keyUp":
		if len(input.Key) > 64 || len(input.Code) > 64 || len(input.Text) > 16 || input.KeyCode < 0 || input.KeyCode > 255 {
			return errViewerInput
		}
		event := input.Type
		if event == "keyDown" && input.Text == "" {
			event = "rawKeyDown"
		}
		_, err := v.send(ctx, "Input.dispatchKeyEvent", map[string]any{
			"type":                  event,
			"key":                   input.Key,
			"code":                  input.Code,
			"windowsVirtualKeyCode": input.KeyCode,
			"modifiers":             input.Modifiers,
			"autoRepeat":            input.Repeat,
			"text":                  input.Text,
		})
		return err
	case "text":
		if input.Text == "" {
			return errViewerInput
		}
		_, err := v.send(ctx, "Input.insertText", map[string]string{
			"text": input.Text,
		})
		return err
	default:
		return errViewerInput
	}
}

func (v *viewerCDP) pointer(ctx context.Context, input browserapi.ViewerInput) error {
	if input.X < 0 || input.X > 1 || input.Y < 0 || input.Y > 1 ||
		input.Buttons < 0 || input.Buttons > 31 || input.ClickCount < 0 || input.ClickCount > 3 ||
		math.Abs(input.DeltaX) > 10000 || math.Abs(input.DeltaY) > 10000 {
		return errViewerInput
	}
	if v.viewport.Width <= 0 || v.viewport.Height <= 0 {
		return nil
	}
	button := input.Button
	switch button {
	case "":
		button = "none"
	case "left", "right", "middle", "back", "forward":
	default:
		return errViewerInput
	}
	event := "mouseMoved"
	switch input.Type {
	case "pointerDown":
		event = "mousePressed"
	case "pointerUp":
		event = "mouseReleased"
	case "wheel":
		event = "mouseWheel"
	}
	_, err := v.send(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type":       event,
		"x":          min(input.X*v.viewport.Width, v.viewport.Width-1),
		"y":          min(input.Y*v.viewport.Height, v.viewport.Height-1),
		"button":     button,
		"buttons":    input.Buttons,
		"clickCount": input.ClickCount,
		"modifiers":  input.Modifiers,
		"deltaX":     input.DeltaX,
		"deltaY":     input.DeltaY,
	})
	return err
}
