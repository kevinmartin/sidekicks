package containerruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kevinmartin/sidekicks/internal/domain/browserinstance"
)

type appleRuntime struct {
	path  string
	image string
}

const appleGuestSocket = "/home/browser/gateway.sock"

const appleSocketPrefix = "sidekicks-browser-"

func (r *appleRuntime) Name() string {
	return "apple"
}

func (r *appleRuntime) List(ctx context.Context) ([]browserinstance.Instance, error) {
	output, err := invoke(ctx, r.path, "list", "--all", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("run Apple container list (check container system status): %w", err)
	}
	var containers []struct {
		ID            string `json:"id"`
		Configuration struct {
			Labels map[string]string `json:"labels"`
			Init   struct {
				Environment []string `json:"environment"`
			} `json:"initProcess"`
			Sockets []struct {
				Guest string `json:"containerPath"`
				Host  string `json:"hostPath"`
			} `json:"publishedSockets"`
		} `json:"configuration"`
		Status struct {
			State string `json:"state"`
		} `json:"status"`
	}
	if err := json.Unmarshal(output, &containers); err != nil {
		return nil, fmt.Errorf("decode Apple container list: %w", err)
	}
	var instances []browserinstance.Instance
	for _, container := range containers {
		name, owned := ownedName(container.ID, container.Configuration.Labels)
		if !owned {
			continue
		}
		instance := browserinstance.Instance{
			Name:    name,
			Runtime: r.Name(),
			Running: container.Status.State == "running",
			ID:      container.ID,
			Token:   tokenFromEnvironment(container.Configuration.Init.Environment),
		}
		for _, socket := range container.Configuration.Sockets {
			if socket.Guest == appleGuestSocket && appleSocketDirectory(socket.Host) != "" {
				instance.Endpoint = "unix://" + socket.Host
				break
			}
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func (r *appleRuntime) Create(ctx context.Context, name string) (string, error) {
	envFile, err := newTokenFile()
	if err != nil {
		return "", fmt.Errorf("create guest token file: %w", err)
	}
	defer func() {
		_ = os.Remove(envFile)
	}()
	// Use a short private path within macOS's Unix socket path limit.
	directory, err := os.MkdirTemp("/tmp", appleSocketPrefix)
	if err != nil {
		return "", fmt.Errorf("allocate Apple guest socket: %w", err)
	}
	socket := filepath.Join(directory, "gateway.sock")
	output, err := invoke(ctx, r.path, "create",
		"--name", namePrefix+name,
		"--label", managedLabel+"="+managedValue,
		"--env-file", envFile,
		"--env", "BROWSER_GATEWAY_SOCKET="+appleGuestSocket,
		"--publish-socket", socket+":"+appleGuestSocket,
		"--shm-size", "256M",
		r.image,
	)
	if err != nil {
		_ = os.Remove(directory)
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (r *appleRuntime) Start(ctx context.Context, id string) error {
	directory, err := r.socketDirectory(ctx, id)
	if err != nil {
		return err
	}
	if directory == "" {
		return fmt.Errorf("container %q has no private guest socket directory", id)
	}
	// The OS can remove temporary directories while an instance is stopped.
	if err := os.Mkdir(directory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("prepare Apple guest socket directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("check Apple guest socket directory: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("guest socket directory %s must be private", directory)
	}
	_, err = invoke(ctx, r.path, "start", id)
	return err
}

func (r *appleRuntime) Stop(ctx context.Context, id string) error {
	_, err := invoke(ctx, r.path, "stop", id)
	return err
}

func (r *appleRuntime) Delete(ctx context.Context, id string) error {
	directory, err := r.socketDirectory(ctx, id)
	if err != nil {
		return err
	}
	if _, err := invoke(ctx, r.path, "delete", id); err != nil {
		return err
	}
	if directory != "" {
		if err := os.Remove(directory); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove Apple guest socket directory: %w", err)
		}
	}
	return nil
}

func (r *appleRuntime) socketDirectory(ctx context.Context, id string) (string, error) {
	instances, err := r.List(ctx)
	if err != nil {
		return "", err
	}
	for _, instance := range instances {
		if instance.ID == id {
			return appleSocketDirectory(strings.TrimPrefix(instance.Endpoint, "unix://")), nil
		}
	}
	return "", fmt.Errorf("no Sidekicks container with ID %q", id)
}

func appleSocketDirectory(path string) string {
	directory := filepath.Dir(path)
	if path != filepath.Join(directory, "gateway.sock") || filepath.Dir(directory) != "/tmp" ||
		!strings.HasPrefix(filepath.Base(directory), appleSocketPrefix) {
		return ""
	}
	return directory
}
