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
	"sync"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/openai"
	"github.com/sharesub/sharesub/backend/internal/security"
)

type gatewayTestRoundTripFunc func(*http.Request) (*http.Response, error)

func (f gatewayTestRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type gatewayTestBlockingBody struct {
	reader  io.Reader
	started chan<- time.Time
	proceed <-chan struct{}
	once    sync.Once
}

func (b *gatewayTestBlockingBody) Read(buffer []byte) (int, error) {
	b.once.Do(func() {
		b.started <- time.Now()
		<-b.proceed
	})
	return b.reader.Read(buffer)
}

func (*gatewayTestBlockingBody) Close() error { return nil }

type gatewayHandlerStore struct {
	application.Store

	credential domain.GatewayCredential
	metricErr  error

	mu              sync.Mutex
	calls           []string
	metric          domain.GatewayMetric
	metrics         []domain.GatewayMetric
	quotaSignals    []domain.QuotaSignal
	quotaPlanID     string
	quotaAccountID  string
	quotaGeneration int64
	quotaRecordedAt time.Time
}

func (s *gatewayHandlerStore) ResolveGatewayRoutes(context.Context, []byte, time.Time) (domain.GatewayRouteSet, error) {
	return domain.GatewayRouteSet{
		APIKey:     domain.APIKey{ID: s.credential.APIKeyID, Strategy: domain.RoutePriority},
		Candidates: []domain.GatewayCredential{s.credential},
	}, nil
}

func (*gatewayHandlerStore) AccountQuotaExhausted(context.Context, string, time.Time) (bool, error) {
	return false, nil
}

func (*gatewayHandlerStore) MemberQuotaExhausted(context.Context, string, string, string, int64, int, time.Time) (bool, error) {
	return false, nil
}

func (*gatewayHandlerStore) TouchAPIKey(context.Context, string, time.Time) error { return nil }

func (s *gatewayHandlerStore) RecordGatewayMetric(_ context.Context, metric domain.GatewayMetric) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "metric")
	s.metric = metric
	s.metrics = append(s.metrics, metric)
	return s.metricErr
}

func (s *gatewayHandlerStore) RecordAccountQuotaSignals(_ context.Context, planID, accountID string, generation int64, signals []domain.QuotaSignal, recordedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "quota")
	s.quotaPlanID = planID
	s.quotaAccountID = accountID
	s.quotaGeneration = generation
	s.quotaSignals = append([]domain.QuotaSignal(nil), signals...)
	s.quotaRecordedAt = recordedAt
	return nil
}

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
		{http.MethodGet, "/v1/responses"},
		{http.MethodGet, "/responses"},
		{http.MethodGet, "/backend-api/codex/responses"},
		{http.MethodPost, "/v1/alpha/search"},
		{http.MethodPost, "/alpha/search"},
		{http.MethodPost, "/backend-api/codex/alpha/search"},
		{http.MethodPost, "/v1/images/generations"},
		{http.MethodPost, "/v1/images/edits"},
		{http.MethodPost, "/images/generations"},
		{http.MethodPost, "/images/edits"},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(`{"model":"gpt-5.4"}`))
			if test.method == http.MethodGet && strings.Contains(test.path, "responses") {
				request.Header.Set("Connection", "Upgrade")
				request.Header.Set("Upgrade", "websocket")
			}
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

func TestGatewayBodyLimitsMatchEndpointCapabilities(t *testing.T) {
	if maxGatewayBody != 256<<20 {
		t.Fatalf("general gateway body limit = %d, want 256 MiB", maxGatewayBody)
	}
	if maxTextGatewayBody != 32<<20 {
		t.Fatalf("text gateway body limit = %d, want 32 MiB", maxTextGatewayBody)
	}
	if gatewayBodyTooLargeMessage != "request body exceeds 256 MiB" {
		t.Fatalf("general gateway body limit message = %q", gatewayBodyTooLargeMessage)
	}
	if textGatewayBodyTooLargeMessage != "request body exceeds 32 MiB" {
		t.Fatalf("text gateway body limit message = %q", textGatewayBodyTooLargeMessage)
	}
}

func TestGatewayContextsHandleClientCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	metricCtx, cancelMetric := metricContext(parent)
	defer cancelMetric()
	attemptCtx, cancelAttempt, acceptUpstream := upstreamAttemptContext(parent)
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
			t.Fatalf("upstream context error = %v, want client cancellation", attemptCtx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("upstream context did not follow cancellation before response acceptance")
	}
	if acceptUpstream() {
		t.Fatal("accepted upstream after client cancellation")
	}
}

