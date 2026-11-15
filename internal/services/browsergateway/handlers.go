package browsergateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
)

// Tabs serves the current page targets and the primary tab ID.
func (b *Browser) Tabs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := b.tabs(ctx)
	writeResult(w, result, err)
}

// Command serves a typed Stagehand operation.
func (b *Browser) Command(w http.ResponseWriter, r *http.Request) {
	var command browserapi.Command
	if !readCommand(w, r, &command) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	result, err := b.command(ctx, command)
	writeResult(w, result, err)
}

// CDP executes one command on the browser or a selected page.
func (b *Browser) CDP(w http.ResponseWriter, r *http.Request) {
	var command browserapi.CDPCommand
	if !readCommand(w, r, &command) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	result, err := b.cdp(ctx, command)
	writeResult(w, result, err)
}

// Screenshot serves a PNG of the selected page.
func (b *Browser) Screenshot(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	result, err := b.cdp(ctx, browserapi.CDPCommand{
		Tab:    r.URL.Query().Get("tab"),
		Method: "Page.captureScreenshot",
	})
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	var image struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &image); err != nil {
		writeResult(w, nil, err)
		return
	}
	png, err := base64.StdEncoding.DecodeString(image.Data)
	if err != nil || len(png) == 0 {
		writeResult(w, nil, errors.New("Chromium returned an invalid screenshot"))
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png)
}

func readCommand(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		http.Error(w, "invalid browser request: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "expected a single browser request", http.StatusBadRequest)
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, result any, err error) {
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, errUnknownTab):
			status = http.StatusNotFound
		case errors.Is(err, errInvalidCommand):
			status = http.StatusBadRequest
		case errors.Is(err, context.DeadlineExceeded):
			status = http.StatusGatewayTimeout
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
