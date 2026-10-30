// Package adapter describes workloads without selecting an isolation backend.
package adapter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kevinmartin/sidekicks/internal/compute"
)

var (
	idPattern     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,61}[a-z0-9]$|^[a-z]$`)
	imagePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]{0,254}$`)
	envPattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type workload struct {
	id           compute.AdapterID
	capabilities compute.Capabilities
}

// ID returns the workload adapter ID.
func (a workload) ID() compute.AdapterID {
	return a.id
}

// Capabilities returns the workload capabilities.
func (a workload) Capabilities() compute.Capabilities {
	return a.capabilities
}

// Validate validates the specification for this workload.
func (a workload) Validate(s compute.Spec) error {
	if s.Adapter != a.id {
		return fmt.Errorf("%w: adapter mismatch", compute.ErrInvalid)
	}
	return Validate(s)
}

// IDs returns a copy in stable order. Registering an adapter does not imply that
// its image or optional browser/desktop service is available at runtime.
func IDs() []compute.AdapterID {
	return []compute.AdapterID{compute.Shell, compute.BrowserChromium, compute.DesktopNoVNC, compute.DesktopNeko}
}

// Get returns the adapter registered for the given workload ID.
func Get(id compute.AdapterID) (compute.Adapter, error) {
	c := compute.Capabilities{Exec: true, Files: true}
	switch id {
	case compute.Shell:
	case compute.BrowserChromium:
		c.Browser = true
	case compute.DesktopNoVNC, compute.DesktopNeko:
		c.Desktop = true
		c.HumanControl = true
	default:
		return nil, fmt.Errorf("%w: adapter %q", compute.ErrUnsupported, id)
	}
	return workload{id, c}, nil
}

// ValidateRuntime intentionally refuses experimental reference POC backends.
// Their existence does not establish this compute layer's lifecycle contract.
func ValidateRuntime(id compute.RuntimeID) error {
	if id != compute.Docker {
		return fmt.Errorf("%w: runtime %q", compute.ErrUnsupported, id)
	}
	return nil
}

// ValidateID validates an instance ID.
func ValidateID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("%w: id must be a lowercase dns-style name of 1..63 characters", compute.ErrInvalid)
	}
	return nil
}

// Validate checks the deliberately restricted container specification. There
// are no host-mount, device, privileged, command-override or Docker-socket fields.
func Validate(s compute.Spec) error {
	if err := ValidateID(s.ID); err != nil {
		return err
	}
	if _, err := Get(s.Adapter); err != nil {
		return err
	}
	if !imagePattern.MatchString(s.Image) || strings.Contains(s.Image, "//") || strings.HasSuffix(s.Image, "/") {
		return fmt.Errorf("%w: invalid image reference", compute.ErrInvalid)
	}
	if image, digest, found := strings.Cut(s.Image, "@"); found {
		if image == "" || !digestPattern.MatchString(digest) {
			return fmt.Errorf("%w: invalid image digest", compute.ErrInvalid)
		}
	}
	if s.Resources.CPUs < 1 || s.Resources.CPUs > 64 {
		return fmt.Errorf("%w: cpus must be 1..64", compute.ErrInvalid)
	}
	if s.Resources.MemoryBytes < 64<<20 || s.Resources.MemoryBytes > 1<<40 {
		return fmt.Errorf("%w: memory must be 64 mebibytes..1 tebibytes", compute.ErrInvalid)
	}
	if s.Resources.PIDs < 16 || s.Resources.PIDs > 65536 {
		return fmt.Errorf("%w: pid limit must be 16..65536", compute.ErrInvalid)
	}
	if s.Network != compute.NetworkNone && s.Network != compute.NetworkEgress {
		return fmt.Errorf("%w: network policy", compute.ErrInvalid)
	}
	if s.Network == compute.NetworkNone && len(s.Ports) > 0 {
		return fmt.Errorf("%w: network-none cannot publish ports", compute.ErrInvalid)
	}
	if len(s.Ports) > 64 {
		return fmt.Errorf("%w: at most 64 published ports", compute.ErrInvalid)
	}
	guest := map[string]bool{}
	host := map[string]bool{}
	for _, p := range s.Ports {
		if p.Guest == 0 || (p.Protocol != "tcp" && p.Protocol != "udp") {
			return fmt.Errorf("%w: invalid port", compute.ErrInvalid)
		}
		key := fmt.Sprintf("%d/%s", p.Guest, p.Protocol)
		if guest[key] {
			return fmt.Errorf("%w: duplicate guest port", compute.ErrInvalid)
		}
		guest[key] = true
		if p.HostPort != 0 {
			key = fmt.Sprintf("%d/%s", p.HostPort, p.Protocol)
			if host[key] {
				return fmt.Errorf("%w: duplicate host port", compute.ErrInvalid)
			}
			host[key] = true
		}
	}
	if len(s.Env) > 64 {
		return fmt.Errorf("%w: at most 64 environment entries", compute.ErrInvalid)
	}
	for k, v := range s.Env {
		if !envPattern.MatchString(k) || len(v) > 8192 || strings.IndexByte(v, 0) >= 0 {
			return fmt.Errorf("%w: invalid environment entry", compute.ErrInvalid)
		}
	}
	return nil
}