func TestGatewayAttemptDetachesClientCancellationAfterResponseAcceptance(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	attemptCtx, cancelAttempt, acceptUpstream := upstreamAttemptContext(parent)
	defer cancelAttempt()
	if !acceptUpstream() {
		t.Fatal("did not accept upstream before client cancellation")
	}
	cancelParent()

	select {
	case <-attemptCtx.Done():
		t.Fatalf("accepted upstream context canceled with client: %v", attemptCtx.Err())
	case <-time.After(10 * time.Millisecond):
	}
	deadline, ok := attemptCtx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > gatewayUpstreamTimeout {
		t.Fatalf("upstream deadline = %v, ok = %t", deadline, ok)
	}
	cancelAttempt()
	select {
	case <-attemptCtx.Done():
		if !errors.Is(attemptCtx.Err(), context.Canceled) {
			t.Fatalf("upstream context error = %v, want explicit cancellation", attemptCtx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("upstream context did not honor explicit cancellation")
	}
}

func TestSuccessfulGatewayResponseRecordsMetricBeforeQuotaWithFrozenTimes(t *testing.T) {
	tests := []struct {
		name      string
		metricErr error
		wantCalls []string
	}{
		{name: "metric success records quota", wantCalls: []string{"metric", "quota"}},
		{name: "metric failure skips quota", metricErr: errors.New("write metric"), wantCalls: []string{"metric"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, err := security.New(make([]byte, 32), make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}
			credential := domain.GatewayCredential{
				APIKeyID:                 "key",
				Member:                   domain.Member{ID: "member", UserID: "user", ShareBasisPoints: 10_000},
				Plan:                     domain.Plan{ID: "plan", AllocationMode: domain.AllocationFixed},
				Account:                  domain.Account{ID: "account", OwnerUserID: "owner", ChatGPTAccountID: "chatgpt"},
				TokenExpiresAt:           time.Now().Add(time.Hour),
				AccountBindingGeneration: 7,
			}
			credential.AccessTokenCiphertext, err = manager.Encrypt("access", []byte("owner:chatgpt:access"))
			if err != nil {
				t.Fatal(err)
			}
			store := &gatewayHandlerStore{credential: credential, metricErr: test.metricErr}
			headersReturned := make(chan time.Time, 1)
			bodyStarted := make(chan time.Time, 1)
			proceed := make(chan struct{})
			client := &http.Client{Transport: gatewayTestRoundTripFunc(func(*http.Request) (*http.Response, error) {
				headersReturned <- time.Now()
				responseHeaders := make(http.Header)
				responseHeaders.Set("Content-Type", "text/event-stream")
				responseHeaders.Set("X-Request-Id", "upstream-request")
				responseHeaders.Set("X-Codex-Primary-Used-Percent", "25")
				responseHeaders.Set("X-Codex-Primary-Reset-After-Seconds", "600")
				responseHeaders.Set("X-Codex-Primary-Window-Minutes", "300")
				body := `data: {"type":"response.completed","response":{"id":"resp","object":"response","status":"completed","model":"gpt-5.4","output":[],"usage":{"input_tokens":2,"output_tokens":1}}}` + "\n\n"
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     responseHeaders,
					Body: &gatewayTestBlockingBody{
						reader: strings.NewReader(body), started: bodyStarted, proceed: proceed,
					},
				}, nil
			})}
			service := application.NewService(store, manager, nil, 0, "", "")
			server := New(service, openai.NewGateway(client), slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"hi","stream":false}`))
			request.Header.Set("Authorization", "Bearer sk-sharesub-test")
			recorder := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				server.Handler().ServeHTTP(recorder, request)
				close(done)
			}()

			headerAt := waitGatewayTestTime(t, headersReturned, "upstream headers")
			bodyAt := waitGatewayTestTime(t, bodyStarted, "upstream body read")
			close(proceed)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("gateway request did not finish")
			}
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}

			store.mu.Lock()
			calls := append([]string(nil), store.calls...)
			metric := store.metric
			quotaSignals := append([]domain.QuotaSignal(nil), store.quotaSignals...)
			quotaPlanID := store.quotaPlanID
			quotaAccountID := store.quotaAccountID
			quotaGeneration := store.quotaGeneration
			quotaRecordedAt := store.quotaRecordedAt
			store.mu.Unlock()
			if strings.Join(calls, ",") != strings.Join(test.wantCalls, ",") {
				t.Fatalf("store calls = %v, want %v", calls, test.wantCalls)
			}
			if metric.RequestID != "upstream-request" {
				t.Fatalf("metric request id = %q, want upstream request id", metric.RequestID)
			}
			if metric.CreatedAt.After(headerAt) {
				t.Fatalf("metric time = %s, after upstream headers at %s", metric.CreatedAt, headerAt)
			}
			if test.metricErr != nil {
				if len(quotaSignals) != 0 || !quotaRecordedAt.IsZero() {
					t.Fatalf("quota snapshot was recorded after metric failure: %+v at %s", quotaSignals, quotaRecordedAt)
				}
				return
			}
			if quotaPlanID != "plan" || quotaAccountID != "account" || quotaGeneration != 7 || len(quotaSignals) != 1 {
				t.Fatalf("quota snapshot binding = %q/%q/%d, signals = %+v", quotaPlanID, quotaAccountID, quotaGeneration, quotaSignals)
			}
			if quotaRecordedAt.Before(headerAt) || quotaRecordedAt.After(bodyAt) {
				t.Fatalf("quota observation = %s, want between headers %s and body read %s", quotaRecordedAt, headerAt, bodyAt)
			}
			wantResetAt := quotaRecordedAt.Truncate(time.Second).Add(10 * time.Minute)
			if !quotaSignals[0].ResetAt.Equal(wantResetAt) {
				t.Fatalf("quota reset = %s, want observation-derived %s", quotaSignals[0].ResetAt, wantResetAt)
			}
		})
	}
}

func TestResponsesRetriesRequestScopedCapacityOnSameAccountWithoutKeyBackoff(t *testing.T) {
	manager, err := security.New(make([]byte, 32), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	credential := domain.GatewayCredential{
		APIKeyID: "key", Member: domain.Member{ID: "member", UserID: "user", ShareBasisPoints: 10_000},
		Plan:           domain.Plan{ID: "plan", AllocationMode: domain.AllocationFixed},
		Account:        domain.Account{ID: "account", OwnerUserID: "owner", ChatGPTAccountID: "chatgpt"},
		TokenExpiresAt: time.Now().Add(time.Hour), AccountBindingGeneration: 1,
	}
	credential.AccessTokenCiphertext, err = manager.Encrypt("access", []byte("owner:chatgpt:access"))
	if err != nil {
		t.Fatal(err)
	}
	store := &gatewayHandlerStore{credential: credential}
	var calls int
	client := &http.Client{Transport: gatewayTestRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		headers := http.Header{"Content-Type": []string{"text/event-stream"}, "Retry-After": []string{"5"}}
		if calls <= maxRequestScopedCapacityRetries {
			body := `data: {"type":"response.failed","response":{"id":"resp_capacity","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}` + "\n\n"
			return &http.Response{StatusCode: http.StatusOK, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
		body := `data: {"type":"response.completed","response":{"id":"resp_ok","model":"gpt-5.4","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":2,"output_tokens":1}}}` + "\n\n"
		return &http.Response{StatusCode: http.StatusOK, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	service := application.NewService(store, manager, nil, 0, "", "")
	server := New(service, openai.NewGateway(client), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.requestScopedRetryDelay = func(int) time.Duration { return 0 }
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"hi","stream":false}`))
	request.Header.Set("Authorization", "Bearer sk-sharesub-test")
	request.Header.Set("X-Request-Id", "gateway-request")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || calls != maxRequestScopedCapacityRetries+1 || !strings.Contains(recorder.Body.String(), `"id":"resp_ok"`) {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, calls, recorder.Body.String())
	}
	server.protections.mu.Lock()
	_, backedOff := server.protections.keyBackoffs[credential.APIKeyID]
	server.protections.mu.Unlock()
	if backedOff {
		t.Fatal("request-scoped capacity failure backed off the API key")
	}
	store.mu.Lock()
	metrics := append([]domain.GatewayMetric(nil), store.metrics...)
	store.mu.Unlock()
	if len(metrics) != maxRequestScopedCapacityRetries+1 {
		t.Fatalf("metric count = %d, want %d", len(metrics), maxRequestScopedCapacityRetries+1)
	}
	requestIDs := make(map[string]struct{}, len(metrics))
	for index, metric := range metrics {
		if _, duplicate := requestIDs[metric.RequestID]; duplicate {
			t.Fatalf("metric %d reused request id %q: %+v", index, metric.RequestID, metrics)
		}
		requestIDs[metric.RequestID] = struct{}{}
	}
	if metrics[0].RequestID != "gateway-request" {
		t.Fatalf("first metric request id = %q", metrics[0].RequestID)
	}
	finalMetric := metrics[len(metrics)-1]
	if finalMetric.StatusCode != http.StatusOK || finalMetric.TokenUsage.InputTokens != 2 || finalMetric.TokenUsage.OutputTokens != 1 {
		t.Fatalf("final success metric = %+v", finalMetric)
	}
}

func waitGatewayTestTime(t *testing.T, values <-chan time.Time, name string) time.Time {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
		return time.Time{}
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

func TestGatewayResponseClientDisconnectedRequiresIncompleteDelivery(t *testing.T) {
	tests := []struct {
		name    string
		metrics openai.ProxyMetrics
		err     error
		want    bool
	}{
		{name: "context canceled before terminal delivery", err: context.Canceled, want: true},
		{name: "write failed before terminal delivery", metrics: openai.ProxyMetrics{ClientDisconnected: true}, want: true},
		{name: "context canceled after terminal delivery", metrics: openai.ProxyMetrics{ResponseDelivered: true}, err: context.Canceled},
		{name: "terminal delivered before later write failure", metrics: openai.ProxyMetrics{ResponseDelivered: true, ClientDisconnected: true}},
		{name: "active request", metrics: openai.ProxyMetrics{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := gatewayResponseClientDisconnected(test.metrics, test.err); got != test.want {
				t.Fatalf("gatewayResponseClientDisconnected() = %v, want %v", got, test.want)
			}
		})
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

func TestGatewayMetricStatusUsesBadGatewayForCopyFailure(t *testing.T) {
	status := gatewayMetricStatus(http.StatusOK, openai.ProxyMetrics{}, errors.New("read upstream response"))
	if status != http.StatusBadGateway {
		t.Fatalf("metric status = %d", status)
	}
}
