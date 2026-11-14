package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObserverChallengesUnauthorizedRequests(t *testing.T) {
	f := &fixture{
		config: config{
			Token: "test-token",
		},
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/origins", nil)

	f.observer().ServeHTTP(response, request)

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Equal(t, `Bearer realm="sidekicks-fixture"`, response.Header().Get("WWW-Authenticate"))
}
