package x11

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/kevinmartin/sidekicks/internal/compute/desktop"
)

// ErrUnavailable indicates that the resource is unavailable.
var ErrUnavailable = errors.New("guest x11 input unavailable")

type connection struct {
	backend Backend
	dead    atomic.Bool
	once    sync.Once
}

func (c *connection) close() {
	c.dead.Store(true)
	c.once.Do(func() {
		_ = c.backend.Close()
	})
}

// Driver implements control.Driver.Reset and bounded normalized XTEST input.
// Call Input only from the control manager's gated dispatch callback. Reset
// queries actual server state; it does not trust transport bookkeeping.
type Driver struct {
	lock          chan struct{}
	current       *connection
	active        atomic.Pointer[connection]
	closing       atomic.Bool
	uncertain     atomic.Bool
	reconnect     func(context.Context) (Backend, error)
	width, height int
	autoShift     map[uint32]byte
	closed        bool
}

// New installs an injectable backend. Open can reconnect only after a known
// no-input preflight failure. Uncertain canceled/failed I/O permanently poisons
// this driver; a fresh guest lifecycle is required before acknowledging control.
func New(backend Backend) *Driver {
	d := &Driver{lock: make(chan struct{}, 1), autoShift: map[uint32]byte{}}
	d.lock <- struct{}{}
	if backend != nil {
		d.current = &connection{backend: backend}
		d.active.Store(d.current)
		d.width, d.height = backend.Size()
	}
	return d
}

// Size returns the current display dimensions.
func (d *Driver) Size() (int, int) {
	return d.width, d.height
}

// Ready is a non-I/O health snapshot. Uncertain input consumption never becomes
// ready again through a different X11 connection; recreate the guest lifecycle.
func (d *Driver) Ready() bool {
	holder := d.active.Load()
	return !d.closing.Load() && !d.uncertain.Load() && holder != nil && !holder.dead.Load()
}

func (d *Driver) operation(ctx context.Context, reconnect bool, fn func(Backend) error) error {
	return d.operate(ctx, reconnect, false, fn)
}
func (d *Driver) operate(ctx context.Context, reconnect, allowClosing bool, fn func(Backend) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-d.lock:
	}
	defer func() {
		d.lock <- struct{}{}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.closed || d.closing.Load() && !allowClosing || d.uncertain.Load() {
		return ErrUnavailable
	}
	holder := d.current
	if holder == nil || holder.dead.Load() {
		if !reconnect || d.reconnect == nil {
			return ErrUnavailable
		}
		backend, err := d.reconnect(ctx)
		if err != nil {
			return ErrUnavailable
		}
		holder = &connection{backend: backend}
		d.current = holder
		d.active.Store(holder)
		d.autoShift = map[uint32]byte{}
	}
	var completed atomic.Bool
	stop := context.AfterFunc(ctx, func() {
		if !completed.Load() {
			d.uncertain.Store(true)
		}
		holder.close()
	})
	err := fn(holder.backend)
	completed.Store(true)
	if err != nil && !errors.Is(err, desktop.ErrInput) {
		d.uncertain.Store(true)
	}
	stopped := stop()
	if ctx.Err() != nil {
		holder.close()
		return ctx.Err()
	}
	if !stopped {
		holder.close()
		return ErrUnavailable
	}
	if err != nil {
		holder.close()
		return err
	}
	return nil
}

// Reset releases every actually pressed keycode and pointer button 1..5, then
// verifies the server is clear. This includes RFB/API/native mixed holds.
func (d *Driver) Reset(ctx context.Context) error {
	return d.reset(ctx, false)
}
func (d *Driver) reset(ctx context.Context, allowClosing bool) error {
	return d.operate(ctx, true, allowClosing, func(backend Backend) error {
		if err := reset(backend); err != nil {
			return err
		}
		d.autoShift = map[uint32]byte{}
		return nil
	})
}
func reset(backend Backend) error {
	state, err := backend.State()
	if err != nil {
		return err
	}
	for code := 8; code < 256; code++ {
		if state.Keys[code/8]&(1<<uint(code%8)) != 0 {
			if err := backend.Fake(xproto.KeyRelease, byte(code), 0, 0); err != nil {
				return err
			}
		}
	}
	// Core QueryPointer exposes only button masks 1..5. RFB permits 1..8,
	// and wheel axes use 6/7, so release the full supported range even when
	// an injected held button is absent from the core mask.
	for button := 1; button <= 8; button++ {
		if err := backend.Fake(xproto.ButtonRelease, byte(button), 0, 0); err != nil {
			return err
		}
	}
	if err := backend.Sync(); err != nil {
		return err
	}
	state, err = backend.State()
	if err != nil {
		return err
	}
	for _, bits := range state.Keys {
		if bits != 0 {
			return errors.New("x11 keys remain pressed after reset")
		}
	}
	if state.Buttons&0x1f00 != 0 {
		return errors.New("x11 buttons remain pressed after reset")
	}
	return nil
}

// State returns the current input state.
func (d *Driver) State(ctx context.Context) (state State, err error) {
	err = d.operation(ctx, false, func(backend Backend) error {
		var err error
		state, err = backend.State()
		return err
	})
	return state, err
}

