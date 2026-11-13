//go:build integration

package browseruse

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"testing"
	"time"

	stagehand "github.com/browserbase/stagehand/packages/sdk-go/v4"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestStagehandWBA(t *testing.T) {
	ctx := browserTestContext(t)
	fixture := devEndpoint(t, "WBA_FIXTURE_URL")
	var origins struct {
		Signed      string `json:"signed"`
		BlockedHost string `json:"blockedHost"`
	}
	readDevJSON(ctx, t, fixture+"/origins", &origins)
	signed, err := url.Parse(origins.Signed)
	if err != nil || signed.Scheme != "https" || signed.Hostname() == "" || origins.BlockedHost == "" {
		t.Fatalf("invalid dev fixture origins: %+v", origins)
	}

	browser := connectStagehand(ctx, t)
	previousPolicy, err := browser.GetDomainPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := browser.SetDomainPolicy(ctx, &stagehand.DomainPolicy{BlockedDomains: []string{origins.BlockedHost}}); err != nil {
		t.Fatal(err)
	}
	browserCleanup(t, "restore domain policy", func(ctx context.Context) error {
		return browser.SetDomainPolicy(ctx, previousPolicy)
	})
	domain, name := stagehand.ExactCookie(signed.Hostname()), stagehand.ExactCookie("session")
	browserCleanup(t, "clear fixture cookie", func(ctx context.Context) error {
		return browser.ClearCookies(ctx, &stagehand.ClearCookieOptions{
			Domain: &domain,
			Name:   &name,
		})
	})

	page, conn := newWBAPage(ctx, t, browser)
	waitForWBATab(ctx, t, fixture, page.PageID(), true)
	if err := page.SetExtraHTTPHeaders(ctx, map[string]string{
		"X-Sidekicks-Existing": "present",
	}); err != nil {
		t.Fatal(err)
	}
	gotoTitle := func(url, title string) {
		t.Helper()
		// Wait for the fixture's iframe and image before navigating away.
		options := &stagehand.PageNavigationOptions{WaitUntil: new(stagehand.LoadStateLoad)}
		if resp, err := page.Goto(ctx, url, options); err != nil || resp == nil || resp.Status() != http.StatusOK {
			t.Fatalf("navigate to %s: response %v, error %v", url, resp, err)
		}
		if got, err := page.Title(ctx); err != nil || got != title {
			t.Fatalf("page title = %q (%v), want %q", got, err, title)
		}
	}

	run := rand.Text()
	query := "?run=" + url.QueryEscape(run)
	gotoTitle(origins.Signed+"/a%2Fb"+query+"&q=one%2Btwo", "Signed request verified")
	gotoTitle(origins.Signed+"/redirect"+query, "Unsigned redirect verified")
	for _, path := range []string{"/blocked-redirect", "/timeout"} {
		if resp, err := page.Goto(ctx, origins.Signed+path+query, nil); err == nil && resp != nil && resp.Status() < 400 {
			t.Fatalf("expected navigation failure for %s, got %v", path, resp)
		}
	}
	events := observations(ctx, t, fixture, run)
	for _, unexpected := range []string{"origin /blocked", "origin /timeout"} {
		if slices.Contains(events, unexpected) {
			t.Errorf("rejected navigation reached the origin: %s", unexpected)
		}
	}
	for _, expected := range []string{"unsigned /frame", "unsigned /asset", "verified blocked redirect", "broker timeout"} {
		if !slices.Contains(events, expected) {
			t.Errorf("fixture did not observe %s", expected)
		}
	}

	// Chromium detaches the extension debugger when the tab navigates to a
	// privileged browser page.
	if _, err := page.Goto(ctx, "chrome://version/", nil); err != nil {
		t.Fatalf("navigate WBA tab to a privileged browser page: %v", err)
	}
	waitForWBATab(ctx, t, fixture, page.PageID(), false)
	if _, err := sendTargetCommand(ctx, conn, 3, "Target.getTargetInfo", map[string]string{
		"targetId": page.PageID(),
	}); err != nil {
		t.Fatalf("WBA tab did not remain open after debugger detach: %v", err)
	}
}

func newWBAPage(ctx context.Context, t *testing.T, browser *stagehand.BrowserContext) (*stagehand.Page, *websocket.Conn) {
	t.Helper()
	// Stagehand creates its own extension page before navigating. Chromium
	// can retain that origin on about:blank and refuse Sidekicks' debugger.
	// Create a neutral blank target the way a session launcher would.
	socket, _ := devBrowserSockets(ctx, t)
	conn, _, err := websocket.Dial(ctx, socket, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": {"Bearer " + os.Getenv("BROWSER_GATEWAY_TOKEN")},
		},
	})
	if err != nil {
		t.Fatalf("connect to browser gateway: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	targetID, err := sendTargetCommand(ctx, conn, 1, "Target.createTarget", map[string]string{
		"url": "about:blank",
	})
	if err != nil || targetID == "" {
		t.Fatalf("create neutral browser target: %v", err)
	}
	browserCleanup(t, "close WBA test tab", func(ctx context.Context) error {
		_, err := sendTargetCommand(ctx, conn, 2, "Target.closeTarget", map[string]string{
			"targetId": targetID,
		})
		return err
	})

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		pages, err := browser.Pages(waitCtx)
		if err != nil {
			t.Fatalf("list Stagehand pages: %v", err)
		}
		for _, page := range pages {
			if page.PageID() == targetID {
				return page, conn
			}
		}
		select {
		case <-waitCtx.Done():
			t.Fatalf("Stagehand did not attach to target %s: %v", targetID, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func sendTargetCommand(ctx context.Context, conn *websocket.Conn, id int, method string, params map[string]string) (string, error) {
	if err := wsjson.Write(ctx, conn, map[string]any{
		"id":     id,
		"method": method,
		"params": params,
	}); err != nil {
		return "", fmt.Errorf("write CDP command %s: %w", method, err)
	}
	for {
		var reply struct {
			ID     int `json:"id"`
			Result struct {
				TargetID string `json:"targetId"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := wsjson.Read(ctx, conn, &reply); err != nil {
			return "", fmt.Errorf("read CDP reply for %s: %w", method, err)
		}
		if reply.ID == id {
			if reply.Error != nil {
				return "", fmt.Errorf("%s: %s", method, reply.Error.Message)
			}
			return reply.Result.TargetID, nil
		}
	}
}

func observations(ctx context.Context, t *testing.T, fixture, key string) []string {
	t.Helper()
	var events []string
	readDevJSON(ctx, t, fixture+"/observations?key="+url.QueryEscape(key), &events)
	return events
}

func waitForWBATab(ctx context.Context, t *testing.T, fixture, targetID string, wantReady bool) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status struct {
			Ready   *bool `json:"ready"`
			Retired bool  `json:"retired"`
		}
		readDevJSON(ctx, t, fixture+"/readiness?targetId="+url.QueryEscape(targetID), &status)
		if status.Ready == nil {
			t.Fatal("fixture did not report a readiness value")
		}
		// A retired target proves the detach reached the fixture even if
		// the readiness lease could have expired during the test.
		if *status.Ready == wantReady && (wantReady || status.Retired) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-deadline.C:
			t.Fatalf("WBA tab %s did not reach ready=%t, retired=%t", targetID, wantReady, !wantReady)
		case <-ticker.C:
		}
	}
}
