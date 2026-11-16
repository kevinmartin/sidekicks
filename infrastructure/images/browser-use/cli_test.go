//go:build integration

package browseruse

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kevinmartin/sidekicks/internal/domain/browserinstance"
	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
	"github.com/stretchr/testify/require"
)

func TestBrowserCLI(t *testing.T) {
	binary := os.Getenv("SIDEKICKS_CLI_BINARY")
	if binary == "" {
		t.Skip("run make cli-integration to exercise a local runtime")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var backends string
	if runtime.GOOS == "darwin" && os.Getenv("SIDEKICKS_TEST_BOTH") == "1" {
		backends = "apple docker"
	} else {
		backends = strings.TrimSpace(string(callCLI(t, binary, "config", "get", "runtime")))
	}
	for _, backend := range strings.Fields(backends) {
		t.Run(backend, func(t *testing.T) {
			testCLILifecycle(t, binary, backend)
		})
	}
	if backends == "apple docker" {
		t.Run("runtime-scoped names", func(t *testing.T) {
			name := testInstanceName()
			registerCLICleanup(t, binary, "apple", name)
			registerCLICleanup(t, binary, "docker", name)
			callCLI(t, binary, "--runtime", "apple", "browser", "start", "--name", name)
			callCLI(t, binary, "--runtime", "docker", "browser", "start", "--name", name)
			callCLI(t, binary, "config", "set", "runtime", "docker")
			t.Setenv("SIDEKICKS_RUNTIME", "")
			callCLI(t, binary, "browser", "stop", "--name", name)
			assertInstanceRunning(t, binary, "docker", name, false)
			callCLI(t, binary, "--runtime", "apple", "browser", "cdp", "--name", name, "Browser.getVersion")
		})
	}
}

func testCLILifecycle(t *testing.T, binary, backend string) {
	t.Helper()
	name, other := testInstanceName(), testInstanceName()
	registerCLICleanup(t, binary, backend, name)
	registerCLICleanup(t, binary, backend, other)
	browser := func(name string, args ...string) []byte {
		return callCLI(t, binary, append([]string{"--runtime", backend, "browser", "--name", name}, args...)...)
	}
	browser(name, "start")
	browser(other, "start")
	assertInstanceRunning(t, binary, backend, name, true)
	assertInstanceRunning(t, binary, backend, other, true)
	var tabs browserapi.Tabs
	require.NoError(t, json.Unmarshal(browser(name, "tabs"), &tabs))
	primary := tabs.Primary
	require.NotEmpty(t, primary)
	require.Len(t, tabs.Tabs, 1)
	require.Equal(t, primary, tabs.Tabs[0].ID)
	require.Equal(t, "about:blank", tabs.Tabs[0].URL)
	browser(name, "cdp", "--tab", primary, "Target.getTargets")
	_, err := runCLI(t.Context(), binary, "--runtime", backend, "browser", "cdp", "--name", name, "--tab", primary, "SystemInfo.getInfo")
	require.ErrorContains(t, err, "400 Bad Request")
	require.ErrorContains(t, err, "requires browser scope")

	const page = `<h1>CLI browser fixture</h1><label for="entry">Message</label><input id="entry"><button id="submit" onclick="document.getElementById('result').textContent = document.getElementById('entry').value">Submit</button><p id="result"></p>`
	browser(name, "cmd", "goto", "data:text/html,"+url.PathEscape(page))
	require.Contains(t, string(browser(name, "cmd", "snapshot")), "CLI browser fixture")
	browser(name, "cmd", "type", "#entry", "Sidekicks is ready")
	resultText := func() string {
		var result struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		data := browser(name, "cdp", "Runtime.evaluate", `{"expression":"document.querySelector('#result').textContent","returnByValue":true}`)
		require.NoError(t, json.Unmarshal(data, &result))
		return result.Result.Value
	}
	require.Empty(t, resultText())
	browser(name, "cmd", "click", "xpath=//*[@id='submit']")
	require.Equal(t, "Sidekicks is ready", resultText())
	var popup struct {
		ID string `json:"targetId"`
	}
	require.NoError(t, json.Unmarshal(browser(name, "cdp", "Target.createTarget", `{"url":"about:blank"}`), &popup))
	require.NotEmpty(t, popup.ID)
	browser(name, "cdp", "--tab", popup.ID, "Page.navigate", `{"url":"data:text/html,%3Ch1%3EAlternate%20tab%3C%2Fh1%3E"}`)
	require.Contains(t, string(browser(name, "cmd", "--tab", popup.ID, "snapshot")), "Alternate tab")
	require.Equal(t, "Sidekicks is ready", resultText())
	browser(name, "cdp", "Target.closeTarget", `{"targetId":"`+popup.ID+`"}`)
	_, err = runCLI(t.Context(), binary, "--runtime", backend, "browser", "cmd", "--name", name, "--tab", popup.ID, "snapshot")
	require.ErrorContains(t, err, "tab unavailable")
	_, err = runCLI(t.Context(), binary, "--runtime", backend, "browser", "cdp", "--name", name, "--tab", popup.ID, "Page.getFrameTree")
	require.ErrorContains(t, err, "404 Not Found")
	require.ErrorContains(t, err, "tab unavailable")
	screenshot := filepath.Join(t.TempDir(), "page.png")
	browser(name, "screenshot", "-o", screenshot)
	image, err := os.ReadFile(screenshot)
	require.NoError(t, err)
	info, err := png.DecodeConfig(bytes.NewReader(image))
	require.NoError(t, err)
	require.Positive(t, info.Width)
	require.Positive(t, info.Height)

	// Set a persistent cookie for an inert origin via CDP. The test needs no external server.
	require.Contains(t, string(browser(name, "cdp", "Network.setCookie", `{"name":"sidekicks-persist","value":"test","url":"https://sidekicks.test/","expires":2147483647}`)), `"success":true`)
	cookies := func(instance string) string {
		return string(browser(instance, "cdp", "Network.getCookies", `{"urls":["https://sidekicks.test/"]}`))
	}
	require.Contains(t, cookies(name), "sidekicks-persist")
	require.NotContains(t, cookies(other), "sidekicks-persist")
	browser(name, "stop")
	assertInstanceRunning(t, binary, backend, name, false)
	browser(other, "cdp", "Browser.getVersion")
	browser(name, "start")
	require.NoError(t, json.Unmarshal(browser(name, "tabs"), &tabs))
	require.NotEqual(t, primary, tabs.Primary)
	require.Contains(t, cookies(name), "sidekicks-persist")
	browser(name, "delete")
	browser(name, "start")
	require.NotContains(t, cookies(name), "sidekicks-persist")
}

func testInstanceName() string {
	return "cli-test-" + strings.ToLower(rand.Text()[:12])
}

func assertInstanceRunning(t *testing.T, binary, backend, name string, running bool) {
	t.Helper()
	var result []browserinstance.Instance
	data := callCLI(t, binary, "--runtime", backend, "list")
	require.NoError(t, json.Unmarshal(data, &result))
	require.Contains(t, result, browserinstance.Instance{
		Name:    name,
		Runtime: backend,
		Running: running,
	})
}

func registerCLICleanup(t *testing.T, binary, backend, name string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := runCLI(ctx, binary, "--runtime", backend, "browser", "delete", "--name", name)
		if err != nil && !strings.Contains(err.Error(), "no browser named") {
			t.Errorf("clean up %s browser %s: %v", backend, name, err)
		}
	})
}

func callCLI(t *testing.T, binary string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	result, err := runCLI(ctx, binary, args...)
	require.NoError(t, err, "sidekicks %q", args)
	return result
}

func runCLI(ctx context.Context, binary string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.Output()
	if exit, ok := err.(*exec.ExitError); ok {
		return nil, fmt.Errorf("%w: %s", exit, strings.TrimSpace(string(exit.Stderr)))
	}
	return output, err
}
