// Package runtime selects an explicitly supported isolation backend.
package runtime

import (
	"fmt"

	"github.com/kevinmartin/sidekicks/internal/compute"
	"github.com/kevinmartin/sidekicks/internal/compute/runtime/docker"
)

// New does not silently replace an unavailable platform backend with Docker.
func New(id compute.RuntimeID, socketPath string) (compute.Runtime, error) {
	switch id {
	case compute.Docker:
		return docker.New(socketPath)
	case compute.AppleVZ, compute.Kubernetes:
		return nil, fmt.Errorf("%w: runtime %s is not implemented in the compute layer; historical poc evidence is not support", compute.ErrUnsupported, id)
	default:
		return nil, fmt.Errorf("%w: unknown runtime", compute.ErrUnsupported)
	}
}
