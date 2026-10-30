package apps

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

var displayPattern = regexp.MustCompile(`^:([0-9]{1,3})(?:\.0)?$`)

// X11 opens a bounded guest-local connection per operation. It never uses TCP,
// an inherited arbitrary socket, a host display, xprop, xdotool or a shell.
type X11 struct {
	display   string
	cookieHex string
}

// NewX11 constructs an X11 window manager client.
func NewX11(display, cookieHex string) (*X11, error) {
	if !displayPattern.MatchString(display) {
		return nil, fmt.Errorf("%w: numeric guest-local display required", ErrInvalid)
	}
	return &X11{display: display, cookieHex: cookieHex}, nil
}

type connection struct {
	conn   *xgb.Conn
	raw    net.Conn
	root   xproto.Window
	stop   func() bool
	cancel context.CancelFunc
}

func (x *X11) connect(ctx context.Context) (*connection, error) {
	match := displayPattern.FindStringSubmatch(x.display)
	if match == nil {
		return nil, ErrInvalid
	}
	n, _ := strconv.Atoi(match[1])
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	raw, err := (&net.Dialer{}).DialContext(ctx, "unix", fmt.Sprintf("/tmp/.X11-unix/X%d", n))
	if err != nil {
		cancel()
		return nil, ErrUnavailable
	}
	stop := context.AfterFunc(ctx, func() {
		_ = raw.Close()
	})
	deadline, _ := ctx.Deadline()
	_ = raw.SetDeadline(deadline)
	var conn *xgb.Conn
	if x.cookieHex != "" {
		conn, err = xgb.NewConnNetWithCookieHex(raw, x.cookieHex)
	} else {
		conn, err = xgb.NewConnNet(raw)
	}
	if err != nil {
		stop()
		raw.Close()
		cancel()
		return nil, ErrUnavailable
	}
	setup := xproto.Setup(conn)
	if len(setup.Roots) != 1 {
		stop()
		raw.Close()
		conn.Close()
		cancel()
		return nil, ErrUnavailable
	}
	return &connection{conn: conn, raw: raw, root: setup.Roots[0].Root, stop: stop, cancel: cancel}, nil
}
func (c *connection) close() {
	c.stop()
	_ = c.raw.Close()
	c.conn.Close()
	c.cancel()
}
func (c *connection) atom(name string) (xproto.Atom, error) {
	r, e := xproto.InternAtom(c.conn, false, uint16(len(name)), name).Reply()
	if e != nil || r == nil {
		return 0, ErrUnavailable
	}
	return r.Atom, nil
}
func (c *connection) property(w xproto.Window, atom xproto.Atom, max uint32) (*xproto.GetPropertyReply, error) {
	r, err := xproto.GetProperty(c.conn, false, w, atom, xproto.GetPropertyTypeAny, 0, max).Reply()
	if err != nil || r == nil || r.BytesAfter != 0 {
		return nil, ErrUnavailable
	}
	return r, nil
}
func decodeClients(p *xproto.GetPropertyReply) ([]xproto.Window, error) {
	if p == nil || p.Format != 32 || p.Type != xproto.AtomWindow || len(p.Value)%4 != 0 || len(p.Value) > 4096*4 {
		return nil, ErrUnavailable
	}
	list := make([]xproto.Window, 0, len(p.Value)/4)
	for i := 0; i < len(p.Value); i += 4 {
		list = append(list, xproto.Window(xgb.Get32(p.Value[i:])))
	}
	return list, nil
}
func classNames(value []byte) (string, string, bool) {
	if len(value) > 4096 {
		return "", "", false
	}
	parts := strings.Split(string(value), "\x00")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] != "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
func normalType(p *xproto.GetPropertyReply, normal xproto.Atom) bool {
	if p == nil {
		return false
	}
	if p.Type == 0 && len(p.Value) == 0 {
		return true
	}
	if p.Format != 32 || p.Type != xproto.AtomAtom || len(p.Value) == 0 || len(p.Value)%4 != 0 {
		return false
	}
	for i := 0; i < len(p.Value); i += 4 {
		if xproto.Atom(xgb.Get32(p.Value[i:])) != normal {
			return false
		}
	}
	return true
}

// EWMH specifies one active WINDOW. Xfwm also appends its activation
// timestamp as a second 32-bit value; truncation or extra entries fail closed.
func activeWindow(p *xproto.GetPropertyReply) (uint32, error) {
	if p == nil || p.Format != 32 || p.Type != xproto.AtomWindow || p.BytesAfter != 0 || (len(p.Value) != 4 && len(p.Value) != 8) {
		return 0, ErrUnavailable
	}
	return xgb.Get32(p.Value), nil
}

