// Package control serializes desktop dispatch and ownership changes. Revocation
// cancels in-flight work immediately; acknowledgement follows the driver's
// release of actual input state. It cannot undo events already delivered to apps.
package control

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"
)

var (
	// ErrInvalid indicates an invalid control request.
	ErrInvalid = errors.New("invalid control request")
	// ErrConflict indicates conflicting desktop ownership.
	ErrConflict = errors.New("desktop ownership conflict")
	// ErrStale indicates a revoked or expired control lease.
	ErrStale = errors.New("stale, expired or revoked control lease")
	// ErrUnavailable indicates that desktop input is unavailable.
	ErrUnavailable = errors.New("desktop input unavailable")
	// ErrClosed indicates that the manager is closed.
	ErrClosed = errors.New("control manager closed")
)

// Owner identifies the type of client holding control.
type Owner string

const (
	// Human identifies a human viewer.
	Human Owner = "human"
	// Agent identifies an automated client.
	Agent Owner = "agent"
)

// Mode identifies the transport used for control input.
type Mode string

const (
	// Gated routes input through the synchronized control gate.
	Gated Mode = "gated"
	// Native delegates control to a native viewer.
	Native Mode = "native"
)

// Driver resets held keys and buttons and revokes any native controller.
// A successful reset is a safety acknowledgement, not an app-queue flush.
type Driver interface{ Reset(context.Context) error }

// NativeDriver transfers native control to a named viewer.
type NativeDriver interface {
	Give(context.Context, string) error
}

// Credentials identifies a control session and its lease token.
type Credentials struct {
	Session string `json:"session"`
	Token   string `json:"token"`
}

// Request describes a requested control lease.
type Request struct {
	Owner    Owner         `json:"owner"`
	Session  string        `json:"session"`
	Token    string        `json:"token,omitempty"`
	Mode     Mode          `json:"mode,omitempty"`
	ViewerID string        `json:"viewerId,omitempty"`
	Duration time.Duration `json:"-"`
}

