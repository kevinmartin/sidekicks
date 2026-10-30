package adapter

import (
	"errors"
	"strings"
	"testing"

	"github.com/kevinmartin/sidekicks/internal/compute"
)

func validSpec() compute.Spec {
	return compute.Spec{ID: "qa-1", Adapter: compute.Shell, Image: "local/shell:current", Resources: compute.Resources{CPUs: 2, MemoryBytes: 2 << 30, PIDs: 256}, Network: compute.NetworkNone}
}

// TestRegistry verifies the adapter registry.
func TestRegistry(t *testing.T) {
	ids := IDs()
	if len(ids) != 4 {
		t.Fatal(ids)
	}
	ids[0] = "mutated"
	if IDs()[0] != compute.Shell {
		t.Fatal("mutable registry")
	}
	for _, id := range IDs() {
		a, err := Get(id)
		if err != nil || a.ID() != id {
			t.Fatal(id, err)
		}
		s := validSpec()
		s.Adapter = id
		if err = a.Validate(s); err != nil {
			t.Fatal(err)
		}
		c := a.Capabilities()
		if !c.Exec || !c.Files {
			t.Fatal(c)
		}
		if c.Browser != (id == compute.BrowserChromium) {
			t.Fatal(c)
		}
		if c.Desktop != (id == compute.DesktopNoVNC || id == compute.DesktopNeko) || c.HumanControl != c.Desktop {
			t.Fatal(c)
		}
		s.Adapter = "bad"
		if !errors.Is(a.Validate(s), compute.ErrInvalid) {
			t.Fatal("mismatch accepted")
		}
	}
	if _, err := Get("browser-firefox"); !errors.Is(err, compute.ErrUnsupported) {
		t.Fatal(err)
	}
	for _, id := range []compute.RuntimeID{compute.AppleVZ, compute.Kubernetes, "bad"} {
		if !errors.Is(ValidateRuntime(id), compute.ErrUnsupported) {
			t.Fatal(id)
		}
	}
	if err := ValidateRuntime(compute.Docker); err != nil {
		t.Fatal(err)
	}
}

// TestValidation verifies request validation.
func TestValidation(t *testing.T) {
	tests := map[string]func(*compute.Spec){
		"empty id": func(s *compute.Spec) {
			s.ID = ""
		},
		"uppercase": func(s *compute.Spec) {
			s.ID = "Bad"
		},
		"option id": func(s *compute.Spec) {
			s.ID = "--privileged"
		},
		"id length": func(s *compute.Spec) {
			s.ID = strings.Repeat("a", 64)
		},
		"id end": func(s *compute.Spec) {
			s.ID = "a-"
		},
		"adapter": func(s *compute.Spec) {
			s.Adapter = "desktop"
		},
		"image": func(s *compute.Spec) {
			s.Image = "--privileged"
		},
		"image space": func(s *compute.Spec) {
			s.Image = "base:tag other"
		},
		"image slash": func(s *compute.Spec) {
			s.Image = "repo//image"
		},
		"image trailing": func(s *compute.Spec) {
			s.Image = "repo/"
		},
		"bad digest": func(s *compute.Spec) {
			s.Image = "repo@sha256:no"
		},
		"two digests": func(s *compute.Spec) {
			s.Image = "repo@x@y"
		},
		"valid digest with extra separator": func(s *compute.Spec) {
			s.Image = "repo@sha256:" + strings.Repeat("a", 64) + "@extra"
		},
		"cpu zero": func(s *compute.Spec) {
			s.Resources.CPUs = 0
		},
		"cpu high": func(s *compute.Spec) {
			s.Resources.CPUs = 65
		},
		"memory zero": func(s *compute.Spec) {
			s.Resources.MemoryBytes = 0
		},
		"memory high": func(s *compute.Spec) {
			s.Resources.MemoryBytes = 1<<40 + 1
		},
		"pids zero": func(s *compute.Spec) {
			s.Resources.PIDs = 0
		},
		"pids high": func(s *compute.Spec) {
			s.Resources.PIDs = 65537
		},
		"network": func(s *compute.Spec) {
			s.Network = "host"
		},
		"none publish": func(s *compute.Spec) {
			s.Ports = []compute.Port{{Guest: 8080, Protocol: "tcp"}}
		},
		"zero port": func(s *compute.Spec) {
			s.Network = compute.NetworkEgress
			s.Ports = []compute.Port{{Protocol: "tcp"}}
		},
		"port protocol": func(s *compute.Spec) {
			s.Network = compute.NetworkEgress
			s.Ports = []compute.Port{{Guest: 80, Protocol: "TCP"}}
		},
		"duplicate guest": func(s *compute.Spec) {
			s.Network = compute.NetworkEgress
			s.Ports = []compute.Port{{Guest: 80, Protocol: "tcp"}, {Guest: 80, Protocol: "tcp"}}
		},
		"duplicate host": func(s *compute.Spec) {
			s.Network = compute.NetworkEgress
			s.Ports = []compute.Port{{Guest: 80, HostPort: 1234, Protocol: "tcp"}, {Guest: 81, HostPort: 1234, Protocol: "tcp"}}
		},
		"many ports": func(s *compute.Spec) {
			s.Network = compute.NetworkEgress
			s.Ports = make([]compute.Port, 65)
		},
		"env name": func(s *compute.Spec) {
			s.Env = map[string]string{"BAD=KEY": "x"}
		},
		"env nul": func(s *compute.Spec) {
			s.Env = map[string]string{"OK": "x\x00y"}
		},
		"env large": func(s *compute.Spec) {
			s.Env = map[string]string{"OK": strings.Repeat("x", 8193)}
		},
		"env count": func(s *compute.Spec) {
			s.Env = map[string]string{}
			for i := 0; i < 65; i++ {
				s.Env[strings.Repeat("x", i+1)] = ""
			}
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			s := validSpec()
			change(&s)
			if err := Validate(s); err == nil {
				t.Fatal("invalid specification accepted")
			}
		})
	}
	for _, id := range []string{"a", "a0", strings.Repeat("a", 63)} {
		if err := ValidateID(id); err != nil {
			t.Fatal(id, err)
		}
	}
	s := validSpec()
	s.Image = "registry.example:5000/repo@sha256:" + strings.Repeat("a", 64)
	s.Network = compute.NetworkEgress
	s.Ports = []compute.Port{{Guest: 8080, Protocol: "tcp"}, {Guest: 8080, Protocol: "udp"}}
	s.Env = map[string]string{"TOKEN": "private"}
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}
}
