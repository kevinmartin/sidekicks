package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute"
	docker "github.com/kevinmartin/sidekicks/internal/compute/runtime/docker"
)

func ownedGuest(t *testing.T, adapter compute.AdapterID, image string, env map[string]string) (context.Context, *docker.Runtime, string) {
	t.Helper()
	if os.Getenv("SIDEKICKS_DOCKER_INTEGRATION") != "1" {
		t.Skip("make integration")
	}
	socket := os.Getenv("SIDEKICKS_DOCKER_SOCKET")
	if socket == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			t.Fatal(e)
		}
		socket = filepath.Join(home, ".docker/run/docker.sock")
	}
	rt, e := docker.New(socket)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	t.Cleanup(cancel)
	id := fmt.Sprintf("sidekicks-compute-e2e-%d", time.Now().UnixNano())
	if env == nil {
		env = map[string]string{}
	}
	env["SIDEKICKS_AGENT_TOKEN"] = strings.Repeat("b", 64)
	env["SIDEKICKS_ADAPTER"] = string(adapter)
	if _, e = rt.Provision(ctx, compute.Spec{ID: id, Adapter: adapter, Image: image, Resources: compute.Resources{CPUs: 2, MemoryBytes: 2 << 30, PIDs: 256}, Network: compute.NetworkNone, Env: env}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if e := rt.Stop(c, id); e != nil {
			t.Errorf("stop own: %v", e)
		}
		if e := rt.Destroy(c, id); e != nil {
			t.Errorf("destroy own: %v", e)
		}
	})
	// Provisioning and guest readiness are distinct boundaries.
	deadline := time.Now().Add(20 * time.Second)
	for {
		_, _, e = callGuest(ctx, rt, id, "GET", "/health", nil)
		if e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("guest readiness: %v", e)
		}
		time.Sleep(75 * time.Millisecond)
	}
	return ctx, rt, id
}
func callGuest(ctx context.Context, rt *docker.Runtime, id, method, path string, body []byte) (int, []byte, error) {
	raw, e := json.Marshal(map[string]any{"method": method, "path": path, "body": body})
	if e != nil {
		return 0, nil, e
	}
	data, e := rt.Invoke(ctx, id, raw)
	if e != nil {
		return 0, nil, e
	}
	var res struct {
		Status int    `json:"status"`
		Body   []byte `json:"body"`
	}
	if e = json.Unmarshal(data, &res); e != nil {
		return 0, nil, e
	}
	if res.Status < 200 || res.Status >= 300 {
		return res.Status, res.Body, fmt.Errorf("guest http%d: %s", res.Status, res.Body)
	}
	return res.Status, res.Body, nil
}
func browserTestImage(candidate string) (string, error) {
	if candidate == "" {
		return "sidekicks-compute/browser-chromium:dev", nil
	}
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(candidate) {
		return "", errors.New("candidate browser image must be an exact local sha256 image id")
	}
	return candidate, nil
}

// TestBrowserCandidateImageRequiresExactID verifies browser candidate image requires exact id.
func TestBrowserCandidateImageRequiresExactID(t *testing.T) {
	if got, err := browserTestImage(""); err != nil || got != "sidekicks-compute/browser-chromium:dev" {
		t.Fatalf("default image: %q %v", got, err)
	}
	want := "sha256:" + strings.Repeat("a", 64)
	if got, err := browserTestImage(want); err != nil || got != want {
		t.Fatalf("exact candidate: %q %v", got, err)
	}
	for _, bad := range []string{"ubuntu:latest", "sha256:abc", "sha256:" + strings.Repeat("A", 64)} {
		if _, err := browserTestImage(bad); err == nil {
			t.Fatalf("accepted non-immutable image reference %q", bad)
		}
	}
}

