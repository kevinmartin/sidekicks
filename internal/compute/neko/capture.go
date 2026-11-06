package neko

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"time"
)

// Capture is a fresh upstream X11 screenshot, bounded in source pixels, output
// pixels and encoded bytes. This is independent of WebRTC decoded-frame timing.
type Capture struct {
	JPEG                                     []byte
	SourceWidth, SourceHeight, Width, Height int
}

// Capture returns a bounded capture from Neko.
func (c *Client) Capture(ctx context.Context, maxWidth, maxHeight int) (Capture, error) {
	if maxWidth < 1 || maxHeight < 1 || maxWidth > 1920 || maxHeight > 1080 {
		return Capture{}, errors.New("neko: capture bounds must be within 1920x1080")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	c.mu.Lock()
	token, ready := c.token, c.ready
	c.mu.Unlock()
	if !ready || token == "" {
		return Capture{}, errors.New("neko: controller disconnected")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.String()+"/api/room/screen/shot.jpg?quality=90", nil)
	if err != nil {
		return Capture{}, errors.New("neko: invalid capture request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return Capture{}, errors.New("neko: capture request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Capture{}, errors.New("neko: capture unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil || len(raw) > 4*1024*1024 {
		return Capture{}, errors.New("neko: capture exceeds 4 mib")
	}
	meta, err := jpeg.DecodeConfig(bytes.NewReader(raw))
	if err != nil || !validSize(meta.Width, meta.Height) {
		return Capture{}, errors.New("neko: invalid or oversized source jpeg")
	}
	src, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return Capture{}, errors.New("neko: invalid source jpeg")
	}
	width, height := meta.Width, meta.Height
	if width > maxWidth {
		height = height * maxWidth / width
		width = maxWidth
		if height < 1 {
			height = 1
		}
	}
	if height > maxHeight {
		width = width * maxHeight / height
		height = maxHeight
		if width < 1 {
			width = 1
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dst.Set(x, y, src.At(x*meta.Width/width, y*meta.Height/height))
		}
	}
	var out bytes.Buffer
	if jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}) != nil || out.Len() > 4*1024*1024 {
		return Capture{}, errors.New("neko: output capture exceeds limit")
	}
	return Capture{JPEG: out.Bytes(), SourceWidth: meta.Width, SourceHeight: meta.Height, Width: width, Height: height}, nil
}
