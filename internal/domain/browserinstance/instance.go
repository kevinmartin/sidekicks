// Package browserinstance implements lifecycle rules for named browser instances.
package browserinstance

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)

// Instance represents a browser owned by Sidekicks in the selected runtime.
type Instance struct {
	Name     string `json:"name"`
	Runtime  string `json:"runtime"`
	Running  bool   `json:"running"`
	ID       string `json:"-"`
	Endpoint string `json:"-"`
	Token    string `json:"-"`
}

// Runtime provides the container operations needed by the browser lifecycle.
type Runtime interface {
	Name() string
	List(ctx context.Context) ([]Instance, error)
	Create(ctx context.Context, name string) (string, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

// Manager coordinates the lifecycle of browsers on one runtime.
type Manager struct {
	runtime Runtime
	ready   func(context.Context, Instance) error
}

// New constructs a manager for the selected runtime.
func New(runtime Runtime, ready func(context.Context, Instance) error) *Manager {
	return &Manager{
		runtime: runtime,
		ready:   ready,
	}
}

// List returns the Sidekicks browsers in the selected runtime in name order.
func (m *Manager) List(ctx context.Context) ([]Instance, error) {
	instances, err := m.runtime.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list %s browsers: %w", m.runtime.Name(), err)
	}
	if instances == nil {
		return []Instance{}, nil
	}
	slices.SortFunc(instances, func(a, b Instance) int {
		return strings.Compare(a.Name, b.Name)
	})
	return instances, nil
}

// Start creates, starts, or reuses a named browser and waits for readiness.
func (m *Manager) Start(ctx context.Context, name string) (Instance, error) {
	instance, err := m.find(ctx, name)
	if err != nil {
		return Instance{}, err
	}
	if instance.ID == "" {
		id, createErr := m.runtime.Create(ctx, name)
		if createErr == nil {
			instance.ID = id
		} else {
			// A concurrent start can win the runtime's container-name reservation.
			instance, err = m.find(ctx, name)
			if err != nil || instance.ID == "" {
				return Instance{}, fmt.Errorf("create browser %q in %s: %w", name, m.runtime.Name(), errors.Join(createErr, err))
			}
		}
	}
	if !instance.Running {
		if startErr := m.runtime.Start(ctx, instance.ID); startErr != nil {
			current, err := m.find(ctx, name)
			if err != nil || current.ID != instance.ID || !current.Running {
				return Instance{}, fmt.Errorf("start browser %q in %s: %w", name, m.runtime.Name(), errors.Join(startErr, err))
			}
		}
	}
	return m.wait(ctx, name, instance.ID)
}

// Stop shuts down the browser and retains its disk state.
func (m *Manager) Stop(ctx context.Context, name string) (Instance, error) {
	instance, err := m.get(ctx, name)
	if err != nil || !instance.Running {
		return instance, err
	}
	if err := m.runtime.Stop(ctx, instance.ID); err != nil {
		return Instance{}, fmt.Errorf("stop browser %q in %s: %w", name, m.runtime.Name(), err)
	}
	instance.Running = false
	return instance, nil
}

// Delete removes the browser and the writable layer owned by its container.
func (m *Manager) Delete(ctx context.Context, name string) error {
	instance, err := m.get(ctx, name)
	if err != nil {
		return err
	}
	if instance.Running {
		if err := m.runtime.Stop(ctx, instance.ID); err != nil {
			return fmt.Errorf("stop browser %q in %s before deleting: %w", name, m.runtime.Name(), err)
		}
	}
	if err := m.runtime.Delete(ctx, instance.ID); err != nil {
		return fmt.Errorf("delete browser %q in %s: %w", name, m.runtime.Name(), err)
	}
	return nil
}

// Running returns a browser with a running guest endpoint.
func (m *Manager) Running(ctx context.Context, name string) (Instance, error) {
	instance, err := m.get(ctx, name)
	if err != nil {
		return Instance{}, err
	}
	if !instance.Running {
		return Instance{}, fmt.Errorf("browser %q in %s is stopped", name, m.runtime.Name())
	}
	if instance.Endpoint == "" || instance.Token == "" {
		return Instance{}, fmt.Errorf("browser %q in %s has no authenticated local endpoint", name, m.runtime.Name())
	}
	return instance, nil
}

func (m *Manager) get(ctx context.Context, name string) (Instance, error) {
	instance, err := m.find(ctx, name)
	if err != nil {
		return Instance{}, err
	}
	if instance.ID == "" {
		return Instance{}, fmt.Errorf("no browser named %q in %s", name, m.runtime.Name())
	}
	return instance, nil
}

func (m *Manager) find(ctx context.Context, name string) (Instance, error) {
	if !validName.MatchString(name) {
		return Instance{}, errors.New("name must contain 1–48 lowercase letters, digits, or hyphens and start with a letter or digit")
	}
	instances, err := m.List(ctx)
	if err != nil {
		return Instance{}, err
	}
	for _, instance := range instances {
		if instance.Name == name {
			return instance, nil
		}
	}
	return Instance{}, nil
}

func (m *Manager) wait(ctx context.Context, name, id string) (Instance, error) {
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	instance, err := m.get(ctx, name)
	if err != nil {
		return Instance{}, err
	}
	for {
		if instance.ID != id || !instance.Running {
			return Instance{}, fmt.Errorf("browser %q in %s stopped before it was ready; inspect the container logs", name, m.runtime.Name())
		}
		if instance.Endpoint == "" || instance.Token == "" {
			return Instance{}, fmt.Errorf("browser %q in %s has no authenticated local endpoint", name, m.runtime.Name())
		}
		probe, stop := context.WithTimeout(ctx, 2*time.Second)
		readyErr := m.ready(probe, instance)
		stop()
		if readyErr == nil {
			return instance, nil
		}
		select {
		case <-ctx.Done():
			return Instance{}, fmt.Errorf("wait for browser %q in %s: %w (%v)", name, m.runtime.Name(), ctx.Err(), readyErr)
		case <-ticker.C:
			instance, err = m.get(ctx, name)
			if err != nil {
				return Instance{}, err
			}
		}
	}
}
