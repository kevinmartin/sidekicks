package browserapi

// ViewerPath is the authenticated guest WebSocket for viewing a tab.
const ViewerPath = "/v1/viewer"

// ViewerProtocol identifies the same-origin WebSocket on the CLI machine.
const ViewerProtocol = "sidekicks-viewer"

// ViewerInputLimit and ViewerFrameLimit bound the viewer's WebSocket messages.
const (
	ViewerInputLimit = 16 << 10
	ViewerFrameLimit = 8 << 20
)

// ViewerFrame holds a base64 JPEG of the selected tab's viewport.
type ViewerFrame struct {
	JPEG string `json:"jpeg"`
}

// ViewerInput is a pointer, wheel, keyboard, or pasted-text event.
// X and Y are fractions of the displayed image's width and height.
type ViewerInput struct {
	Type       string  `json:"type"`
	X          float64 `json:"x,omitempty"`
	Y          float64 `json:"y,omitempty"`
	Button     string  `json:"button,omitempty"`
	Buttons    int     `json:"buttons,omitempty"`
	ClickCount int     `json:"clickCount,omitempty"`
	DeltaX     float64 `json:"deltaX,omitempty"`
	DeltaY     float64 `json:"deltaY,omitempty"`
	Key        string  `json:"key,omitempty"`
	Code       string  `json:"code,omitempty"`
	KeyCode    int     `json:"keyCode,omitempty"`
	Modifiers  int     `json:"modifiers,omitempty"`
	Repeat     bool    `json:"repeat,omitempty"`
	Text       string  `json:"text,omitempty"`
}
