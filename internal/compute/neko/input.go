package neko

import (
	"context"
	"errors"

	"github.com/kevinmartin/sidekicks/internal/compute/control"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop"
)

var (
	// ErrNativeInputUnsupported fails closed until upstream ownership checks and
	// native dispatch can be made atomic with reset and generation changes.
	ErrNativeInputUnsupported = errors.New("neko: native input is unsupported for the pinned upstream server")
	// ErrUpstreamInputUnsupported prevents asynchronous controller WebSocket input
	// from bypassing the synchronized Go X11 driver and its reset barrier.
	ErrUpstreamInputUnsupported = errors.New("neko: upstream input dispatch is unsupported; use the synchronized go x11 driver")
)

// Event remains an alias for source compatibility; input is implemented by the
// shared desktop/X11 driver, not by an upstream Neko control transport.
type Event = desktop.Event

// Execute always fails closed. Neko v3's asynchronous WebSocket handler does not
// provide a dispatch/reset generation barrier. Media signaling remains supported.
// Deprecated: call the synchronized desktop driver inside Manager.DispatchGated.
func (c *Client) Execute(_ context.Context, _ *control.Manager, _ control.Credentials, _ []Event) error {
	return ErrUpstreamInputUnsupported
}
