package webbotauth

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestPathAndQuery(t *testing.T) {
	for _, tc := range []struct{ url, path, query string }{
		{"https://example.com", "/", "?"},
		{"https://example.com/path?", "/path", "?"},
		{"https://example.com/a%2Fb?foo=bar&baz=bat%2Dman", "/a%2Fb", "?foo=bar&baz=bat%2Dman"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tc.url, nil)
			require.NoError(t, err)
			msg := &SignerMessage{derivedComponents: make(map[string]signerMessageComponent)}
			WithPath(false)(msg, req)
			WithQuery(false)(msg, req)
			assert.Equal(t, tc.path, msg.derivedComponents["@path"].value, "@path")
			assert.Equal(t, tc.query, msg.derivedComponents["@query"].value, "@query")
		})
	}
}
