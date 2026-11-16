package browsergateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	stagehand "github.com/browserbase/stagehand/packages/sdk-go/v4"
	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
)

var (
	errUnknownTab     = errors.New("tab unavailable; use browser tabs to select a live tab")
	errInvalidCommand = errors.New("invalid browser command")
)

// Browser controls Chromium and its Stagehand extension for the life of the guest.
type Browser struct {
	endpoint     string
	primary      string
	mu           sync.Mutex
	localBrowser *stagehand.Browser
	stagehand    *stagehandSession
	staleWorker  stagehandWorker
}

// OpenBrowser waits for Chromium and initializes the primary tab.
func (g *Gateway) OpenBrowser(ctx context.Context) (*Browser, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var endpoint string
	for {
		var err error
		endpoint, err = g.browserSocket(ctx)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for Chromium CDP: %w (%v)", ctx.Err(), err)
		case <-ticker.C:
		}
	}
	connection, err := openCDP(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer connection.close()
	// The guest launches Chromium with about:blank. Discovery can become
	// available before Chromium registers that page target.
	for {
		tabs, err := connection.pageTargets(ctx)
		if err != nil {
			return nil, fmt.Errorf("find Chromium startup tab: %w", err)
		}
		for _, tab := range tabs {
			if tab.URL == "about:blank" && tab.ID != "" {
				return &Browser{
					endpoint: endpoint,
					primary:  tab.ID,
				}, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for Chromium startup tab: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (b *Browser) tabs(ctx context.Context) (browserapi.Tabs, error) {
	connection, err := openCDP(ctx, b.endpoint)
	if err != nil {
		return browserapi.Tabs{}, err
	}
	defer connection.close()
	tabs, err := connection.pageTargets(ctx)
	if err != nil {
		return browserapi.Tabs{}, err
	}
	return browserapi.Tabs{
		Primary: b.primary,
		Tabs:    tabs,
	}, nil
}

func (c *cdpConnection) pageTargets(ctx context.Context) ([]browserapi.Tab, error) {
	targets, err := c.targets(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]browserapi.Tab, 0, len(targets))
	for _, target := range targets {
		if target.Type == "page" {
			result = append(result, browserapi.Tab{
				ID:    target.ID,
				Title: target.Title,
				URL:   target.URL,
			})
		}
	}
	return result, nil
}

func (b *Browser) tabID(tab string) string {
	if tab == "" {
		return b.primary
	}
	return tab
}

func (b *Browser) page(ctx context.Context, session *stagehandSession, tab string) (*stagehand.Page, error) {
	pages, err := session.pages.Pages(ctx)
	if err != nil {
		return nil, err
	}
	id := b.tabID(tab)
	for _, page := range pages {
		if page.PageID() == id {
			return page, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", errUnknownTab, id)
}

func (b *Browser) command(ctx context.Context, command browserapi.Command) (any, error) {
	session, err := b.stagehandForCommand(ctx)
	if err != nil {
		return nil, err
	}
	result, err := b.runCommand(ctx, session, command)
	if stagehandDisconnected(err) {
		b.discardStagehand(session)
	}
	return result, err
}

func (b *Browser) runCommand(ctx context.Context, session *stagehandSession, command browserapi.Command) (any, error) {
	page, err := b.page(ctx, session, command.Tab)
	if err != nil {
		return nil, err
	}
	result := browserapi.CommandResult{
		Tab: page.PageID(),
	}
	switch command.Operation {
	case "goto":
		if strings.TrimSpace(command.URL) == "" {
			return nil, fmt.Errorf("%w: goto requires a URL", errInvalidCommand)
		}
		response, err := page.Goto(ctx, command.URL, nil)
		if err != nil {
			return nil, err
		}
		result.URL, err = page.URL(ctx)
		if err != nil {
			return nil, err
		}
		if response != nil {
			result.Status = new(response.Status())
		}
	case "snapshot":
		snapshot, err := page.Snapshot(ctx, nil)
		if err != nil {
			return nil, err
		}
		return struct {
			Tab string `json:"tab"`
			stagehand.SnapshotResult
		}{
			Tab:            page.PageID(),
			SnapshotResult: snapshot,
		}, nil
	case "click", "type":
		if strings.TrimSpace(command.Selector) == "" {
			return nil, fmt.Errorf("%w: %s requires a selector", errInvalidCommand, command.Operation)
		}
		locator := page.Locator(command.Selector)
		if command.Operation == "click" {
			err = locator.Click(ctx, nil)
		} else {
			err = locator.Fill(ctx, command.Text)
		}
	default:
		return nil, fmt.Errorf("%w: unknown operation %q", errInvalidCommand, command.Operation)
	}
	return result, err
}

func (b *Browser) cdp(ctx context.Context, command browserapi.CDPCommand) (json.RawMessage, error) {
	if domain, method, ok := strings.Cut(command.Method, "."); !ok || domain == "" || method == "" {
		return nil, fmt.Errorf("%w: specify a CDP method as Domain.method", errInvalidCommand)
	}
	if command.Browser && command.Tab != "" {
		return nil, fmt.Errorf("%w: choose a tab or browser scope", errInvalidCommand)
	}
	if len(command.Params) != 0 && command.Params[0] != '{' {
		return nil, fmt.Errorf("%w: CDP parameters must be a JSON object", errInvalidCommand)
	}
	connection, err := openCDP(ctx, b.endpoint)
	if err != nil {
		return nil, err
	}
	defer connection.close()
	var session struct {
		ID string `json:"sessionId"`
	}
	if !command.Browser && (command.Tab != "" || !browserScope(command.Method)) {
		id := b.tabID(command.Tab)
		if err := connection.call(ctx, "", "Target.attachToTarget", map[string]any{
			"targetId": id,
			"flatten":  true,
		}, &session); err != nil {
			var protocolErr *cdpError
			if errors.As(err, &protocolErr) && protocolErr.Message == "No target with given id found" {
				return nil, fmt.Errorf("%w: %q", errUnknownTab, id)
			}
			return nil, fmt.Errorf("attach to tab %q: %w", id, err)
		}
	}
	var params any
	if len(command.Params) > 0 {
		params = command.Params
	}
	var result json.RawMessage
	if err := connection.call(ctx, session.ID, command.Method, params, &result); err != nil {
		var protocolErr *cdpError
		if session.ID != "" && errors.As(err, &protocolErr) && strings.HasSuffix(protocolErr.Message, "is only supported on the browser target") {
			return nil, fmt.Errorf("%w: %s requires browser scope. Use --browser without --tab", errInvalidCommand, command.Method)
		}
		return nil, err
	}
	return result, nil
}

func browserScope(method string) bool {
	prefix, _, _ := strings.Cut(method, ".")
	return prefix == "Browser" || prefix == "Target" || prefix == "SystemInfo"
}
