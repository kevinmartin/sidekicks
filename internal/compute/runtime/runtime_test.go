package runtime

import (
	"errors"
	"github.com/kevinmartin/sidekicks/internal/compute"
	"testing"
)

// TestSelection verifies runtime selection.
func TestSelection(t *testing.T) {
	r, err := New(compute.Docker, "/tmp/does-not-connect-until-used.sock")
	if err != nil || r.ID() != compute.Docker {
		t.Fatal(r, err)
	}
	for _, id := range []compute.RuntimeID{compute.AppleVZ, compute.Kubernetes, "unknown", ""} {
		if _, err := New(id, ""); !errors.Is(err, compute.ErrUnsupported) {
			t.Fatalf("%s: %v", id, err)
		}
	}
}
