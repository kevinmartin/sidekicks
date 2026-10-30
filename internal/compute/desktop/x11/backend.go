// Package x11 implements guest-local X11 input and capture through pure-Go XGB.
// It must run inside the workload, never against a user's host display.
package x11

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// MaxPixels limits the number of pixels in a captured image.
const MaxPixels = 4 << 20

// Stroke describes an X11 key stroke.
type Stroke struct {
	Code  byte
	Shift bool
}

// State reports the current X11 key and pointer state.
type State struct {
	Keys    [32]byte
	Buttons uint16
	X, Y    int
}

// RawImage contains image data received from X11.
type RawImage struct {
	Width, Height                               int
	Depth, BitsPerPixel, ScanlinePad, ByteOrder byte
	RedMask, GreenMask, BlueMask                uint32
	Data                                        []byte
}

// Backend is injectable for driver tests. Operations are serialized by Driver;
// Close must unblock any in-flight operation. Only code in this package creates
// the real backend, restricted to a numeric local Unix X11 display.
type Backend interface {
	Size() (int, int)
	Mapping() map[uint32]Stroke
	State() (State, error)
	Fake(kind, detail byte, x, y int16) error
	Image() (RawImage, error)
	Sync() error
	Close() error
}

type backend struct {
	conn    *xgb.Conn
	raw     net.Conn
	screen  xproto.ScreenInfo
	setup   *xproto.SetupInfo
	strokes map[uint32]Stroke
}

var displayPattern = regexp.MustCompile(`^:([0-9]{1,3})(?:\.0)?$`)

// Open connects only /tmp/.X11-unix/XN, never TCP or a host-selected socket.
// CookieHex may provide guest MIT-MAGIC-COOKIE-1 data; do not log it. Empty cookie
// retains XGB's Xauthority lookup / local server authentication behavior.
func Open(ctx context.Context, display, cookieHex string) (*Driver, error) {
	b, err := openBackend(ctx, display, cookieHex)
	if err != nil {
		return nil, err
	}
	d := New(b)
	d.reconnect = func(ctx context.Context) (Backend, error) {
		return openBackend(ctx, display, cookieHex)
	}
	return d, nil
}

