// Package containerruntime adapts the installed container CLIs to Sidekicks.
package containerruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/kevinmartin/sidekicks/internal/domain/browserinstance"
)

const (
	// DefaultImage is the local tag produced by the browser image build.
	DefaultImage    = "sidekicks-browser-use:latest"
	managedLabel    = "dev.sidekicks.workload"
	managedValue    = "browser"
	namePrefix      = "sidekicks-browser-"
	gatewayToken    = "BROWSER_GATEWAY_TOKEN"
	guestPortString = "8080"
	loopback        = "127.0.0.1"
)

// Open locates the selected CLI and configures its browser image.
func Open(ctx context.Context, backend, image string) (browserinstance.Runtime, error) {
	if image == "" {
		image = DefaultImage
	}
	if strings.HasPrefix(image, "-") || strings.ContainsAny(image, " \t\r\n") {
		return nil, fmt.Errorf("SIDEKICKS_BROWSER_IMAGE must be an image reference")
	}
	switch backend {
	case "apple":
		path, err := exec.LookPath("container")
		if err != nil {
			return nil, fmt.Errorf("find Apple container CLI (install container or select --runtime docker): %w", err)
		}
		return &appleRuntime{
			path:  path,
			image: image,
		}, nil
	case "docker":
		path, err := exec.LookPath("docker")
		if err != nil {
			return nil, fmt.Errorf("find Docker CLI: %w", err)
		}
		output, err := invoke(ctx, path, "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
		if err != nil {
			return nil, fmt.Errorf("resolve Docker context: %w", err)
		}
		host := strings.TrimSpace(string(output))
		if !strings.HasPrefix(host, "unix:///") {
			return nil, fmt.Errorf("the active Docker context must use a local Unix socket")
		}
		return &dockerRuntime{
			path:  path,
			image: image,
			host:  host,
		}, nil
	default:
		return nil, fmt.Errorf("unknown container runtime %q", backend)
	}
}

func invoke(ctx context.Context, path string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, path, args...)
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if exit, ok := err.(*exec.ExitError); ok {
		if message := strings.TrimSpace(string(exit.Stderr)); message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
	}
	return nil, err
}

func newTokenFile() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", "sidekicks-guest-*.env")
	if err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(file, "%s=%s\n", gatewayToken, hex.EncodeToString(token[:])); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func ownedName(id string, labels map[string]string) (string, bool) {
	name, ok := strings.CutPrefix(id, namePrefix)
	return name, ok && name != "" && labels[managedLabel] == managedValue
}

func tokenFromEnvironment(values []string) string {
	for _, value := range values {
		if token, ok := strings.CutPrefix(value, gatewayToken+"="); ok {
			return token
		}
	}
	return ""
}

func endpointFor(address string, port int) string {
	if address != loopback || port < 1 || port > 65535 {
		return ""
	}
	return "http://" + loopback + ":" + strconv.Itoa(port)
}
