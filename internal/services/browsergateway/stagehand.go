package browsergateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	stagehand "github.com/browserbase/stagehand/packages/sdk-go/v4"
)

// StagehandExtensionID identifies the Stagehand extension at its fixed image path.
const StagehandExtensionID = "mibgiokgnjiohdeccdpmccjlfpdoliim"

const stagehandWorkerURL = "chrome-extension://" + StagehandExtensionID + "/service-worker.js"
const stagehandWorkerPollInterval = 50 * time.Millisecond

type stagehandBrowser struct {
	sdk       *stagehand.Browser
	transport *stagehandTransport
}

type stagehandSession struct {
	client *stagehand.Stagehand
	pages  *stagehand.BrowserContext
	worker stagehandWorker
}

type stagehandWorker struct {
	targetID  string
	isolateID string
}

func stagehandDisconnected(err error) bool {
	return errors.Is(err, stagehand.ErrCDPConnectionClosed) || errors.Is(err, stagehand.ErrCDPClientClosed)
}

func stagehandSessionLost(err error) bool {
	if stagehandDisconnected(err) || stagehandAttachmentLost(err) {
		return true
	}
	// A replacement worker can retain the CDP session ID while losing its
	// Stagehand session. These errors come from the worker's session lookup.
	var rpcErr *stagehand.RPCError
	return errors.As(err, &rpcErr) &&
		(rpcErr.Message == "Stagehand loopback CDP is not configured" ||
			rpcErr.Message == "Stagehand loopback CDP is disconnected")
}

