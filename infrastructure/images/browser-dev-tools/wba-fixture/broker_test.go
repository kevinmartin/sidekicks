package main

import (
	"fmt"
	"io"
	"net"
	"runtime/debug"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBrokerReportsDecodeFailures(t *testing.T) {
	type brokerResult struct {
		err      error
		panicErr error
	}

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
				require.NoError(t, client.SetWriteDeadline(time.Now().Add(time.Second)))
				ctx := t.Context()
				result := make(chan brokerResult, 1)
				go func() {
					var outcome brokerResult
					defer func() {
						if panicValue := recover(); panicValue != nil {
							outcome.panicErr = fmt.Errorf("broker panic: %v\n%s", panicValue, debug.Stack())
						}
						result <- outcome
					}()
					outcome.err = f.serveBroker(ctx, server)
				}()

				// Let the broker start reading before sending or closing the pipe.
				synctest.Wait()
				var writeErr error
				if tc.request != "" {
					_, writeErr = io.WriteString(client, tc.request)
				}
				_ = client.Close()
				outcome := <-result
				require.NoError(t, outcome.panicErr)
				if tc.wantErr != "" {
					require.ErrorContains(t, outcome.err, tc.wantErr)
					return
				}
				require.NoError(t, writeErr)
				require.NoError(t, outcome.err)
			})
		})
	}
}
