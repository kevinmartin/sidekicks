// Package compute defines workload capabilities independently of isolation backends.
package compute

import (
	"context"
	"errors"
	"time"
)

// AdapterID identifies a workload adapter.
type AdapterID string

const (
	// Shell identifies the shell workload adapter.
	Shell AdapterID = "shell"
	// BrowserChromium identifies the headless Chromium workload adapter.
	BrowserChromium AdapterID = "browser-chromium"
	// DesktopNoVNC identifies the noVNC desktop adapter.
	DesktopNoVNC AdapterID = "desktop-novnc"
	// DesktopNeko identifies the Neko desktop adapter.
	DesktopNeko AdapterID = "desktop-neko"
)

// RuntimeID identifies an isolation runtime.
type RuntimeID string

const (
	// Docker identifies the Docker isolation runtime.
	Docker RuntimeID = "docker"
	// AppleVZ identifies the Apple Virtualization framework runtime.
	AppleVZ RuntimeID = "apple-vz"
	// Kubernetes identifies the Kubernetes runtime.
	Kubernetes RuntimeID = "kubernetes"
)

var (
	// ErrUnsupported indicates an unsupported operation.
	ErrUnsupported = errors.New("unsupported capability or runtime")
	// ErrNotFound indicates that an instance was not found.
	ErrNotFound = errors.New("compute instance not found")
	// ErrConflict indicates a conflicting ownership or lifecycle operation.
	ErrConflict = errors.New("compute ownership conflict")
	// ErrInvalid indicates an invalid request.
	ErrInvalid = errors.New("invalid compute request")
)

// Resources specifies workload resource limits.
type Resources struct {
	CPUs        int   `json:"cpus"`
	MemoryBytes int64 `json:"memoryBytes"`
	PIDs        int64 `json:"pids"`
}

// Port describes a guest port and its requested host binding.
type Port struct {
	Guest    uint16 `json:"guest"`
	Protocol string `json:"protocol"`
	// HostPort zero requests an ephemeral loopback port.
	HostPort uint16 `json:"hostPort,omitempty"`
}

// NetworkPolicy specifies a workload's network access.
type NetworkPolicy string

const (
	// NetworkNone disables external networking for a workload.
	NetworkNone NetworkPolicy = "none"
	// NetworkEgress allows workload network egress without enforcing an egress allowlist.
	NetworkEgress NetworkPolicy = "egress"
)

// Spec describes a requested workload instance.
type Spec struct {
	ID        string        `json:"id"`
	Adapter   AdapterID     `json:"adapter"`
	Image     string        `json:"image"`
	Resources Resources     `json:"resources"`
	Network   NetworkPolicy `json:"network"`
	Ports     []Port        `json:"ports,omitempty"`
	// Env is private instance configuration; never return it from Inspect or logs.
	Env map[string]string `json:"-"`
}

// Endpoint describes an exposed guest endpoint.
type Endpoint struct {
	Guest    uint16 `json:"guest"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
}

// Instance describes an observed workload instance.
type Instance struct {
	ID        string     `json:"id"`
	Runtime   RuntimeID  `json:"runtime"`
	Adapter   AdapterID  `json:"adapter"`
	State     string     `json:"state"`
	Image     string     `json:"image"`
	Endpoints []Endpoint `json:"endpoints,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

// Runtime owns isolation and lifecycle, not browser or desktop semantics.
type Runtime interface {
	ID() RuntimeID
	Provision(context.Context, Spec) (Instance, error)
	Inspect(context.Context, string) (Instance, error)
	Start(context.Context, string) (Instance, error)
	Stop(context.Context, string) error
	Destroy(context.Context, string) error
}

// Capabilities describes the operations supported by an adapter.
type Capabilities struct {
	Exec         bool `json:"exec"`
	Files        bool `json:"files"`
	Browser      bool `json:"browser"`
	Desktop      bool `json:"desktop"`
	HumanControl bool `json:"humanControl"`
}

// Adapter describes workload capabilities independently of runtime isolation.
type Adapter interface {
	ID() AdapterID
	Capabilities() Capabilities
	Validate(Spec) error
}
