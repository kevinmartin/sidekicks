package webbotauth

import (
	"net/http"
	"testing"
)

func TestRequestPathAndQuery(t *testing.T) {
	for _, tc := range []struct{ url, path, query string }{
		{"https://example.com", "/", "?"},
		{"https://example.com/path?", "/path", "?"},
		{"https://example.com/a%2Fb?foo=bar&baz=bat%2Dman", "/a%2Fb", "?foo=bar&baz=bat%2Dman"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tc.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			msg := &SignerMessage{derivedComponents: make(map[string]signerMessageComponent)}
			WithPath(false)(msg, req)
			WithQuery(false)(msg, req)
			if got := msg.derivedComponents["@path"].value; got != tc.path {
				t.Errorf("path = %q, want %q", got, tc.path)
			}
			if got := msg.derivedComponents["@query"].value; got != tc.query {
				t.Errorf("query = %q, want %q", got, tc.query)
			}
		})
	}
}
