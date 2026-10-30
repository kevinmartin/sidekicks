package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute"
	docker "github.com/kevinmartin/sidekicks/internal/compute/runtime/docker"
)

func shellTestImage(candidate string) (string, error) {
	if candidate == "" {
		return "sidekicks-compute/shell:dev", nil
	}
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(candidate) {
		return "", errors.New("candidate shell image must be an exact local sha256 image id")
	}
	return candidate, nil
}

// TestShellCandidateImageRequiresExactID verifies shell candidate image requires exact id.
func TestShellCandidateImageRequiresExactID(t *testing.T) {
	if got, err := shellTestImage(""); err != nil || got != "sidekicks-compute/shell:dev" {
		t.Fatalf("default image: %q %v", got, err)
	}
	want := "sha256:" + strings.Repeat("a", 64)
	if got, err := shellTestImage(want); err != nil || got != want {
		t.Fatalf("exact candidate: %q %v", got, err)
	}
	for _, bad := range []string{"ubuntu:latest", "sha256:abc", "sha256:" + strings.Repeat("A", 64)} {
		if _, err := shellTestImage(bad); err == nil {
			t.Fatalf("accepted non-immutable image reference %q", bad)
		}
	}
}

// TestDockerShellLifecycle verifies docker shell lifecycle.
func TestDockerShellLifecycle(t *testing.T) {
	if os.Getenv("SIDEKICKS_DOCKER_INTEGRATION") != "1" {
		t.Skip("explicit Docker functional test: make integration")
	}
	socket := os.Getenv("SIDEKICKS_DOCKER_SOCKET")
	if socket == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		socket = filepath.Join(home, ".docker/run/docker.sock")
	}
	rt, err := docker.New(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	id := fmt.Sprintf("sidekicks-compute-e2e-%d", time.Now().UnixNano())
	image, err := shellTestImage(os.Getenv("SIDEKICKS_TEST_SHELL_IMAGE"))
	if err != nil {
		t.Fatal(err)
	}
	spec := compute.Spec{ID: id, Adapter: compute.Shell, Image: image, Resources: compute.Resources{CPUs: 1, MemoryBytes: 128 << 20, PIDs: 64}, Network: compute.NetworkNone, Env: map[string]string{"SIDEKICKS_AGENT_TOKEN": strings.Repeat("a", 64)}}
	instance, err := rt.Provision(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := rt.Stop(c, id); err != nil {
			t.Errorf("cleanup stop own %s: %v", id, err)
		}
		if err := rt.Destroy(c, id); err != nil {
			t.Errorf("cleanup own %s: %v", id, err)
		}
	})
	if instance.State != "running" || len(instance.Endpoints) != 0 {
		t.Fatalf("unexpected instance %+v", instance)
	}
	if strings.HasPrefix(image, "sha256:") && instance.Image != image {
		t.Fatalf("provisioned image differs from requested immutable ID: got %q want %q", instance.Image, image)
	}
	invoke := func(method, path string, body []byte) (int, []byte) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"method": method, "path": path, "body": body})
		result, err := rt.Invoke(ctx, id, raw)
		if err != nil {
			t.Fatal(err)
		}
		var r struct {
			Status int    `json:"status"`
			Body   []byte `json:"body"`
		}
		if err := json.Unmarshal(result, &r); err != nil {
			t.Fatalf("envelope: %v %s", err, result)
		}
		return r.Status, r.Body
	}
	status, body := invoke("GET", "/health", nil)
	if status != 200 {
		t.Fatalf("health %d %s", status, body)
	}
	status, body = invoke("PUT", "/files?path=proof.txt", []byte("Sidekicks exact UTF-8 ✓\n"))
	if status != 200 {
		t.Fatalf("write %d %s", status, body)
	}
	status, body = invoke("GET", "/files?path=proof.txt", nil)
	if status != 200 || string(body) != "Sidekicks exact UTF-8 ✓\n" {
		t.Fatalf("read %d %q", status, body)
	}
	status, _ = invoke("GET", "/files?path=../etc/passwd", nil)
	if status < 400 {
		t.Fatal("path escape accepted")
	}
	command, _ := json.Marshal(map[string]any{"argv": []string{"/bin/sh", "-c", "printf 'Go guest execution'; id -u; test ! -S /var/run/docker.sock; test ! -d /host"}, "timeoutMs": 3000})
	status, body = invoke("POST", "/exec", command)
	var execResult struct {
		ExitCode int    `json:"exitCode"`
		Stdout   string `json:"stdout"`
		TimedOut bool   `json:"timedOut"`
	}
	if err := json.Unmarshal(body, &execResult); err != nil || status != 200 || execResult.ExitCode != 0 || !strings.Contains(execResult.Stdout, "1000") {
		t.Fatalf("exec %d %s %v", status, body, err)
	}
	command, _ = json.Marshal(map[string]any{"argv": []string{"/bin/sh", "-c", "sleep 30 & wait"}, "timeoutMs": 150})
	status, body = invoke("POST", "/exec", command)
	if err := json.Unmarshal(body, &execResult); err != nil || status != 200 || !execResult.TimedOut {
		t.Fatalf("timeout %d %s %v", status, body, err)
	}
	// Exercise host cancellation against a command that is known to have started.
	// The delayed file must not appear after the cancellation acknowledgment.
	command, _ = json.Marshal(map[string]any{"argv": []string{"/bin/sh", "-c", "printf ready > /workspace/cancel-started; sleep 3; printf late > /workspace/cancel-late"}, "timeoutMs": 10000})
	raw, _ := json.Marshal(map[string]any{"method": "POST", "path": "/exec", "body": command})
	cancelCtx, cancelInvoke := context.WithCancel(ctx)
	finished := make(chan error, 1)
	go func() {
		_, err := rt.Invoke(cancelCtx, id, raw)
		finished <- err
	}()
	started := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		status, body = invoke("GET", "/files?path=cancel-started", nil)
		if status == 200 && string(body) == "ready" {
			started = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !started {
		cancelInvoke()
		t.Fatal("cancellation test command did not start")
	}
	cancelInvoke()
	select {
	case err := <-finished:
		var canceled *docker.CancellationError
		if !errors.As(err, &canceled) || !errors.Is(err, context.Canceled) || !canceled.Confirmed || !canceled.WasActive {
			t.Fatalf("active cancellation not confirmed: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	time.Sleep(3200 * time.Millisecond)
	status, _ = invoke("GET", "/files?path=cancel-late", nil)
	if status != 404 {
		t.Fatalf("canceled command's delayed write was not absent: status %d", status)
	}
	if err := rt.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	instance, err = rt.Inspect(ctx, id)
	if err != nil || instance.State == "running" {
		t.Fatalf("stop %+v %v", instance, err)
	}
	if _, err = rt.Start(ctx, id); err != nil {
		t.Fatal(err)
	}
	status, body = invoke("GET", "/health", nil)
	if status != 200 {
		t.Fatalf("restart health %d %s", status, body)
	}
	// Workspaces are deliberately ephemeral tmpfs: persistence is not promised.
	if err := rt.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := rt.Destroy(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err = rt.Inspect(ctx, id); !errors.Is(err, compute.ErrNotFound) {
		t.Fatalf("destroy inspect %v", err)
	}
}
