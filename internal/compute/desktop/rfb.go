// Package desktop contains the Sidekicks-owned desktop protocol boundary.
// It does not implement an RFB server or replace the upstream noVNC client.
package desktop

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// MaxClientMessage bounds both an individual RFB message and buffered fragments.
const MaxClientMessage = 1 << 20

// ErrProtocol indicates an unsupported or invalid protocol message.
var ErrProtocol = errors.New("invalid or unsupported rfb client stream")

// Frame is one complete client message. Mutating frames must be authorized at
// the final upstream write, not merely when a WebSocket is opened.
type Frame struct {
	Mutating bool
	Data     []byte
}

// ClientFrames recognizes only RFB 3.8, loopback None authentication and shared
// desktop clients. Its zero value is ready for use. Feed must have one caller.
// Any protocol failure permanently poisons the parser; reconnect to retry.
type ClientFrames struct {
	buf    []byte
	stage  int
	failed bool
}

func (p *ClientFrames) fail(reason string) ([]Frame, error) {
	p.failed = true
	p.buf = nil
	return nil, fmt.Errorf("%w: %s", ErrProtocol, reason)
}

// Feed accepts fragmented or coalesced RFB bytes. It returns independent copies,
// so the caller may reuse its input. No frames from a failing Feed are returned.
func (p *ClientFrames) Feed(data []byte) ([]Frame, error) {
	if p.failed {
		return nil, ErrProtocol
	}
	if len(data) > MaxClientMessage-len(p.buf) {
		return p.fail("client buffer exceeds limit")
	}
	p.buf = append(p.buf, data...)
	var frames []Frame
	for len(p.buf) > 0 {
		if p.stage < 3 {
			expected := [][]byte{[]byte("RFB 003.008\n"), {1}, {1}}[p.stage]
			if len(p.buf) < len(expected) {
				break
			}
			if !bytes.Equal(p.buf[:len(expected)], expected) {
				return p.fail("rfb 3.8 shared desktop with no authentication required")
			}
			frames = append(frames, Frame{Data: bytes.Clone(p.buf[:len(expected)])})
			p.buf = p.buf[len(expected):]
			p.stage++
			continue
		}
		typ := p.buf[0]
		n := 0
		mutating := false
		switch typ {
		case 0:
			n = 20 // The message is SetPixelFormat.
		case 2: // The message is SetEncodings.
			if len(p.buf) < 4 {
				return frames, nil
			}
			n = 4 + 4*int(binary.BigEndian.Uint16(p.buf[2:4]))
		case 3:
			n = 10 // The message is FramebufferUpdateRequest.
		case 4:
			n = 8
			mutating = true // The message is KeyEvent.
		case 5:
			n = 6
			mutating = true // The message is PointerEvent.
		case 6: // The message is ClientCutText, including the extended clipboard length.
			if len(p.buf) < 8 {
				return frames, nil
			}
			size := int64(int32(binary.BigEndian.Uint32(p.buf[4:8])))
			if size < 0 {
				size = -size
			}
			if size > MaxClientMessage-8 {
				return p.fail("clipboard exceeds limit")
			}
			n = 8 + int(size)
			mutating = true
		case 150:
			n = 10 // The message is EnableContinuousUpdates.
		case 248: // The message is Fence.
			if len(p.buf) < 9 {
				return frames, nil
			}
			n = 9 + int(p.buf[8])
		case 250:
			n = 4
			mutating = true // The message controls XVP power.
		case 251: // The message is SetDesktopSize.
			if len(p.buf) < 8 {
				return frames, nil
			}
			n = 8 + 16*int(p.buf[6])
			mutating = true
		case 255:
			n = 12
			mutating = true // The message is a QEMU extended key event of subtype zero.
		default:
			return p.fail("unknown message type")
		}
		if n > MaxClientMessage {
			return p.fail("message exceeds limit")
		}
		if len(p.buf) < n {
			break
		}
		if typ == 255 && p.buf[1] != 0 {
			return p.fail("unknown extended key subtype")
		}
		frames = append(frames, Frame{Mutating: mutating, Data: bytes.Clone(p.buf[:n])})
		p.buf = p.buf[n:]
	}
	if len(p.buf) == 0 {
		p.buf = nil
	}
	return frames, nil
}
