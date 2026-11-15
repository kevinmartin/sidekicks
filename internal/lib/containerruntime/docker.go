package containerruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kevinmartin/sidekicks/internal/domain/browserinstance"
)

type dockerRuntime struct {
	path  string
	image string
	host  string
}

func (r *dockerRuntime) Name() string {
	return "docker"
}

func (r *dockerRuntime) call(ctx context.Context, args ...string) ([]byte, error) {
	return invoke(ctx, r.path, append([]string{"--host", r.host, "container"}, args...)...)
}

func (r *dockerRuntime) List(ctx context.Context) ([]browserinstance.Instance, error) {
	output, err := r.call(ctx, "list", "--all", "--quiet", "--no-trunc", "--filter", "label="+managedLabel+"="+managedValue)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(output))
	if len(ids) == 0 {
		return nil, nil
	}
	output, err = r.call(ctx, append([]string{"inspect"}, ids...)...)
	if err != nil {
		return nil, err
	}
	var containers []struct {
		ID     string
		Name   string
		Config struct {
			Labels map[string]string
			Env    []string
		}
		State struct {
			Running bool
		}
		NetworkSettings struct {
			Ports map[string][]struct {
				HostIP   string `json:"HostIp"`
				HostPort string
			}
		}
	}
	if err := json.Unmarshal(output, &containers); err != nil {
		return nil, fmt.Errorf("decode Docker container inspect: %w", err)
	}
	var instances []browserinstance.Instance
	for _, container := range containers {
		name, owned := ownedName(strings.TrimPrefix(container.Name, "/"), container.Config.Labels)
		if !owned {
			continue
		}
		instance := browserinstance.Instance{
			Name:    name,
			Runtime: r.Name(),
			Running: container.State.Running,
			ID:      container.ID,
			Token:   tokenFromEnvironment(container.Config.Env),
		}
		for _, port := range container.NetworkSettings.Ports[guestPortString+"/tcp"] {
			number, err := strconv.Atoi(port.HostPort)
			if err == nil && port.HostIP == loopback {
				instance.Endpoint = endpointFor(port.HostIP, number)
				break
			}
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func (r *dockerRuntime) Create(ctx context.Context, name string) (string, error) {
	envFile, err := newTokenFile()
	if err != nil {
		return "", fmt.Errorf("create guest token file: %w", err)
	}
	defer os.Remove(envFile)
	output, err := r.call(ctx, "create",
		"--name", namePrefix+name,
		"--label", managedLabel+"="+managedValue,
		"--env-file", envFile,
		"--publish", loopback+"::"+guestPortString,
		"--shm-size", "256m",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--security-opt", "seccomp=unconfined",
		r.image,
	)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (r *dockerRuntime) Start(ctx context.Context, id string) error {
	_, err := r.call(ctx, "start", id)
	return err
}

func (r *dockerRuntime) Stop(ctx context.Context, id string) error {
	_, err := r.call(ctx, "stop", id)
	return err
}

func (r *dockerRuntime) Delete(ctx context.Context, id string) error {
	_, err := r.call(ctx, "rm", id)
	return err
}