// View describes control ownership without disclosing its token.
type View struct {
	Owner     Owner     `json:"owner,omitempty"`
	Session   string    `json:"session,omitempty"`
	Mode      Mode      `json:"mode,omitempty"`
	ViewerID  string    `json:"viewerId,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	Ready     bool      `json:"ready"`
}

// Lease contains a control view and its private lease credentials.
type Lease struct {
	View
	Token      string `json:"token"`
	Generation uint64 `json:"generation"`
}

// Option configures a control manager.
type Option func(*Manager)

// WithClock substitutes only the expiry clock. Real expiry timers are retained;
// tests advancing a fake clock may call Expire for deterministic acknowledgement.
func WithClock(now func() time.Time) Option {
	return func(m *Manager) {
		if now != nil {
			m.now = now
		}
	}
}

// WithResetTimeout sets the timeout for input reset operations.
func WithResetTimeout(d time.Duration) Option {
	return func(m *Manager) {
		if d > 0 {
			m.resetTimeout = d
		}
	}
}

type intent struct {
	ticket uint64
	owner  Owner
}

// Manager serializes control leases and input dispatch.
type Manager struct {
	mu           sync.Mutex
	serial       chan struct{}
	driver       Driver
	now          func() time.Time
	resetTimeout time.Duration
	current      *Lease
	pending      *intent
	generation   uint64
	blocked      bool
	closed       bool
	timer        *time.Timer
	inflight     context.CancelFunc
}

var sessionPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// New constructs a control manager for the given driver.
func New(driver Driver, options ...Option) *Manager {
	m := &Manager{driver: driver, serial: make(chan struct{}, 1), now: time.Now, resetTimeout: 5 * time.Second}
	m.serial <- struct{}{}
	for _, option := range options {
		if option != nil {
			option(m)
		}
	}
	if driver == nil {
		m.blocked = true
	}
	return m
}
func (m *Manager) lock(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.serial:
		return nil
	}
}
func (m *Manager) unlock() {
	m.serial <- struct{}{}
}
func (m *Manager) revokeLocked() {
	m.current = nil
	m.pending = nil
	m.generation++
	m.blocked = true
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	if m.inflight != nil {
		m.inflight()
	}
}
func (m *Manager) expiredLocked() bool {
	if m.current != nil && !m.now().Before(m.current.ExpiresAt) {
		m.revokeLocked()
		return true
	}
	return false
}
func (m *Manager) viewLocked() View {
	if m.current != nil {
		return m.current.View
	}
	return View{Ready: !m.blocked && !m.closed && m.driver != nil}
}

// View returns the current public control view.
func (m *Manager) View() View {
	m.mu.Lock()
	expired := m.expiredLocked()
	ticket := m.generation
	v := m.viewLocked()
	m.mu.Unlock()
	if expired {
		m.cleanupAsync(ticket)
	}
	return v
}
func (m *Manager) validateLocked(c Credentials, generation uint64) (Lease, error) {
	if m.closed {
		return Lease{}, ErrClosed
	}
	if m.blocked || m.current == nil || m.current.Generation != generation || m.current.Session != c.Session || subtle.ConstantTimeCompare([]byte(m.current.Token), []byte(c.Token)) != 1 {
		return Lease{}, ErrStale
	}
	if !m.now().Before(m.current.ExpiresAt) {
		return Lease{}, ErrStale
	}
	return *m.current, nil
}

// Validate validates credentials against the current lease.
func (m *Manager) Validate(c Credentials) (Lease, error) {
	m.mu.Lock()
	expired := m.expiredLocked()
	ticket := m.generation
	l, err := m.validateLocked(c, m.generation)
	m.mu.Unlock()
	if expired {
		m.cleanupAsync(ticket)
	}
	return l, err
}
func valid(r Request) error {
	if (r.Owner != Human && r.Owner != Agent) || !sessionPattern.MatchString(r.Session) {
		return ErrInvalid
	}
	if r.Mode != Gated && r.Mode != Native {
		return ErrInvalid
	}
	if r.Duration < 100*time.Millisecond || r.Duration > 60*time.Second {
		return ErrInvalid
	}
	if r.Mode == Native && (r.Owner != Human || !sessionPattern.MatchString(r.ViewerID)) {
		return ErrInvalid
	}
	if r.Mode == Gated && r.ViewerID != "" {
		return ErrInvalid
	}
	return nil
}

// Acquire acquires or renews a control lease.
func (m *Manager) Acquire(ctx context.Context, r Request) (Lease, error) {
	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}
	if r.Mode == "" {
		r.Mode = Gated
	}
	if r.Duration == 0 {
		r.Duration = 30 * time.Second
	}
	if err := valid(r); err != nil {
		return Lease{}, err
	}
	if m.driver == nil {
		return Lease{}, ErrUnavailable
	}
	native, canNative := m.driver.(NativeDriver)
	if r.Mode == Native && !canNative {
		return Lease{}, ErrUnavailable
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Lease{}, ErrClosed
	}
	m.expiredLocked()
	if m.current != nil {
		c := m.current
		if c.Owner == r.Owner && c.Session == r.Session && c.Mode == r.Mode && c.ViewerID == r.ViewerID {
			if subtle.ConstantTimeCompare([]byte(c.Token), []byte(r.Token)) != 1 {
				m.mu.Unlock()
				return Lease{}, ErrConflict
			}
			c.ExpiresAt = m.now().Add(r.Duration)
			m.armTimerLocked(r.Duration, c.Generation)
			result := *c
			m.mu.Unlock()
			return result, nil
		}
		if !(r.Owner == Human && c.Owner == Agent) {
			m.mu.Unlock()
			return Lease{}, ErrConflict
		}
	}
	if m.pending != nil && !(r.Owner == Human && m.pending.owner == Agent) {
		m.mu.Unlock()
		return Lease{}, ErrConflict
	}
	m.revokeLocked()
	ticket := m.generation
	m.pending = &intent{ticket, r.Owner}
	m.mu.Unlock()
	if err := m.lock(ctx); err != nil {
		m.cancelIntent(ticket)
		return Lease{}, err
	}
	defer m.unlock()
	if !m.intentCurrent(ticket) {
		return Lease{}, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		m.failIntent(ticket)
		m.cleanupAsync(ticket)
		return Lease{}, err
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		m.failIntent(ticket)
		m.cleanupAsync(ticket)
		return Lease{}, fmt.Errorf("%w: entropy unavailable", ErrUnavailable)
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	if err := m.driver.Reset(ctx); err != nil {
		m.failIntent(ticket)
		m.cleanupAsync(ticket)
		return Lease{}, fmt.Errorf("%w: reset: %w", ErrUnavailable, err)
	}
	if !m.intentCurrent(ticket) {
		return Lease{}, ErrConflict
	}
	if r.Mode == Native {
		if err := native.Give(ctx, r.ViewerID); err != nil {
			m.failIntent(ticket)
			m.cleanupAsync(ticket)
			return Lease{}, fmt.Errorf("%w: native transfer: %w", ErrUnavailable, err)
		}
	}
	m.mu.Lock()
	if m.closed || m.pending == nil || m.pending.ticket != ticket || ctx.Err() != nil {
		if m.pending != nil && m.pending.ticket == ticket {
			m.pending = nil
		}
		m.mu.Unlock()
		m.cleanupAsync(ticket)
		if err := ctx.Err(); err != nil {
			return Lease{}, err
		}
		return Lease{}, ErrConflict
	}
	l := Lease{View: View{Owner: r.Owner, Session: r.Session, Mode: r.Mode, ViewerID: r.ViewerID, ExpiresAt: m.now().Add(r.Duration), Ready: true}, Token: token, Generation: ticket}
	m.current = &l
	m.pending = nil
	m.blocked = false
	m.armTimerLocked(r.Duration, ticket)
	m.mu.Unlock()
	return l, nil
}
func (m *Manager) armTimerLocked(d time.Duration, ticket uint64) {
	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = time.AfterFunc(d, func() {
		m.mu.Lock()
		if m.current == nil || m.current.Generation != ticket {
			m.mu.Unlock()
			return
		}
		remaining := m.current.ExpiresAt.Sub(m.now())
		if remaining > 0 {
			m.armTimerLocked(remaining, ticket)
			m.mu.Unlock()
			return
		}
		m.revokeLocked()
		next := m.generation
		m.mu.Unlock()
		m.cleanupAsync(next)
	})
}
func (m *Manager) intentCurrent(ticket uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.closed && m.pending != nil && m.pending.ticket == ticket
}
func (m *Manager) failIntent(ticket uint64) {
	m.mu.Lock()
	if m.pending != nil && m.pending.ticket == ticket {
		m.pending = nil
		m.blocked = true
	}
	m.mu.Unlock()
}
func (m *Manager) cancelIntent(ticket uint64) {
	m.failIntent(ticket)
	m.cleanupAsync(ticket)
}

// cleanupAsync never resets a newer published owner. While there is no owner,
// redundant cleanup may reset a pending acquisition: its own serialized reset
// still precedes grant. This ensures a Stop acknowledgement never relies on an
// acquisition that has merely queued, but has not yet released actual input.
func (m *Manager) cleanupAsync(ticket uint64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), m.resetTimeout)
		defer cancel()
		_ = m.resetAt(ctx, ticket)
	}()
}
func (m *Manager) resetAt(ctx context.Context, ticket uint64) error {
	if err := m.lock(ctx); err != nil {
		return err
	}
	defer m.unlock()
	m.mu.Lock()
	hasOwner := m.current != nil
	m.mu.Unlock()
	if hasOwner {
		return nil
	}
	if m.driver == nil {
		return ErrUnavailable
	}
	err := m.driver.Reset(ctx)
	m.mu.Lock()
	if m.generation == ticket && m.current == nil && m.pending == nil {
		m.blocked = err != nil
	}
	m.mu.Unlock()
	if err != nil {
		return fmt.Errorf("%w: reset: %w", ErrUnavailable, err)
	}
	return nil
}

// Dispatch executes one final input write. The callback must honor its context;
// do not put a long sequence of input events inside a single callback.
func (m *Manager) Dispatch(ctx context.Context, c Credentials, write func(context.Context) error) error {
	return m.dispatch(ctx, c, false, "", write)
}

// DispatchHuman dispatches an input operation for a human owner.
func (m *Manager) DispatchHuman(ctx context.Context, c Credentials, write func(context.Context) error) error {
	return m.dispatch(ctx, c, true, "", write)
}

// DispatchGated refuses normalized HTTP input during native control.
func (m *Manager) DispatchGated(ctx context.Context, c Credentials, write func(context.Context) error) error {
	return m.dispatch(ctx, c, false, Gated, write)
}

// DispatchHumanGated authorizes human RFB frames without bypassing native mode.
func (m *Manager) DispatchHumanGated(ctx context.Context, c Credentials, write func(context.Context) error) error {
	return m.dispatch(ctx, c, true, Gated, write)
}
func (m *Manager) dispatch(ctx context.Context, c Credentials, human bool, mode Mode, write func(context.Context) error) error {
	if write == nil {
		return ErrInvalid
	}
	l, err := m.Validate(c)
	if err != nil {
		return err
	}
	if (human && l.Owner != Human) || (mode != "" && l.Mode != mode) {
		return ErrConflict
	}
	if err = m.lock(ctx); err != nil {
		return err
	}
	locked := true
	defer func() {
		if locked {
			m.unlock()
		}
	}()
	if err = ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	l, err = m.validateLocked(c, l.Generation)
	if err == nil && ((human && l.Owner != Human) || (mode != "" && l.Mode != mode)) {
		err = ErrConflict
	}
	if err != nil {
		m.mu.Unlock()
		return err
	}
	active, cancel := context.WithCancel(ctx)
	m.inflight = cancel
	m.mu.Unlock()
	err = callWrite(active, write)
	if err == nil {
		err = ctx.Err()
	}
	cancel()
	m.mu.Lock()
	m.inflight = nil
	_, validErr := m.validateLocked(c, l.Generation)
	reset := err != nil && m.current != nil && m.current.Generation == l.Generation
	var ticket uint64
	if reset {
		m.revokeLocked()
		ticket = m.generation
	}
	m.mu.Unlock()
	if reset {
		m.unlock()
		locked = false
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), m.resetTimeout)
		defer cancelCleanup()
		resetErr := m.resetOrSchedule(cleanup, ticket)
		return errors.Join(err, resetErr)
	}
	if err != nil {
		return err
	}
	if validErr != nil {
		return validErr
	}
	return nil
}

// A driver panic must not leave a possibly held key under a valid lease.
func callWrite(ctx context.Context, write func(context.Context) error) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("%w: input driver panicked", ErrUnavailable)
		}
	}()
	return write(ctx)
}

// Release releases the specified lease and resets held input.
func (m *Manager) Release(ctx context.Context, c Credentials) error {
	m.mu.Lock()
	expired := m.expiredLocked()
	ticket := m.generation
	_, err := m.validateLocked(c, ticket)
	if err == nil {
		m.revokeLocked()
		ticket = m.generation
	}
	m.mu.Unlock()
	if expired {
		m.cleanupAsync(ticket)
	}
	if err != nil {
		return err
	}
	return m.resetOrSchedule(ctx, ticket)
}
func (m *Manager) resetOrSchedule(ctx context.Context, ticket uint64) error {
	err := m.resetAt(ctx, ticket)
	if err != nil {
		m.cleanupAsync(ticket)
	}
	return err
}

// Stop revokes the current lease and resets held input.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	m.revokeLocked()
	ticket := m.generation
	m.mu.Unlock()
	return m.resetOrSchedule(ctx, ticket)
}

// Expire revokes and resets the current lease if it has expired.
func (m *Manager) Expire(ctx context.Context) error {
	m.mu.Lock()
	expired := m.expiredLocked()
	ticket := m.generation
	m.mu.Unlock()
	if !expired {
		return nil
	}
	return m.resetOrSchedule(ctx, ticket)
}

// Close closes the manager and resets held input.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	m.revokeLocked()
	ticket := m.generation
	m.mu.Unlock()
	return m.resetOrSchedule(ctx, ticket)
}
