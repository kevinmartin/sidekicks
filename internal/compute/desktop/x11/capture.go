package x11

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math/bits"
)

// MaxCaptureBytes limits the size of an encoded X11 capture.
const MaxCaptureBytes = 16 << 20

// CapturePNG returns a fresh root-window PNG without shell commands, temp files,
// clipboard access or Python. It excludes native hardware cursor composition.
func (d *Driver) CapturePNG(ctx context.Context) ([]byte, error) {
	var data []byte
	err := d.operation(ctx, false, func(backend Backend) error {
		raw, err := backend.Image()
		if err != nil {
			return err
		}
		img, err := decodeImage(raw)
		if err != nil {
			return err
		}
		out := &boundedBuffer{limit: MaxCaptureBytes}
		encoder := png.Encoder{CompressionLevel: png.BestSpeed}
		if err := encoder.Encode(out, img); err != nil {
			return err
		}
		data = bytes.Clone(out.Bytes())
		return nil
	})
	return data, err
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

// Write writes data from the supplied buffer.
func (b *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.Len() {
		return 0, errors.New("png exceeds capture byte limit")
	}
	return b.Buffer.Write(data)
}

func decodeImage(raw RawImage) (*image.RGBA, error) {
	if raw.Width < 1 || raw.Height < 1 || raw.Width > MaxPixels || raw.Height > MaxPixels || raw.Width > MaxPixels/raw.Height {
		return nil, errors.New("invalid image dimensions")
	}
	if raw.BitsPerPixel != 16 && raw.BitsPerPixel != 24 && raw.BitsPerPixel != 32 {
		return nil, errors.New("unsupported x11 pixel format")
	}
	if raw.ScanlinePad != 8 && raw.ScanlinePad != 16 && raw.ScanlinePad != 32 {
		return nil, errors.New("unsupported x11 scanline padding")
	}
	if raw.ByteOrder > 1 {
		return nil, errors.New("unsupported x11 byte order")
	}
	if raw.RedMask == 0 || raw.GreenMask == 0 || raw.BlueMask == 0 || raw.RedMask&raw.GreenMask != 0 || raw.RedMask&raw.BlueMask != 0 || raw.GreenMask&raw.BlueMask != 0 {
		return nil, errors.New("invalid x11 color masks")
	}
	mask := raw.RedMask | raw.GreenMask | raw.BlueMask
	if raw.BitsPerPixel < 32 && mask>>raw.BitsPerPixel != 0 {
		return nil, errors.New("x11 masks exceed pixel size")
	}
	stride := ((raw.Width*int(raw.BitsPerPixel) + int(raw.ScanlinePad) - 1) / int(raw.ScanlinePad)) * int(raw.ScanlinePad) / 8
	expected := stride * raw.Height
	if len(raw.Data) != ((expected+3)/4)*4 {
		return nil, errors.New("x11 image payload length mismatch")
	}
	channel := func(value, mask uint32) (byte, error) {
		shift := bits.TrailingZeros32(mask)
		maximum := mask >> uint(shift)
		if maximum&(maximum+1) != 0 {
			return 0, errors.New("non-contiguous x11 color mask")
		}
		return byte((uint64((value&mask)>>uint(shift))*255 + uint64(maximum)/2) / uint64(maximum)), nil
	}
	img := image.NewRGBA(image.Rect(0, 0, raw.Width, raw.Height))
	bytesPerPixel := int(raw.BitsPerPixel) / 8
	for y := 0; y < raw.Height; y++ {
		for x := 0; x < raw.Width; x++ {
			offset := y*stride + x*bytesPerPixel
			var value uint32
			for index := 0; index < bytesPerPixel; index++ {
				position := index
				if raw.ByteOrder == 1 {
					position = bytesPerPixel - index - 1
				}
				value |= uint32(raw.Data[offset+index]) << uint(position*8)
			}
			red, err := channel(value, raw.RedMask)
			if err != nil {
				return nil, err
			}
			green, err := channel(value, raw.GreenMask)
			if err != nil {
				return nil, err
			}
			blue, err := channel(value, raw.BlueMask)
			if err != nil {
				return nil, err
			}
			img.SetRGBA(x, y, color.RGBA{R: red, G: green, B: blue, A: 255})
		}
	}
	return img, nil
}
