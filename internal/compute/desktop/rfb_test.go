package desktop

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func readyParser(t *testing.T) *ClientFrames {
	t.Helper()
	p := &ClientFrames{}
	frames, err := p.Feed([]byte("RFB 003.008\n\x01\x01"))
	if err != nil || len(frames) != 3 {
		t.Fatalf("handshake: %v %+v", err, frames)
	}
	for _, f := range frames {
		if f.Mutating {
			t.Fatal("handshake must be read-only")
		}
	}
	return p
}

// TestRFBFragmentedInput verifies rfb fragmented input.
func TestRFBFragmentedInput(t *testing.T) {
	key := []byte{4, 1, 0, 0, 0, 0, 0xff, 0xe1}
	p := readyParser(t)
	for _, b := range key[:len(key)-1] {
		frames, err := p.Feed([]byte{b})
		if err != nil || len(frames) != 0 {
			t.Fatalf("partial key escaped: %v %+v", err, frames)
		}
	}
	frames, err := p.Feed(key[len(key)-1:])
	if err != nil || len(frames) != 1 || !frames[0].Mutating || !bytes.Equal(frames[0].Data, key) {
		t.Fatalf("key: %v %+v", err, frames)
	}
}

// TestRFBCoalescedMessagesAndIndependentCopies verifies rfb coalesced messages and independent copies.
func TestRFBCoalescedMessagesAndIndependentCopies(t *testing.T) {
	request := []byte{3, 1, 0, 0, 0, 0, 5, 0, 2, 208}
	pointer := []byte{5, 1, 0, 10, 0, 20}
	data := append(bytes.Clone(request), pointer...)
	frames, err := readyParser(t).Feed(data)
	if err != nil || len(frames) != 2 || frames[0].Mutating || !frames[1].Mutating {
		t.Fatalf("frames: %v %+v", err, frames)
	}
	for i := range data {
		data[i] = 0
	}
	if !bytes.Equal(frames[0].Data, request) || !bytes.Equal(frames[1].Data, pointer) {
		t.Fatal("parser retained caller buffer")
	}
}

// TestRFBMessageClassification verifies rfb message classification.
func TestRFBMessageClassification(t *testing.T) {
	tests := []struct {
		name     string
		packet   []byte
		mutating bool
	}{
		{"pixel-format", make([]byte, 20), false},
		{"encodings", []byte{2, 0, 0, 1, 0, 0, 0, 7}, false},
		{"framebuffer", []byte{3, 1, 0, 0, 0, 0, 0, 1, 0, 1}, false},
		{"key", []byte{4, 1, 0, 0, 0, 0, 0, 65}, true},
		{"pointer", []byte{5, 0, 0, 3, 0, 4}, true},
		{"clipboard", []byte{6, 0, 0, 0, 0, 0, 0, 3, 'a', 'b', 'c'}, true},
		{"extended-clipboard", []byte{6, 0, 0, 0, 255, 255, 255, 253, 'a', 'b', 'c'}, true},
		{"continuous", append([]byte{150}, make([]byte, 9)...), false},
		{"fence", []byte{248, 0, 0, 0, 0, 0, 0, 0, 2, 1, 2}, false},
		{"power", []byte{250, 0, 1, 2}, true},
		{"resize", append([]byte{251, 0, 5, 0, 2, 208, 1, 0}, make([]byte, 16)...), true},
		{"extended-key", append([]byte{255, 0}, make([]byte, 10)...), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := readyParser(t)
			for _, b := range test.packet[:len(test.packet)-1] {
				frames, err := p.Feed([]byte{b})
				if err != nil || len(frames) != 0 {
					t.Fatalf("partial: %v %+v", err, frames)
				}
			}
			frames, err := p.Feed(test.packet[len(test.packet)-1:])
			if err != nil || len(frames) != 1 || frames[0].Mutating != test.mutating || !bytes.Equal(frames[0].Data, test.packet) {
				t.Fatalf("classification: %v %+v", err, frames)
			}
		})
	}
}

// TestRFBHandshakeFailsClosedAndPoisonsParser verifies rfb handshake fails closed and poisons parser.
func TestRFBHandshakeFailsClosedAndPoisonsParser(t *testing.T) {
	for _, bad := range [][]byte{[]byte("RFB 003.003\n"), []byte("RFB 003.008\n\x02"), []byte("RFB 003.008\n\x01\x00")} {
		p := &ClientFrames{}
		if f, err := p.Feed(bad); !errors.Is(err, ErrProtocol) || len(f) != 0 {
			t.Fatalf("accepted %q: %+v %v", bad, f, err)
		}
		if _, err := p.Feed([]byte("RFB 003.008\n\x01\x01")); !errors.Is(err, ErrProtocol) {
			t.Fatal("failed parser recovered without reconnect")
		}
	}
}

// TestRFBUnknownAndOversizedMessages verifies rfb unknown and oversized messages.
func TestRFBUnknownAndOversizedMessages(t *testing.T) {
	for _, packet := range [][]byte{{249}, {255, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, {6, 0, 0, 0, 0, 32, 0, 0}, {6, 0, 0, 0, 128, 0, 0, 0}} {
		p := readyParser(t)
		if frames, err := p.Feed(packet); !errors.Is(err, ErrProtocol) || len(frames) != 0 {
			t.Fatalf("accepted %x: %v", packet, err)
		}
	}
	p := &ClientFrames{}
	if _, err := p.Feed(make([]byte, MaxClientMessage+1)); !errors.Is(err, ErrProtocol) {
		t.Fatal("unbounded input")
	}
	p = readyParser(t)
	packet := make([]byte, MaxClientMessage)
	packet[0] = 6
	binary.BigEndian.PutUint32(packet[4:8], MaxClientMessage-8)
	frames, err := p.Feed(packet)
	if err != nil || len(frames) != 1 || len(frames[0].Data) != MaxClientMessage {
		t.Fatalf("exact bound: %v", err)
	}
}

// TestRFBFailingCoalescedFeedReturnsNoEarlierFrames verifies rfb failing coalesced feed returns no earlier frames.
func TestRFBFailingCoalescedFeedReturnsNoEarlierFrames(t *testing.T) {
	p := readyParser(t)
	frames, err := p.Feed([]byte{5, 1, 0, 1, 0, 1, 249})
	if !errors.Is(err, ErrProtocol) || len(frames) != 0 {
		t.Fatalf("partial forward on invalid chunk: %+v %v", frames, err)
	}
}

// FuzzRFBParser exercises rfb parser with generated inputs.
func FuzzRFBParser(f *testing.F) {
	f.Add([]byte("RFB 003.008\n\x01\x01\x05\x01\x00\x01\x00\x02"))
	f.Add([]byte("RFB 003.008\n\x01\x01\x06\x00\x00\x00\x80\x00\x00\x00"))
	f.Add([]byte{255, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		p := ClientFrames{}
		for len(data) > 0 {
			n := min(17, len(data))
			frames, err := p.Feed(data[:n])
			data = data[n:]
			for _, frame := range frames {
				if len(frame.Data) == 0 || len(frame.Data) > MaxClientMessage {
					t.Fatal("invalid frame bound")
				}
			}
			if len(p.buf) > MaxClientMessage {
				t.Fatal("buffer exceeded bound")
			}
			if err != nil {
				if len(frames) != 0 {
					t.Fatal("frames returned on failure")
				}
				return
			}
		}
	})
}
