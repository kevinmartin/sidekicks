package browserviewer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
	"github.com/stretchr/testify/require"
)

func TestViewerStreamRequiresLocalOriginAndCredential(t *testing.T) {
	const host = "127.0.0.1:12345"
	viewer, err := New(host, func(_ context.Context) (*websocket.Conn, error) {
		t.Error("unauthorized request reached the guest")
		return nil, errors.New("unexpected viewer dial")
	})
	require.NoError(t, err)
	_, token, _ := strings.Cut(viewer.URL(), "#")
	for _, fixture := range []struct {
		name, host, origin, token string
	}{
		{
			name:   "wrong host",
			host:   "outside.test",
			origin: "http://" + host,
			token:  token,
		},
		{
			name:   "wrong origin port",
			host:   host,
			origin: "http://127.0.0.1:9999",
			token:  token,
		},
		{
			name:  "missing origin",
			host:  host,
			token: token,
		},
		{
			name:   "wrong credential",
			host:   host,
			origin: "http://" + host,
			token:  "unrelated",
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://"+fixture.host+"/stream", nil)
			request.Header.Set("Origin", fixture.origin)
			request.Header.Set("Sec-WebSocket-Protocol", browserapi.ViewerProtocol+", "+fixture.token)
			response := httptest.NewRecorder()
			viewer.Stream(response, request)
			require.Equal(t, http.StatusForbidden, response.Code)
		})
	}
}
