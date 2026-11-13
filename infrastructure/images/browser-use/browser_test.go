//go:build integration

package browseruse

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	stagehand "github.com/browserbase/stagehand/packages/sdk-go/v4"
)

var devClient = &http.Client{Timeout: 5 * time.Second}

// TestStagehandBrowser navigates a data URL in the deployed browser.
func TestStagehandBrowser(t *testing.T) {
	ctx := browserTestContext(t)
	browser := connectStagehand(t, ctx)
	page := newTestPage(t, ctx, browser)

	const title = "Sidekicks Stagehand smoke"
	fixture := "data:text/html," + url.PathEscape("<title>"+title+"</title><h1>Ready</h1>")
	if _, err := page.Goto(ctx, fixture, nil); err != nil {
		t.Fatalf("navigate to data fixture: %v", err)
	}
	if got, err := page.Title(ctx); err != nil || got != title {
		t.Fatalf("page title = %q, %v; want %q", got, err, title)
	}
}

func browserTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func connectStagehand(t *testing.T, ctx context.Context) *stagehand.BrowserContext {
	t.Helper()
	gatewaySocket, guestSocket := devBrowserSockets(t, ctx)
	browser, err := stagehand.ConnectLocalBrowser(ctx, stagehand.LocalBrowserConnectOptions{
		CDPURL:          gatewaySocket,
		ExtensionCDPURL: guestSocket,
		Headers: map[string]string{
			"Authorization": "Bearer " + os.Getenv("BROWSER_GATEWAY_TOKEN"),
		},
	})
	if err != nil {
		t.Fatalf("connect Stagehand to deployed Chromium: %v", err)
	}
	// Browser.Close sends Browser.close even for ConnectLocalBrowser. Close the
	// Stagehand context below and let the test process release the CDP socket.
	client, err := stagehand.Create(ctx, stagehand.CreateOptions{Browser: browser})
	if err != nil {
		t.Fatalf("initialize Stagehand: %v", err)
	}
	browserCleanup(t, "close Stagehand context", client.Close)
	browserContext, err := browser.Context()
	if err != nil {
		t.Fatal(err)
	}
	return browserContext
}

func devBrowserSockets(t *testing.T, ctx context.Context) (gatewaySocket, guestSocket string) {
	t.Helper()
	gateway := devEndpoint(t, "BROWSER_GATEWAY_URL")
	var version struct {
		URL string `json:"webSocketDebuggerUrl"`
	}
	readDevJSON(t, ctx, gateway+"/json/version", &version)
	guestURL, err := url.Parse(version.URL)
	guestCDP := os.Getenv("BROWSER_CDP_ADDRESS")
	if err != nil || guestURL.Scheme != "ws" || !net.ParseIP(guestURL.Hostname()).IsLoopback() || guestURL.Host != guestCDP ||
		!strings.HasPrefix(guestURL.Path, "/devtools/browser/") {
		t.Fatalf("unexpected Chromium discovery URL %q, expected guest loopback address %q", version.URL, guestCDP)
	}
	return "ws" + strings.TrimPrefix(gateway, "http") + guestURL.EscapedPath(), guestURL.String()
}

func newTestPage(t *testing.T, ctx context.Context, browser *stagehand.BrowserContext) *stagehand.Page {
	t.Helper()
	page, err := browser.NewPage(ctx)
	if err != nil {
		t.Fatalf("create page: %v", err)
	}
	browserCleanup(t, "close test tab", page.Close)
	return page
}

func browserCleanup(t *testing.T, description string, cleanup func(context.Context) error) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cleanup(ctx); err != nil {
			t.Errorf("%s: %v", description, err)
		}
	})
}

func devEndpoint(t *testing.T, name string) string {
	t.Helper()
	raw := strings.TrimRight(os.Getenv(name), "/")
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		t.Fatalf("set %s to the dev endpoint or run make dev and make integration", name)
	}
	if os.Getenv("BROWSER_GATEWAY_TOKEN") == "" {
		t.Fatal("set BROWSER_GATEWAY_TOKEN or run make dev and make integration")
	}
	return raw
}

func readDevJSON(t *testing.T, ctx context.Context, endpoint string, result any) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("BROWSER_GATEWAY_TOKEN"))
	resp, err := devClient.Do(req)
	if err != nil {
		t.Fatalf("connect to %s (is make dev running?): %v", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s", endpoint, resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(result); err != nil {
		t.Fatalf("decode dev endpoint response: %v", err)
	}
}
