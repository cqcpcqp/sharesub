package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/buildinfo"
)

func TestVersionEndpointReturnsBuildIdentity(t *testing.T) {
	server := New(&application.Service{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
	}
	var fields map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	want := buildinfo.Current()
	if len(fields) != 2 || fields["version"] != want.Version || fields["revision"] != want.Revision {
		t.Fatalf("body = %+v, want exactly version=%q and revision=%q", fields, want.Version, want.Revision)
	}
}

func TestHealthEndpointContractRemainsUnchanged(t *testing.T) {
	server := New(&application.Service{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("health response = status %d body %q", recorder.Code, recorder.Body.String())
	}
}
