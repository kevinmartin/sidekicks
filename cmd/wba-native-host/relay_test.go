package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRelayRejectsInvalidNativeMessages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    uint32
		body    string
		wantErr string
	}{
		{
			name:    "zero length",
			wantErr: "invalid native message size",
		},
		{
			name:    "oversized frame",
			size:    maxMessageSize + 1,
			wantErr: "invalid native message size",
		},
		{
			name:    "truncated frame",
			size:    3,
			body:    "{}",
			wantErr: "read native message",
		},
		{
			name:    "invalid JSON",
			size:    1,
			body:    "{",
			wantErr: "invalid native message",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := binary.NativeEndian.AppendUint32(nil, tc.size)
			message = append(message, tc.body...)
			var output bytes.Buffer

			err := relay(t.Context(), bytes.NewReader(message), &output, brokerConfig{})

			require.ErrorContains(t, err, tc.wantErr)
			require.Empty(t, output.Bytes())
		})
	}
}

func TestRelayBrokerResponse(t *testing.T) {
	const (
		request   = `{"type":"configure"}`
		testLimit = 5 * time.Second
	)

	for _, tc := range []struct {
		name    string
		reply   string
		wantErr string
	}{
		{
			name:  "valid reply",
			reply: `{"ok":true}`,
		},
		{
			name:  "largest valid reply",
			reply: `"` + strings.Repeat("a", maxMessageSize-2) + `"`,
		},
		{
			name:    "invalid JSON reply",
			reply:   "{",
			wantErr: "invalid broker response",
		},
		{
			name:    "oversized JSON reply",
			reply:   `"` + strings.Repeat("a", maxMessageSize-1) + `"`,
			wantErr: "invalid broker response",
		},
		{
			name:    "broker withholds reply",
			wantErr: "read broker response",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), testLimit)
			defer cancel()

			listener := listenForTestBroker(t)
			require.NoError(t, listener.SetDeadline(time.Now().Add(testLimit)))
			cfg := brokerConfig{
				Socket:  listener.Addr().String(),
				Timeout: testLimit,
			}
			if tc.reply == "" {
				cfg.Timeout = time.Second
			}
			message := binary.NativeEndian.AppendUint32(nil, uint32(len(request)))
			message = append(message, request...)
			var output bytes.Buffer
			result := make(chan error, 1)
			go func() {
				defer close(result)
				defer func() {
					if panicValue := recover(); panicValue != nil {
						result <- fmt.Errorf("native relay panic: %v\n%s", panicValue, debug.Stack())
					}
				}()
				result <- relay(ctx, bytes.NewReader(message), &output, cfg)
			}()
			t.Cleanup(func() {
				cancel()
				if err := <-result; err != nil {
					t.Logf("relay ended during cleanup: %v", err)
				}
			})

			peer, err := listener.AcceptUnix()
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = peer.Close()
			})
			require.NoError(t, peer.SetDeadline(time.Now().Add(testLimit)))
			received, err := io.ReadAll(peer)
			require.NoError(t, err)
			require.Equal(t, request, string(received))
			if tc.reply != "" {
				_, err := io.WriteString(peer, tc.reply)
				require.NoError(t, err)
				require.NoError(t, peer.CloseWrite())
			}

			select {
			case err = <-result:
			case <-ctx.Done():
				t.Fatalf("wait for native relay: %v", ctx.Err())
			}
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Empty(t, output.Bytes())
				return
			}
			require.NoError(t, err)
			want := binary.NativeEndian.AppendUint32(nil, uint32(len(tc.reply)))
			want = append(want, tc.reply...)
			require.Equal(t, want, output.Bytes())
		})
	}
}

func listenForTestBroker(t *testing.T) *net.UnixListener {
	t.Helper()
	// Keep the pathname short enough for the Unix socket address on macOS.
	dir, err := os.MkdirTemp("", "wba-")
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove test broker socket directory: %v", err)
		}
	})
	listener, err := net.ListenUnix("unix", &net.UnixAddr{
		Name: filepath.Join(dir, "broker.sock"),
		Net:  "unix",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = listener.Close()
	})
	return listener
}
