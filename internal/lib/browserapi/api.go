// Package browserapi defines the wire format for the guest browser API.
package browserapi

import (
	"encoding/json"
)

// Tabs is the guest readiness response and current list of page targets.
type Tabs struct {
	Primary string `json:"primary"`
	Tabs    []Tab  `json:"tabs"`
}

// Tab identifies a page target in the running Chromium instance.
type Tab struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Command describes a typed browser operation.
type Command struct {
	Tab       string `json:"tab,omitempty"`
	Operation string `json:"operation"`
	URL       string `json:"url,omitempty"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
}

// CDPCommand describes a single Chrome DevTools Protocol command.
type CDPCommand struct {
	Tab     string          `json:"tab,omitempty"`
	Browser bool            `json:"browser,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// CommandResult contains the tab and result of a browser operation.
type CommandResult struct {
	Tab    string `json:"tab"`
	URL    string `json:"url,omitempty"`
	Status *int   `json:"status,omitempty"`
}