// TestDockerBrowserFunctional verifies docker browser functional.
func TestDockerBrowserFunctional(t *testing.T) {
	imageRef, err := browserTestImage(os.Getenv("SIDEKICKS_TEST_BROWSER_IMAGE"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, rt, id := ownedGuest(t, compute.BrowserChromium, imageRef, nil)
	action := func(req map[string]any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(req)
		_, body, e := callGuest(ctx, rt, id, "POST", "/browser", b)
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]any
		if e = json.Unmarshal(body, &v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	page := action(map[string]any{"action": "new"})["pageId"].(string)
	// Verify the real guest Chromium processes did not receive the flag that
	// disables its sandbox. This is not a full sandbox-escape assessment.
	probe, _ := json.Marshal(map[string]any{"argv": []string{"/bin/sh", "-c", `count=0; for p in /proc/[0-9]*; do exe=$(readlink "$p/exe" 2>/dev/null || :); case "$exe" in */chromium) count=$((count+1)); if tr '\000' '\n' < "$p/cmdline" | grep -Fx -- '--no-sandbox' >/dev/null; then exit 3; fi ;; esac; done; test "$count" -gt 0`}, "timeoutMs": 3000})
	_, probeBody, probeErr := callGuest(ctx, rt, id, "POST", "/exec", probe)
	var probeResult struct {
		ExitCode int `json:"exitCode"`
	}
	if probeErr != nil || json.Unmarshal(probeBody, &probeResult) != nil || probeResult.ExitCode != 0 {
		t.Fatalf("Chromium process/sandbox-flag probe: %s %v", probeBody, probeErr)
	}
	action(map[string]any{"action": "navigate", "pageId": page, "url": "http://127.0.0.1:8081/fixture"})
	title := action(map[string]any{"action": "title", "pageId": page})["title"].(string)
	if !strings.Contains(title, "Synthetic Desktop Fixture") {
		t.Fatalf("title %q", title)
	}
	text := action(map[string]any{"action": "text", "pageId": page, "selector": "h1"})["text"].(string)
	if !strings.Contains(text, "Input Lab") {
		t.Fatalf("DOM %q", text)
	}
	action(map[string]any{"action": "click", "pageId": page, "x": 180, "y": 175})
	action(map[string]any{"action": "type", "pageId": page, "text": "Exact Unicode ✓"})
	// Focus and exact text are observed from the fixture, not inferred from a CDP acknowledgment.
	log := action(map[string]any{"action": "text", "pageId": page, "selector": "#event-log"})["text"].(string)
	if !strings.Contains(log, "Exact Unicode ✓") {
		t.Fatalf("typed text not observed in fixture: %q", log)
	}
	shot := action(map[string]any{"action": "screenshot", "pageId": page, "width": 960, "height": 540})
	encoded, _ := json.Marshal(shot["data"])
	var imageBytes []byte
	if e := json.Unmarshal(encoded, &imageBytes); e != nil {
		t.Fatal(e)
	}
	image, e := png.Decode(bytes.NewReader(imageBytes))
	if e != nil || image.Bounds().Dx() != 960 || image.Bounds().Dy() != 540 {
		t.Fatalf("PNG %v %v", image, e)
	}
	action(map[string]any{"action": "close", "pageId": page})
	invalid, _ := json.Marshal(map[string]any{"action": "title", "pageId": page})
	status, _, e := callGuest(ctx, rt, id, "POST", "/browser", invalid)
	if e == nil || status != 404 {
		t.Fatalf("closed page %d %v", status, e)
	}
	if err := rt.Stop(ctx, id); err != nil {
		t.Fatalf("stop browser guest: %v", err)
	}
	if _, err := rt.Start(ctx, id); err != nil {
		t.Fatalf("restart browser guest: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		_, _, err := callGuest(ctx, rt, id, "GET", "/health", nil)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restarted browser readiness: %v", err)
		}
		time.Sleep(75 * time.Millisecond)
	}
	newPage := action(map[string]any{"action": "new"})["pageId"].(string)
	action(map[string]any{"action": "navigate", "pageId": newPage, "url": "http://127.0.0.1:8081/fixture"})
	if got := action(map[string]any{"action": "text", "pageId": newPage, "selector": "h1"})["text"].(string); !strings.Contains(got, "Input Lab") {
		t.Fatalf("restarted browser DOM: %q", got)
	}
	action(map[string]any{"action": "close", "pageId": newPage})
}