// List returns the available windows.
func (x *X11) List(ctx context.Context) ([]Window, error) {
	c, err := x.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.close()
	atoms := map[string]xproto.Atom{}
	for _, name := range []string{"_NET_CLIENT_LIST", "_NET_WM_WINDOW_TYPE", "_NET_WM_WINDOW_TYPE_NORMAL", "WM_WINDOW_ROLE"} {
		a, e := c.atom(name)
		if e != nil {
			return nil, e
		}
		atoms[name] = a
	}
	clients, err := c.property(c.root, atoms["_NET_CLIENT_LIST"], 4096)
	if err != nil {
		return nil, err
	}
	ids, err := decodeClients(clients)
	if err != nil {
		return nil, err
	}
	windows := make([]Window, 0, len(ids))
	for _, id := range ids {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		classes, e := c.property(id, xproto.AtomWmClass, 1024)
		if e != nil {
			continue
		}
		if classes.Format != 8 || classes.Type != xproto.AtomString {
			continue
		}
		instance, class, ok := classNames(classes.Value)
		if !ok {
			continue
		}
		typ, e := c.property(id, atoms["_NET_WM_WINDOW_TYPE"], 32)
		if e != nil {
			continue
		}
		role, e := c.property(id, atoms["WM_WINDOW_ROLE"], 128)
		if e != nil {
			continue
		}
		roleName := ""
		if role.Type != 0 {
			if role.Format != 8 || role.Type != xproto.AtomString {
				continue
			}
			roleName = strings.TrimRight(string(role.Value), "\x00")
		}
		geometry, e := xproto.GetGeometry(c.conn, xproto.Drawable(id)).Reply()
		if e != nil || geometry == nil {
			continue
		}
		attributes, e := xproto.GetWindowAttributes(c.conn, id).Reply()
		if e != nil || attributes == nil {
			continue
		}
		windows = append(windows, Window{ID: uint32(id), Instance: instance, Class: class, Role: roleName, Normal: normalType(typ, atoms["_NET_WM_WINDOW_TYPE_NORMAL"]), Mapped: attributes.MapState == xproto.MapStateViewable, Width: geometry.Width, Height: geometry.Height})
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return windows, nil
}

// Activate activates the specified window.
func (x *X11) Activate(ctx context.Context, id uint32) error {
	if id == 0 {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c, err := x.connect(requestCtx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	defer c.close()
	a, err := c.atom("_NET_ACTIVE_WINDOW")
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if requestCtx.Err() != nil {
		return ErrUnavailable
	}
	// EWMH source=2 denotes a pager/launcher. The WM handles deiconification,
	// workspace and focus; no direct map/resize or forced tab navigation occurs.
	event := xproto.ClientMessageEvent{Format: 32, Window: xproto.Window(id), Type: a, Data: xproto.ClientMessageDataUnionData32New([]uint32{2, 0, 0, 0, 0})}
	err = xproto.SendEventChecked(c.conn, false, c.root, xproto.EventMaskSubstructureRedirect|xproto.EventMaskSubstructureNotify, string(event.Bytes())).Check()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// SendEvent confirms only X-server delivery. A successful restore requires
	// the requested client to become both active and viewable. A request that
	// has already reached the WM cannot be retracted if the caller cancels.
	err = waitForActivation(requestCtx, id, 2*time.Second, 25*time.Millisecond, func(_ context.Context) (uint32, bool, error) {
		property, e := c.property(c.root, a, 2)
		if e != nil {
			return 0, false, e
		}
		active, e := activeWindow(property)
		if e != nil {
			return 0, false, e
		}
		if active == id {
			attributes, e := xproto.GetWindowAttributes(c.conn, xproto.Window(id)).Reply()
			if e != nil || attributes == nil {
				return 0, false, ErrUnavailable
			}
			return active, attributes.MapState == xproto.MapStateViewable, nil
		}
		return active, false, nil
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if requestCtx.Err() != nil {
		return ErrUnavailable
	}
	return err
}

func waitForActivation(ctx context.Context, id uint32, timeout, interval time.Duration, observe func(context.Context) (uint32, bool, error)) error {
	observationCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	deadlineError := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if observationCtx.Err() != nil {
			return ErrUnavailable
		}
		return nil
	}
	for {
		if err := deadlineError(); err != nil {
			return err
		}
		active, viewable, err := observe(observationCtx)
		if expired := deadlineError(); expired != nil {
			return expired
		}
		if err != nil {
			return ErrUnavailable
		}
		if active == id && viewable {
			return nil
		}
		select {
		case <-observationCtx.Done():
			return deadlineError()
		case <-ticker.C:
		}
	}
}

// Compile-time assertions keep the public injectable contracts in sync.
var _ Windows = (*X11)(nil)
var _ Starter = ExecStarter{}