func (b *Browser) stagehandForCommand(ctx context.Context) (*stagehandSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stagehand != nil {
		return b.stagehand, nil
	}
	if b.staleWorker.targetID != "" {
		if err := b.stopStagehandWorker(ctx, b.staleWorker); err != nil {
			return nil, fmt.Errorf("recover Stagehand worker: %w", err)
		}
		b.staleWorker = stagehandWorker{}
	}
	ticker := time.NewTicker(stagehandWorkerPollInterval)
	defer ticker.Stop()
	for {
		session, err := b.initializeStagehand(ctx)
		if err == nil {
			b.stagehand = session
			return session, nil
		}
		if !stagehandAttachmentLost(err) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("reattach to Stagehand worker: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (b *Browser) initializeStagehand(ctx context.Context) (*stagehandSession, error) {
	// Acquire the worker on the first typed command so external SDK clients
	// can still use a fresh guest. A worker RPC rejection releases the claim
	// but leaves the SDK's browser connection available for another attempt.
	if b.localBrowser == nil || b.localBrowser.sdk.Closed() {
		b.closeStagehandBrowser()
		transport, err := newStagehandTransport(ctx, b.endpoint)
		if err != nil {
			return nil, fmt.Errorf("open Stagehand transport: %w", err)
		}
		browser, err := stagehand.ConnectLocalBrowser(ctx, stagehand.LocalBrowserConnectOptions{
			CDPURL:          transport.endpoint,
			ExtensionCDPURL: b.endpoint,
		})
		if err != nil {
			transport.close()
			return nil, fmt.Errorf("connect Stagehand: %w", err)
		}
		b.localBrowser = &stagehandBrowser{
			sdk:       browser,
			transport: transport,
		}
	}
	worker, err := b.stagehandWorker(ctx)
	if err != nil {
		return nil, fmt.Errorf("find Stagehand worker: %w", err)
	}
	client, err := stagehand.Create(ctx, stagehand.CreateOptions{
		Browser: b.localBrowser.sdk,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Stagehand: %w", err)
	}
	pages, err := b.localBrowser.sdk.Context()
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		return nil, errors.Join(fmt.Errorf("get Stagehand browser context: %w", err), client.Close(cleanup))
	}
	return &stagehandSession{
		client: client,
		pages:  pages,
		worker: worker,
	}, nil
}

func stagehandAttachmentLost(err error) bool {
	if err == nil {
		return false
	}
	// The pinned SDK keeps CDP protocol errors private. A lost target or
	// session here means Chromium rejected the call before dispatching it.
	message := err.Error()
	return strings.HasSuffix(message, "CDP command failed: Target.attachToTarget: No target with given id found") ||
		strings.HasSuffix(message, "CDP command failed: Runtime.evaluate: Session with given id not found")
}

func (b *Browser) discardStagehand(session *stagehandSession) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stagehand != session {
		return
	}
	// Retire the worker we owned before attaching again. Closing the
	// transport lets the SDK drain any buffered replies before it shuts down.
	b.stagehand = nil
	b.staleWorker = session.worker
	b.closeStagehandBrowser()
}

func (b *Browser) closeStagehandBrowser() {
	if b.localBrowser != nil {
		b.localBrowser.transport.close()
		b.localBrowser = nil
	}
}

// Close releases the Stagehand client.
func (b *Browser) Close(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer b.closeStagehandBrowser()
	if b.stagehand == nil {
		return nil
	}
	return b.stagehand.client.Close(ctx)
}

func (b *Browser) stagehandWorker(ctx context.Context) (stagehandWorker, error) {
	connection, err := openCDP(ctx, b.endpoint)
	if err != nil {
		return stagehandWorker{}, err
	}
	defer connection.close()
	ticker := time.NewTicker(stagehandWorkerPollInterval)
	defer ticker.Stop()
	for {
		worker, err := connection.stagehandWorker(ctx)
		if err != nil {
			return stagehandWorker{}, err
		}
		if worker.targetID != "" {
			return worker, nil
		}
		select {
		case <-ctx.Done():
			return stagehandWorker{}, fmt.Errorf("wait for Stagehand service worker target: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (b *Browser) stopStagehandWorker(ctx context.Context, worker stagehandWorker) error {
	connection, err := openCDP(ctx, b.endpoint)
	if err != nil {
		return err
	}
	defer connection.close()
	ticker := time.NewTicker(stagehandWorkerPollInterval)
	defer ticker.Stop()
	for closeRequested := false; ; closeRequested = true {
		current, err := connection.stagehandWorker(ctx)
		if err != nil {
			return err
		}
		if current != worker {
			return nil
		}
		if !closeRequested {
			var result struct {
				Success bool `json:"success"`
			}
			err := connection.call(ctx, "", "Target.closeTarget", map[string]string{
				"targetId": worker.targetID,
			}, &result)
			var protocolErr *cdpError
			if errors.As(err, &protocolErr) && protocolErr.Message == "No target with given id found" {
				return nil
			}
			if err != nil {
				return err
			}
			if !result.Success {
				return errors.New("browser refused to stop the Stagehand worker")
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *cdpConnection) stagehandWorker(ctx context.Context) (stagehandWorker, error) {
	targets, err := c.targets(ctx)
	if err != nil {
		return stagehandWorker{}, err
	}
	for _, target := range targets {
		if target.Type == "service_worker" && target.URL == stagehandWorkerURL {
			return c.stagehandWorkerIdentity(ctx, target.ID)
		}
	}
	return stagehandWorker{}, nil
}

func (c *cdpConnection) stagehandWorkerIdentity(ctx context.Context, targetID string) (stagehandWorker, error) {
	var session struct {
		ID string `json:"sessionId"`
	}
	err := c.call(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": targetID,
		"flatten":  true,
	}, &session)
	var protocolErr *cdpError
	if errors.As(err, &protocolErr) && protocolErr.Message == "No target with given id found" {
		return stagehandWorker{}, nil
	}
	if err != nil {
		return stagehandWorker{}, err
	}
	// Chromium may reuse the target ID after restarting a service worker.
	// The isolate ID distinguishes that replacement from the abandoned worker.
	var isolate struct {
		ID string `json:"id"`
	}
	err = c.call(ctx, session.ID, "Runtime.getIsolateId", nil, &isolate)
	if errors.As(err, &protocolErr) && protocolErr.Message == "Session with given id not found" {
		return stagehandWorker{}, nil
	}
	if err != nil {
		return stagehandWorker{}, err
	}
	if isolate.ID == "" {
		return stagehandWorker{}, errors.New("missing Stagehand worker isolate ID")
	}
	return stagehandWorker{
		targetID:  targetID,
		isolateID: isolate.ID,
	}, nil
}