// Input validates an entire event before emitting it. Text mapping is also
// validated up front, and each character checks cancellation. The outer control
// dispatch must revalidate the lease between events or cancel this callback.
func (d *Driver) Input(ctx context.Context, event desktop.Event) error {
	if err := desktop.ValidateEvents([]desktop.Event{event}, d.width, d.height); err != nil {
		return err
	}
	return d.operation(ctx, false, func(backend Backend) error {
		check := func() error {
			return ctx.Err()
		}
		fake := func(kind, detail byte, x, y int16) error {
			if err := check(); err != nil {
				return err
			}
			return backend.Fake(kind, detail, x, y)
		}
		switch event.Type {
		case "releaseAll":
			if err := reset(backend); err != nil {
				return err
			}
			d.autoShift = map[uint32]byte{}
		case "key":
			symbol, _ := desktop.KeySym(event.Key)
			stroke, ok := backend.Mapping()[symbol]
			if !ok {
				return fmt.Errorf("%w: key absent from guest layout", desktop.ErrInput)
			}
			if event.Action == "down" {
				if stroke.Shift {
					shift, ok := backend.Mapping()[0xffe1]
					if !ok {
						return desktop.ErrInput
					}
					state, err := backend.State()
					if err != nil {
						return err
					}
					if state.Keys[int(shift.Code)/8]&(1<<uint(shift.Code%8)) == 0 {
						if err := fake(xproto.KeyPress, shift.Code, 0, 0); err != nil {
							return err
						}
						d.autoShift[symbol] = shift.Code
					} else if len(d.autoShift) > 0 {
						d.autoShift[symbol] = shift.Code
					}
				}
				if err := fake(xproto.KeyPress, stroke.Code, 0, 0); err != nil {
					return err
				}
			} else {
				if err := fake(xproto.KeyRelease, stroke.Code, 0, 0); err != nil {
					return err
				}
				if shift, ok := d.autoShift[symbol]; ok {
					delete(d.autoShift, symbol)
					if len(d.autoShift) == 0 {
						if err := fake(xproto.KeyRelease, shift, 0, 0); err != nil {
							return err
						}
					}
				}
			}
			// An explicit Shift command owns its modifier independently of a
			// symbolic key. A later symbolic keyup must not release that Shift.
			if symbol == 0xffe1 {
				for shifted, code := range d.autoShift {
					if code == stroke.Code {
						delete(d.autoShift, shifted)
					}
				}
			}
		case "text":
			state, err := backend.State()
			if err != nil {
				return err
			}
			if state.Buttons&xproto.ModMaskLock != 0 {
				return fmt.Errorf("%w: caps lock must be off for exact text", desktop.ErrInput)
			}
			strokes := make([]Stroke, 0, len(event.Text))
			shift, hasShift := backend.Mapping()[0xffe1]
			for _, char := range event.Text {
				symbol := uint32(char)
				if char == '\n' {
					symbol = 0xff0d
				}
				if char == '\t' {
					symbol = 0xff09
				}
				stroke, ok := backend.Mapping()[symbol]
				if !ok || stroke.Shift && !hasShift {
					return fmt.Errorf("%w: text absent from guest layout", desktop.ErrInput)
				}
				strokes = append(strokes, stroke)
			}
			if err := reset(backend); err != nil {
				return err
			}
			d.autoShift = map[uint32]byte{}
			for _, stroke := range strokes {
				if stroke.Shift {
					if err := fake(xproto.KeyPress, shift.Code, 0, 0); err != nil {
						return err
					}
				}
				if err := fake(xproto.KeyPress, stroke.Code, 0, 0); err != nil {
					return err
				}
				if err := fake(xproto.KeyRelease, stroke.Code, 0, 0); err != nil {
					return err
				}
				if stroke.Shift {
					if err := fake(xproto.KeyRelease, shift.Code, 0, 0); err != nil {
						return err
					}
				}
			}
		case "pointer", "click", "scroll":
			if err := fake(xproto.MotionNotify, 0, int16(*event.X), int16(*event.Y)); err != nil {
				return err
			}
			button := byte(1)
			if event.Button == "middle" {
				button = 2
			}
			if event.Button == "right" {
				button = 3
			}
			switch event.Type {
			case "pointer":
				if event.Action == "down" {
					if err := fake(xproto.ButtonPress, button, 0, 0); err != nil {
						return err
					}
				}
				if event.Action == "up" {
					if err := fake(xproto.ButtonRelease, button, 0, 0); err != nil {
						return err
					}
				}
			case "click":
				count := max(1, event.Count)
				for i := 0; i < count; i++ {
					if i > 0 {
						if err := pause(ctx, 80*time.Millisecond); err != nil {
							return err
						}
					}
					if err := fake(xproto.ButtonPress, button, 0, 0); err != nil {
						return err
					}
					if err := fake(xproto.ButtonRelease, button, 0, 0); err != nil {
						return err
					}
				}
			case "scroll":
				for _, axis := range []struct {
					delta              int
					positive, negative byte
				}{{event.DeltaY, 5, 4}, {event.DeltaX, 7, 6}} {
					if axis.delta == 0 {
						continue
					}
					detail := axis.positive
					if axis.delta < 0 {
						detail = axis.negative
					}
					count := min(20, max(1, int(math.Round(math.Abs(float64(axis.delta))/120))))
					for range count {
						if err := fake(xproto.ButtonPress, detail, 0, 0); err != nil {
							return err
						}
						if err := fake(xproto.ButtonRelease, detail, 0, 0); err != nil {
							return err
						}
					}
				}
			}
		}
		return backend.Sync()
	})
}
func pause(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Close first makes a bounded release attempt. Close the control manager before
// this driver so no further authorized dispatch can race shutdown.
func (d *Driver) Close() error {
	if !d.closing.CompareAndSwap(false, true) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resetErr := d.reset(ctx, true)
	if resetErr != nil {
		d.uncertain.Store(true)
		if holder := d.active.Load(); holder != nil {
			holder.close()
		}
	}
	select {
	case <-d.lock:
	case <-ctx.Done():
		return errors.Join(resetErr, ctx.Err())
	}
	defer func() {
		d.lock <- struct{}{}
	}()
	if d.closed {
		return nil
	}
	d.closed = true
	if d.current != nil {
		d.current.close()
	}
	return resetErr
}