func openBackend(ctx context.Context, display, cookieHex string) (Backend, error) {
	match := displayPattern.FindStringSubmatch(display)
	if match == nil {
		return nil, errors.New("x11 display must be a numeric guest-local display")
	}
	number, _ := strconv.Atoi(match[1])
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(ctx, "unix", fmt.Sprintf("/tmp/.X11-unix/X%d", number))
	if err != nil {
		return nil, errors.New("guest x11 socket unavailable")
	}
	stop := context.AfterFunc(ctx, func() {
		_ = raw.Close()
	})
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = raw.SetDeadline(deadline)
	var conn *xgb.Conn
	if cookieHex != "" {
		conn, err = xgb.NewConnNetWithCookieHex(raw, cookieHex)
	} else {
		conn, err = xgb.NewConnNet(raw)
	}
	if err != nil {
		_ = raw.Close()
		return nil, errors.New("guest x11 authentication/setup failed")
	}
	fail := func(message string) (Backend, error) {
		_ = raw.Close()
		conn.Close()
		return nil, errors.New(message)
	}
	setup := xproto.Setup(conn)
	if len(setup.Roots) != 1 {
		return fail("exactly one guest x11 screen is required")
	}
	screen := setup.Roots[0]
	if int(screen.WidthInPixels)*int(screen.HeightInPixels) > MaxPixels || screen.WidthInPixels == 0 || screen.HeightInPixels == 0 {
		return fail("guest x11 screen exceeds capture bounds")
	}
	if err = xtest.Init(conn); err != nil {
		return fail("guest xtest extension unavailable")
	}
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	if count < 1 || count > 255 {
		return fail("unsupported guest x11 keycode range")
	}
	mapping, err := xproto.GetKeyboardMapping(conn, setup.MinKeycode, byte(count)).Reply()
	if err != nil || mapping == nil || mapping.KeysymsPerKeycode < 1 {
		return fail("guest x11 keyboard map unavailable")
	}
	strokes := map[uint32]Stroke{}
	columns := int(mapping.KeysymsPerKeycode)
	if len(mapping.Keysyms) != count*columns {
		return fail("malformed guest x11 keyboard map")
	}
	for code := 0; code < count; code++ {
		for column := 0; column < min(columns, 2); column++ {
			symbol := uint32(mapping.Keysyms[code*columns+column])
			if symbol == 0 {
				continue
			}
			stroke := Stroke{Code: byte(int(setup.MinKeycode) + code), Shift: column == 1}
			previous, found := strokes[symbol]
			if !found || previous.Shift && !stroke.Shift {
				strokes[symbol] = stroke
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return fail("guest x11 initialization canceled")
	}
	_ = raw.SetDeadline(time.Time{})
	return &backend{conn: conn, raw: raw, screen: screen, setup: setup, strokes: strokes}, nil
}

// Size returns the current display dimensions.
func (b *backend) Size() (int, int) {
	return int(b.screen.WidthInPixels), int(b.screen.HeightInPixels)
}

// Mapping returns the X11 key mapping.
func (b *backend) Mapping() map[uint32]Stroke {
	return b.strokes
}

// State returns the current input state.
func (b *backend) State() (State, error) {
	var state State
	keys, err := xproto.QueryKeymap(b.conn).Reply()
	if err != nil || keys == nil {
		return state, errors.New("x11 key query failed")
	}
	if len(keys.Keys) != 32 {
		return state, errors.New("malformed x11 key map")
	}
	copy(state.Keys[:], keys.Keys)
	pointer, err := xproto.QueryPointer(b.conn, b.screen.Root).Reply()
	if err != nil || pointer == nil || !pointer.SameScreen {
		return state, errors.New("x11 pointer query failed")
	}
	state.Buttons = pointer.Mask
	state.X = int(pointer.RootX)
	state.Y = int(pointer.RootY)
	return state, nil
}

// Fake sends an X11 input event.
func (b *backend) Fake(kind, detail byte, x, y int16) error {
	return xtest.FakeInputChecked(b.conn, kind, detail, 0, b.screen.Root, x, y, 0).Check()
}

// Sync synchronizes pending X11 requests.
func (b *backend) Sync() error {
	reply, err := xproto.GetInputFocus(b.conn).Reply()
	if err != nil || reply == nil {
		return errors.New("x11 synchronization failed")
	}
	return nil
}

// Close releases the receiver's resources.
func (b *backend) Close() error {
	err := b.raw.Close()
	b.conn.Close()
	return err
}

// Image returns a captured X11 image.
func (b *backend) Image() (RawImage, error) {
	geometry, err := xproto.GetGeometry(b.conn, xproto.Drawable(b.screen.Root)).Reply()
	if err != nil || geometry == nil {
		return RawImage{}, errors.New("x11 root geometry unavailable")
	}
	width, height := int(geometry.Width), int(geometry.Height)
	if width < 1 || height < 1 || width*height > MaxPixels {
		return RawImage{}, errors.New("x11 capture exceeds pixel bound")
	}
	reply, err := xproto.GetImage(b.conn, xproto.ImageFormatZPixmap, xproto.Drawable(b.screen.Root), 0, 0, uint16(width), uint16(height), ^uint32(0)).Reply()
	if err != nil || reply == nil {
		return RawImage{}, errors.New("x11 image unavailable")
	}
	image := RawImage{Width: width, Height: height, Depth: reply.Depth, ByteOrder: b.setup.ImageByteOrder, Data: reply.Data}
	for _, format := range b.setup.PixmapFormats {
		if format.Depth == reply.Depth {
			image.BitsPerPixel = format.BitsPerPixel
			image.ScanlinePad = format.ScanlinePad
			break
		}
	}
	for _, depth := range b.screen.AllowedDepths {
		for _, visual := range depth.Visuals {
			if visual.VisualId == reply.Visual && visual.Class == xproto.VisualClassTrueColor {
				image.RedMask = visual.RedMask
				image.GreenMask = visual.GreenMask
				image.BlueMask = visual.BlueMask
				return image, nil
			}
		}
	}
	return RawImage{}, errors.New("only truecolor x11 captures are supported")
}
