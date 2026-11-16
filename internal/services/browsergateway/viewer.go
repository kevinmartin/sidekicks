package browsergateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
)

var errViewerInput = errors.New("invalid viewer input")

type viewerViewport struct {
	Width  float64 `json:"clientWidth"`
	Height float64 `json:"clientHeight"`
}

type viewerMetrics struct {
	Viewport viewerViewport `json:"cssVisualViewport"`
}

// Viewer streams images and accepts input for a single Chromium page target.
func (b *Browser) Viewer(w http.ResponseWriter, r *http.Request) {
	id := b.tabID(r.URL.Query().Get("tab"))
	setup, stopSetup := context.WithTimeout(r.Context(), 5*time.Second)
	defer stopSetup()
	cdp, err := openCDP(setup, b.endpoint)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	defer cdp.close()
	cdp.socket.SetReadLimit(browserapi.ViewerFrameLimit)
	tabs, err := cdp.pageTargets(setup)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	found := false
	for _, tab := range tabs {
		if tab.ID == id {
			found = true
			break
		}
	}
	if !found {
		writeResult(w, nil, fmt.Errorf("%w: %q", errUnknownTab, id))
		return
	}
	session, err := cdp.attachTab(setup, id)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	if err := cdp.call(setup, session, "Page.enable", nil, nil); err != nil {
		writeResult(w, nil, err)
		return
	}
	var metrics viewerMetrics
	if err := cdp.call(setup, session, "Page.getLayoutMetrics", nil, &metrics); err != nil {
		writeResult(w, nil, err)
		return
	}
	stopSetup()
	socket, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() {
		_ = socket.CloseNow()
	}()
	socket.SetReadLimit(browserapi.ViewerInputLimit)
	serveViewer(r.Context(), socket, &viewerCDP{
		connection: cdp,
		session:    session,
		viewport:   metrics.Viewport,
	})
}

func serveViewer(ctx context.Context, socket *websocket.Conn, page *viewerCDP) {
	ctx, cancel := context.WithCancel(ctx)
	messages := make(chan cdpMessage, 1)
	inputs := make(chan browserapi.ViewerInput, 16)
	frames := make(chan browserapi.ViewerFrame, 1)
	failures := make(chan error, 3)
	var workers sync.WaitGroup
	run := func(work func() error) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() {
				if failure := recover(); failure != nil {
					log.Printf("browser viewer worker panicked: %v", failure)
					failures <- errors.New("browser viewer worker failed")
				}
			}()
			failures <- work()
		}()
	}
	defer workers.Wait()
	defer cancel()
	run(func() error {
		for {
			message, err := page.connection.receive(ctx)
			if err != nil {
				return err
			}
			select {
			case messages <- message:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})
	run(func() error {
		for {
			var input browserapi.ViewerInput
			if err := wsjson.Read(ctx, socket, &input); err != nil {
				return err
			}
			select {
			case inputs <- input:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})
	run(func() error {
		for {
			select {
			case frame := <-frames:
				write, stop := context.WithTimeout(ctx, 5*time.Second)
				err := wsjson.Write(write, socket, frame)
				stop()
				if err != nil {
					return err
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})
	err := page.stream(ctx, messages, inputs, frames, failures)
	switch {
	case errors.Is(err, errUnknownTab):
		_ = socket.Close(websocket.StatusPolicyViolation, "selected browser tab is closed")
	case errors.Is(err, errViewerInput):
		_ = socket.Close(websocket.StatusPolicyViolation, "invalid viewer input")
	case ctx.Err() == nil && websocket.CloseStatus(err) == -1:
		_ = socket.Close(websocket.StatusInternalError, "browser viewer disconnected")
	}
}

type viewerCDP struct {
	connection     *cdpConnection
	session        string
	viewport       viewerViewport
	screenshot     int
	metrics        int
	lastScreencast time.Time
	lastJPEG       string
}

func (v *viewerCDP) send(ctx context.Context, method string, params any) (int, error) {
	write, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return v.connection.send(write, v.session, method, params)
}

func (v *viewerCDP) stream(
	ctx context.Context,
	messages <-chan cdpMessage,
	inputs <-chan browserapi.ViewerInput,
	frames chan browserapi.ViewerFrame,
	failures <-chan error,
) error {
	_, err := v.send(ctx, "Page.startScreencast", map[string]any{
		"format":    "jpeg",
		"quality":   75,
		"maxWidth":  1600,
		"maxHeight": 1000,
	})
	if err != nil {
		return err
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-failures:
			return err
		case message := <-messages:
			if err := v.receive(ctx, message, frames); err != nil {
				return err
			}
		case event := <-inputs:
			if err := v.input(ctx, event); err != nil {
				return err
			}
		case <-ticker.C:
			// Headless Chromium stops sending screencast frames for a hidden
			// page. A surface screenshot keeps that same page viewable.
			if v.screenshot == 0 && time.Since(v.lastScreencast) >= 600*time.Millisecond {
				v.screenshot, err = v.send(ctx, "Page.captureScreenshot", map[string]any{
					"format":      "jpeg",
					"quality":     75,
					"fromSurface": true,
				})
				if err != nil {
					return err
				}
			}
		}
	}
}

func (v *viewerCDP) receive(ctx context.Context, message cdpMessage, frames chan browserapi.ViewerFrame) error {
	if message.Method == "Target.detachedFromTarget" {
		var detached struct {
			Session string `json:"sessionId"`
		}
		if err := json.Unmarshal(message.Params, &detached); err != nil {
			return err
		}
		if detached.Session == v.session {
			return errUnknownTab
		}
	}
	if message.Error != nil {
		switch message.Error.Message {
		case "Session with given id not found", "Target closed":
			return errUnknownTab
		case "Not attached to an active page":
			// Chromium can swap the renderer during navigation.
			if message.ID == v.screenshot {
				v.screenshot = 0
			}
			if message.ID == v.metrics {
				v.metrics = 0
			}
			return nil
		}
		return message.Error
	}
	switch {
	case message.ID != 0 && message.ID == v.screenshot:
		v.screenshot = 0
		var screenshot struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(message.Result, &screenshot); err != nil {
			return err
		}
		v.publish(frames, screenshot.Data)
	case message.ID != 0 && message.ID == v.metrics:
		v.metrics = 0
		var metrics viewerMetrics
		if err := json.Unmarshal(message.Result, &metrics); err != nil {
			return err
		}
		v.viewport = metrics.Viewport
	case message.Session == v.session && message.Method == "Page.screencastFrame":
		var frame struct {
			Data      string `json:"data"`
			SessionID int    `json:"sessionId"`
		}
		if err := json.Unmarshal(message.Params, &frame); err != nil {
			return err
		}
		_, err := v.send(ctx, "Page.screencastFrameAck", map[string]int{
			"sessionId": frame.SessionID,
		})
		if err != nil {
			return err
		}
		v.lastScreencast = time.Now()
		v.publish(frames, frame.Data)
	case message.Session == v.session && message.Method == "Page.frameResized" && v.metrics == 0:
		var err error
		v.metrics, err = v.send(ctx, "Page.getLayoutMetrics", nil)
		return err
	}
	return nil
}

func (v *viewerCDP) publish(frames chan browserapi.ViewerFrame, jpeg string) {
	if jpeg == "" || jpeg == v.lastJPEG {
		return
	}
	v.lastJPEG = jpeg
	// Keep the latest image while a slow viewer is still reading.
	select {
	case <-frames:
	default:
	}
	frames <- browserapi.ViewerFrame{
		JPEG: jpeg,
	}
}
