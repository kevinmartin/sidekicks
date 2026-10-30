package x11

import (
	"image/color"
	"testing"
)

// TestDecodePixelFormatsAndPadding verifies decode pixel formats and padding.
func TestDecodePixelFormatsAndPadding(t *testing.T) {
	tests := []RawImage{
		{Width: 1, Height: 1, Depth: 24, BitsPerPixel: 32, ScanlinePad: 32, RedMask: 0xff0000, GreenMask: 0xff00, BlueMask: 0xff, Data: []byte{3, 2, 1, 0}},
		{Width: 1, Height: 1, Depth: 24, BitsPerPixel: 32, ScanlinePad: 32, ByteOrder: 1, RedMask: 0xff0000, GreenMask: 0xff00, BlueMask: 0xff, Data: []byte{0, 1, 2, 3}},
		{Width: 1, Height: 1, Depth: 24, BitsPerPixel: 24, ScanlinePad: 8, RedMask: 0xff0000, GreenMask: 0xff00, BlueMask: 0xff, Data: []byte{3, 2, 1, 0}},
	}
	for _, raw := range tests {
		img, err := decodeImage(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := img.RGBAAt(0, 0); got != (color.RGBA{R: 1, G: 2, B: 3, A: 255}) {
			t.Fatalf("pixel: %+v", got)
		}
	}
	raw := RawImage{Width: 1, Height: 1, Depth: 16, BitsPerPixel: 16, ScanlinePad: 32, RedMask: 0xf800, GreenMask: 0x7e0, BlueMask: 0x1f, Data: []byte{0, 248, 0, 0}}
	img, err := decodeImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if img.RGBAAt(0, 0) != (color.RGBA{R: 255, A: 255}) {
		t.Fatal("RGB565 scaling wrong")
	}
}

// TestDecodeRejectsMalformedOrUnsupportedPixels verifies decode rejects malformed or unsupported pixels.
func TestDecodeRejectsMalformedOrUnsupportedPixels(t *testing.T) {
	base := newFakeBackend().image
	mutations := []func(*RawImage){
		func(r *RawImage) {
			r.Width = 0
		}, func(r *RawImage) {
			r.Height = 0
		}, func(r *RawImage) {
			r.Width = MaxPixels
			r.Height = 2
		},
		func(r *RawImage) {
			r.BitsPerPixel = 8
		}, func(r *RawImage) {
			r.ScanlinePad = 64
		}, func(r *RawImage) {
			r.ByteOrder = 2
		},
		func(r *RawImage) {
			r.RedMask = 0
		}, func(r *RawImage) {
			r.GreenMask = r.RedMask
		}, func(r *RawImage) {
			r.BitsPerPixel = 16
		},
		func(r *RawImage) {
			r.Data = nil
		}, func(r *RawImage) {
			r.RedMask = 0xa00000
		},
	}
	for i, mutate := range mutations {
		raw := base
		mutate(&raw)
		if _, err := decodeImage(raw); err == nil {
			t.Errorf("malformed format %d accepted", i)
		}
	}
	b := &boundedBuffer{limit: 3}
	if _, err := b.Write([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if n, err := b.Write([]byte{4}); err == nil || n != 0 || b.Len() != 3 {
		t.Fatal("capture byte bound failed")
	}
}
