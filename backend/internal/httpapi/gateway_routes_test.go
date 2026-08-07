package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/openai"
)

func TestGatewayCompatibilityRoutesAreRegistered(t *testing.T) {
	server := New(&application.Service{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/models"},
		{http.MethodGet, "/models"},
		{http.MethodGet, "/models?client_version=0.137.0"},
		{http.MethodGet, "/backend-api/codex/models"},
		{http.MethodPost, "/v1/responses"},
		{http.MethodPost, "/v1/responses/compact"},
		{http.MethodPost, "/responses"},
		{http.MethodPost, "/responses/compact"},
		{http.MethodPost, "/backend-api/codex/responses"},
		{http.MethodPost, "/backend-api/codex/responses/compact"},
		{http.MethodPost, "/v1/images/generations"},
		{http.MethodPost, "/v1/images/edits"},
		{http.MethodPost, "/images/generations"},
		{http.MethodPost, "/images/edits"},
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

func TestGatewayContextsHandleClientCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	metricCtx, cancelMetric := metricContext(parent)
	defer cancelMetric()
	attemptCtx, cancelAttempt := upstreamAttemptContext(parent)
	defer cancelAttempt()
	cancelParent()

	select {
	case <-metricCtx.Done():
		t.Fatalf("metric context canceled with client: %v", metricCtx.Err())
	case <-time.After(10 * time.Millisecond):
	}
	select {
	case <-attemptCtx.Done():
		if !errors.Is(attemptCtx.Err(), context.Canceled) {
			t.Fatalf("upstream context error = %v, want context canceled", attemptCtx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("upstream context did not inherit client cancellation")
	}
}

func TestShouldSwitchUpstreamAccountForRetryableFailure(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusHTTPVersionNotSupported, 529} {
		if !shouldSwitchUpstreamAccount(status) {
			t.Fatalf("status %d should switch account", status)
		}
	}
	for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusNotFound, http.StatusRequestTimeout} {
		if shouldSwitchUpstreamAccount(status) {
			t.Fatalf("status %d must not switch account", status)
		}
	}
}

func TestShouldSwitchModelsAccountForRetryableFailure(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
		if !shouldSwitchModelsAccount(status) {
			t.Fatalf("status %d should switch models account", status)
		}
	}
	for _, status := range []int{http.StatusOK, http.StatusNotModified, http.StatusBadRequest, http.StatusForbidden, 600} {
		if shouldSwitchModelsAccount(status) {
			t.Fatalf("status %d must not switch models account", status)
		}
	}
}

func TestGatewayMetricCarriesStructuredErrorContext(t *testing.T) {
	metric := gatewayMetric("request-1", "gpt-5.6-sol", "/v1/responses", openai.RequestBilling{
		Model: "gpt-5.6-sol", ServiceTier: "priority", Stream: true,
	}, http.StatusServiceUnavailable, openai.ProxyMetrics{
		Duration: 3 * time.Second, ErrorCode: "server_error", ErrorMessage: "upstream temporarily unavailable",
	})
	if metric.Endpoint != "/v1/responses" || !metric.IsStream || metric.ErrorSource != domain.GatewayErrorSourceUpstream || metric.ErrorCode != "server_error" || metric.ErrorMessage != "upstream temporarily unavailable" {
		t.Fatalf("gateway metric = %+v", metric)
	}
}

func TestGatewayClientDisconnectMetricUsesRequestErrorStatus(t *testing.T) {
	metric := gatewayErrorMetric("request-1", "/v1/responses", "gpt-5.6-sol", openai.RequestBilling{
		Model: "gpt-5.6-sol", Stream: true,
	}, clientClosedRequestStatus, domain.GatewayErrorSourceRequest, "client_disconnected", "client disconnected before response completed", time.Second)

	if metric.StatusCode != 499 || metric.ErrorSource != domain.GatewayErrorSourceRequest || metric.ErrorCode != "client_disconnected" {
		t.Fatalf("client disconnect metric = %+v", metric)
	}
}

func TestGatewayMetricStatusPreservesUpstreamHTTPError(t *testing.T) {
	status := gatewayMetricStatus(http.StatusNotFound, openai.ProxyMetrics{ErrorStatusCode: http.StatusBadGateway}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("metric status = %d", status)
	}
}

func TestGatewayMetricStatusUsesTerminalFailureForSuccessfulHTTPResponse(t *testing.T) {
	status := gatewayMetricStatus(http.StatusOK, openai.ProxyMetrics{ErrorStatusCode: http.StatusBadGateway}, nil)
	if status != http.StatusBadGateway {
		t.Fatalf("metric status = %d", status)
	}
}
