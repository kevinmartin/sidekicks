package main

import (
	"io"
	"net"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBrokerReportsDecodeFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request string
		wantErr string
	}{
		{
			name:    "invalid JSON",
			request: `{"id":@}`,
			wantErr: "decode broker request",
		},
		{
			name:    "truncated by read limit",
			request: `{"id":"` + strings.Repeat("a", 32<<10) + `"}`,
			wantErr: "decode broker request",
		},
		{
			name:    "missing ID",
			request: `{"type":"configure"}`,
		},
		{
			name: "empty connection",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := &fixture{
					config: config{
						BrokerTimeout: time.Second,
					},
				}
				client, server := net.Pipe()
				t.Cleanup(func() {
					_ = client.Close()
				})
				ctx := t.Context()
				result := make(chan error, 1)
				go func() {
					result <- f.serveBroker(ctx, server)
				}()

				// Let the broker set its deadline and start reading before closing the pipe.
				synctest.Wait()
				require.NoError(t, client.SetWriteDeadline(time.Now().Add(time.Second)))
				if tc.request != "" {
					_, err := io.WriteString(client, tc.request)
					if tc.wantErr == "" {
						require.NoError(t, err)
					}
				}
				_ = client.Close()
				err := <-result
				if tc.wantErr != "" {
					require.ErrorContains(t, err, tc.wantErr)
					return
				}
				require.NoError(t, err)
			})
		})
	}
}
