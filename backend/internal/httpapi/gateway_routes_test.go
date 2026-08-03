package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sharesub/sharesub/backend/internal/application"
)

func TestGatewayCompatibilityRoutesAreRegistered(t *testing.T) {
	server := New(&application.Service{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/models"},
		{http.MethodPost, "/v1/responses"},
		{http.MethodPost, "/v1/responses/compact"},
		{http.MethodPost, "/responses"},
		{http.MethodPost, "/responses/compact"},
		{http.MethodPost, "/backend-api/codex/responses"},
		{http.MethodPost, "/backend-api/codex/responses/compact"},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(`{"model":"gpt-5.4"}`))
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body = %s", recorder.Code, recorder.Body.String())
			}
			var body struct {
				Error struct {
					Type string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Type != "authentication_error" {
				t.Fatalf("error type = %q", body.Error.Type)
			}
		})
	}
}

func TestShouldSwitchUpstreamAccountOnlyForExplicitRejection(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, 529} {
		if !shouldSwitchUpstreamAccount(status) {
			t.Fatalf("status %d should switch account", status)
		}
	}
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
		if shouldSwitchUpstreamAccount(status) {
			t.Fatalf("status %d must not switch account", status)
		}
	}
}
