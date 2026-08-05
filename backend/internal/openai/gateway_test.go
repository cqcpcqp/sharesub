package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestProbeQuotaParsesSignalsBefore429Status(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		headers := make(http.Header)
		headers.Set("x-codex-primary-used-percent", "18.25")
		headers.Set("x-codex-primary-reset-after-seconds", "600")
		headers.Set("x-codex-primary-window-minutes", "10080")
		headers.Set("x-codex-secondary-used-percent", "2.5")
		headers.Set("x-codex-secondary-reset-after-seconds", "120")
		headers.Set("x-codex-secondary-window-minutes", "300")
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     headers,
			Body:       io.NopCloser(strings.NewReader("rate limited")),
			Request:    req,
		}, nil
	})}
	startedAt := time.Now()

	signals, err := NewGateway(client).ProbeQuota(context.Background(), "access-token", "account-id", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 2 {
		t.Fatalf("got %d signals, want 2", len(signals))
	}
	if signals[0].WindowType != domain.Window7D || signals[0].AccountUsedMicros != 18_250_000 {
		t.Fatalf("primary signal = %#v", signals[0])
	}
	if signals[1].WindowType != domain.Window5H || signals[1].AccountUsedMicros != 2_500_000 {
		t.Fatalf("secondary signal = %#v", signals[1])
	}
	if got := signals[0].ResetAt.Sub(signals[0].WindowStart); got != 7*24*time.Hour {
		t.Fatalf("7d window duration = %v", got)
	}
	if signals[0].ResetAt.Before(startedAt.Add(599*time.Second)) || signals[0].ResetAt.After(time.Now().Add(601*time.Second)) {
		t.Fatalf("7d reset_at = %v", signals[0].ResetAt)
	}
	assertProbeRequest(t, captured)
}

func TestProbeQuotaReturnsStatusErrorWithoutSignals(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("no quota headers")),
					Request:    req,
				}, nil
			})}

			signals, err := NewGateway(client).ProbeQuota(context.Background(), "access-token", "account-id", "")
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("status %d", status)) {
				t.Fatalf("error = %v, want status %d", err, status)
			}
			if signals != nil {
				t.Fatalf("signals = %#v, want nil", signals)
			}
		})
	}
}

func assertProbeRequest(t *testing.T, req *http.Request) {
	t.Helper()
	if req == nil {
		t.Fatal("probe request was not captured")
	}
	if req.Method != http.MethodPost || req.URL.String() != codexResponsesURL || req.Host != "chatgpt.com" {
		t.Fatalf("request target = %s %s (Host %q)", req.Method, req.URL, req.Host)
	}
	wantHeaders := map[string]string{
		"Authorization":      "Bearer access-token",
		"Chatgpt-Account-Id": "account-id",
		"Content-Type":       "application/json",
		"Accept":             "text/event-stream",
		"OpenAI-Beta":        "responses=experimental",
		"Originator":         "codex_cli_rs",
		"Version":            codexProbeVersion,
		"User-Agent":         codexProbeUserAgent,
	}
	for key, want := range wantHeaders {
		if got := req.Header.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	deadline, ok := req.Context().Deadline()
	if !ok {
		t.Fatal("probe request context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 14*time.Second || remaining > codexProbeTimeout {
		t.Errorf("probe deadline remaining = %v", remaining)
	}

	var payload struct {
		Model string `json:"model"`
		Input []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"input"`
		Stream bool `json:"stream"`
		Store  bool `json:"store"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != codexProbeModel || !payload.Stream || payload.Store || len(payload.Input) != 1 ||
		payload.Input[0].Role != "user" || len(payload.Input[0].Content) != 1 ||
		payload.Input[0].Content[0].Type != "input_text" || payload.Input[0].Content[0].Text != "hi" {
		t.Fatalf("probe payload = %#v", payload)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestIsolateSessionSeparatesAPIKeys(t *testing.T) {
	first := isolateSession("key-a", "conversation")
	second := isolateSession("key-b", "conversation")
	if first == second {
		t.Fatal("different member API keys produced the same upstream session")
	}
	if first != isolateSession("key-a", "conversation") {
		t.Fatal("session isolation is not stable")
	}
}

func TestParseRequestBilling(t *testing.T) {
	got, err := ParseRequestBilling([]byte(`{"model":"gpt-5.3-codex","service_tier":"priority"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "gpt-5.3-codex" || got.ServiceTier != "priority" {
		t.Fatalf("billing metadata = %+v", got)
	}
	if _, err := ParseRequestBilling([]byte(`{"input":[]}`)); err == nil {
		t.Fatal("request without model was accepted")
	}
}

func TestPrepareRequestNormalizesChatGPTPayload(t *testing.T) {
	body, metadata, err := PrepareRequest([]byte(`{
		"model":"gpt-5.4",
		"stream":false,
		"store":true,
		"prompt_cache_key":"session-a",
		"metadata":{"private":"value"},
		"stream_options":{"include_usage":true},
		"input":[]
	}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Model != "gpt-5.4" || metadata.Stream || metadata.PromptCacheKey != "session-a" {
		t.Fatalf("metadata = %+v", metadata)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["store"] != false || payload["stream"] != true {
		t.Fatalf("normalized flags = store:%v stream:%v", payload["store"], payload["stream"])
	}
	for _, field := range []string{"metadata", "stream_options"} {
		if _, exists := payload[field]; exists {
			t.Fatalf("unsupported field %q was preserved", field)
		}
	}
	if payload["prompt_cache_key"] != "session-a" {
		t.Fatalf("prompt_cache_key = %v", payload["prompt_cache_key"])
	}
}

func TestPrepareCompactRequestKeepsOnlyCompactSchema(t *testing.T) {
	body, metadata, err := PrepareRequest([]byte(`{
		"model":"gpt-5.4",
		"input":[],
		"instructions":"compact",
		"prompt_cache_key":"compact-session",
		"service_tier":"priority",
		"stream":true,
		"store":true,
		"temperature":0.2
	}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.Stream || metadata.ServiceTier != "priority" || metadata.PromptCacheKey != "compact-session" {
		t.Fatalf("metadata = %+v", metadata)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 3 || payload["model"] != "gpt-5.4" || payload["instructions"] != "compact" {
		t.Fatalf("compact payload = %#v", payload)
	}
	if _, exists := payload["prompt_cache_key"]; exists {
		t.Fatal("request-scoped prompt_cache_key was forwarded to compact")
	}
}

func TestPrepareRequestRejectsInvalidStreamType(t *testing.T) {
	if _, _, err := PrepareRequest([]byte(`{"model":"gpt-5.4","stream":"true"}`), false); err == nil {
		t.Fatal("string stream field was accepted")
	}
}

func TestForwardCompactUsesCompactEndpointAndHeaders(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_1"}`)), Request: req}, nil
	})}
	inbound := httptest.NewRequest(http.MethodPost, "http://gateway.test/v1/responses/compact", nil)
	inbound.Header.Set("User-Agent", "Mozilla/5.0")
	metadata := RequestBilling{Model: "gpt-5.4", PromptCacheKey: "compact-session"}
	response, err := NewGateway(client).Forward(context.Background(), inbound, []byte(`{"model":"gpt-5.4"}`), metadata, "access", "account", "key", "")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if captured.URL.String() != codexCompactURL {
		t.Fatalf("target = %q", captured.URL)
	}
	if captured.Header.Get("Accept") != "application/json" || captured.Header.Get("Version") != codexProbeVersion {
		t.Fatalf("compact headers = %#v", captured.Header)
	}
	if captured.Header.Get("User-Agent") != codexProbeUserAgent {
		t.Fatalf("user-agent = %q", captured.Header.Get("User-Agent"))
	}
	if got, want := captured.Header.Get("Session_Id"), isolateSession("key", "compact-session"); got != want {
		t.Fatalf("session_id = %q, want %q", got, want)
	}
}

func TestFetchModelsForwardsCodexDiscoveryHeaders(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "ETag": []string{`"manifest"`}},
			Body:       io.NopCloser(strings.NewReader(`{"models":[{"slug":"gpt-5.5"}]}`)),
			Request:    req,
		}, nil
	})}
	inbound := httptest.NewRequest(http.MethodGet, "http://gateway.test/models?client_version=0.137.0", nil)
	inbound.Header.Set("If-None-Match", `"previous"`)

	response, err := NewGateway(client).FetchModels(context.Background(), inbound, "access", "account", "")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if captured.URL.String() != codexModelsURL+"?client_version=0.137.0" || captured.Host != "chatgpt.com" {
		t.Fatalf("request target = %s (Host %q)", captured.URL, captured.Host)
	}
	wantHeaders := map[string]string{
		"Authorization":      "Bearer access",
		"Chatgpt-Account-Id": "account",
		"Accept":             "application/json",
		"Originator":         "codex_cli_rs",
		"Version":            "0.137.0",
		"User-Agent":         codexProbeUserAgent,
		"If-None-Match":      `"previous"`,
	}
	for key, want := range wantHeaders {
		if got := captured.Header.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestFetchModelsUsesDefaultClientVersion(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{StatusCode: http.StatusNotModified, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
	})}

	response, err := NewGateway(client).FetchModels(context.Background(), httptest.NewRequest(http.MethodGet, "http://gateway.test/backend-api/codex/models", nil), "access", "", "")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if got := captured.URL.Query().Get("client_version"); got != codexProbeVersion {
		t.Fatalf("client_version = %q, want %q", got, codexProbeVersion)
	}
	if got := captured.Header.Get("Chatgpt-Account-Id"); got != "" {
		t.Fatalf("empty account id produced header %q", got)
	}
}

func TestClientForProxyOverridesBaseTransport(t *testing.T) {
	baseTransport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	gateway := NewGateway(&http.Client{Transport: baseTransport})
	client, err := gateway.clientForProxy("http://proxy.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	transport := client.Transport.(*http.Transport)
	request := &http.Request{URL: mustURL(t, "https://chatgpt.com/backend-api/codex/responses")}
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL.String() != "http://proxy.example:8080" {
		t.Fatalf("proxy URL = %q", proxyURL)
	}
	if direct, err := gateway.clientForProxy(""); err != nil || direct != gateway.httpClient {
		t.Fatalf("empty proxy client = %p, %v", direct, err)
	}
}

func TestGatewayConcurrencyLimitReleasesExactlyOnce(t *testing.T) {
	gateway := NewGateway(&http.Client{}, 1)
	release, ok := gateway.TryAcquire()
	if !ok {
		t.Fatal("first gateway slot was rejected")
	}
	if _, ok := gateway.TryAcquire(); ok {
		t.Fatal("gateway accepted more requests than its concurrency limit")
	}
	release()
	release()
	if releaseAgain, ok := gateway.TryAcquire(); !ok {
		t.Fatal("released gateway slot was not reusable")
	} else {
		releaseAgain()
	}
}

func TestProxyClientCacheEvictsOldAndExpiredEntries(t *testing.T) {
	baseTransport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	gateway := NewGateway(&http.Client{Transport: baseTransport})
	now := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	gateway.now = func() time.Time { return now }
	for index := 0; index <= maxProxyClients; index++ {
		if _, err := gateway.clientForProxy(fmt.Sprintf("http://proxy-%d.example:8080", index)); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	if len(gateway.proxyClients) != maxProxyClients {
		t.Fatalf("proxy cache size = %d, want %d", len(gateway.proxyClients), maxProxyClients)
	}
	if gateway.proxyClients["http://proxy-0.example:8080"] != nil {
		t.Fatal("oldest proxy client was not evicted")
	}
	now = now.Add(proxyClientTTL)
	if _, err := gateway.clientForProxy("http://fresh-proxy.example:8080"); err != nil {
		t.Fatal(err)
	}
	if len(gateway.proxyClients) != 1 {
		t.Fatalf("expired proxy clients remain cached: %d", len(gateway.proxyClients))
	}
}

func TestReadLimitedLineRejectsOversizedSSEEvent(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(strings.Repeat("x", maxSSELineBytes+1)))
	if _, err := readLimitedLine(reader); !errors.Is(err, errSSELineTooLarge) {
		t.Fatalf("oversized SSE error = %v", err)
	}
}

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestIsClientOutputEventUsesSemanticOutputBoundary(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{name: "output text delta", line: `data: {"type":"response.output_text.delta","delta":"hi"}`, want: true},
		{name: "completed response", line: `data: {"type":"response.completed"}`, want: true},
		{name: "reasoning delta", line: `data: {"type":"response.reasoning_summary_text.delta","delta":"thinking"}`, want: true},
		{name: "function arguments delta", line: `data: {"type":"response.function_call_arguments.delta","delta":"{}"}`, want: true},
		{name: "output item added", line: `data: {"type":"response.output_item.added"}`, want: true},
		{name: "created preamble", line: `data: {"type":"response.created"}`, want: false},
		{name: "in progress preamble", line: `data: {"type":"response.in_progress"}`, want: false},
		{name: "rate limit metadata", line: `data: {"type":"rate_limits.updated"}`, want: false},
		{name: "failed terminal", line: `data: {"type":"response.failed"}`, want: false},
		{name: "malformed", line: `data: {`, want: false},
		{name: "comment", line: `: keep-alive`, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isClientOutputEvent([]byte(test.line)); got != test.want {
				t.Fatalf("isClientOutputEvent() = %v, want %v", got, test.want)
			}
		})
	}
}

type failingResponseWriter struct {
	header http.Header
	status int
	writes int
}

func (w *failingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *failingResponseWriter) WriteHeader(status int) { w.status = status }
func (w *failingResponseWriter) Write(body []byte) (int, error) {
	w.writes++
	return 0, errors.New("client disconnected")
}

func TestCopyResponseDrainsTerminalUsageAfterClientDisconnect(t *testing.T) {
	body := "data: {\"type\":\"response.created\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5.6-sol\",\"usage\":{\"input_tokens\":11,\"output_tokens\":7,\"input_tokens_details\":{\"cached_tokens\":3,\"cache_write_tokens\":2}}}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	destination := &failingResponseWriter{}
	metrics, err := CopyResponse(destination, source, time.Now().Add(-time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if !metrics.ClientDisconnected || destination.writes != 1 {
		t.Fatalf("disconnect metrics = %+v, writes = %d", metrics, destination.writes)
	}
	if metrics.InputTokens != 11 || metrics.OutputTokens != 7 || metrics.CachedTokens != 3 || metrics.CacheCreationTokens != 2 {
		t.Fatalf("terminal usage was not drained: %+v", metrics)
	}
}

func TestCopyResponseReturnsSafeFailoverBeforeVisibleOutput(t *testing.T) {
	body := "data: {\"type\":\"response.created\"}\n\n" +
		"data: {\"type\":\"response.in_progress\"}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"model\":\"gpt-5.6-terra\",\"usage\":{\"input_tokens\":9},\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"limited\"}}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponse(recorder, source, time.Now())
	var failure *StreamFailoverError
	if !errors.As(err, &failure) || failure.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("error = %#v", err)
	}
	if recorder.Body.Len() != 0 || metrics.InputTokens != 9 || metrics.UpstreamModel != "gpt-5.6-terra" {
		t.Fatalf("response = %q, metrics = %+v", recorder.Body.String(), metrics)
	}
	if !strings.Contains(string(failure.StreamBody), "response.failed") {
		t.Fatalf("failover body = %q", failure.StreamBody)
	}
}

func TestCopyResponseCommitsPreambleAtPendingBufferLimit(t *testing.T) {
	preamble := "data: {\"type\":\"response.created\"}\n\n"
	failure := "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"rate_limit_exceeded\"}}}\n\n"
	body := preamble + failure
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()

	if _, err := copySSEWithPendingLimit(recorder, source, time.Now(), len(preamble)); err != nil {
		t.Fatal(err)
	}
	if recorder.Body.String() != body {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestCopyResponseReturnsSemanticStatusForNonStreamingTerminalFailure(t *testing.T) {
	responseJSON := `{"id":"resp_failed","status":"failed","error":{"code":"rate_limit_exceeded","message":"limited"}}`
	body := "data: {\"type\":\"response.failed\",\"response\":" + responseJSON + "}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	_, err := CopyResponseForRequest(recorder, source, time.Now(), false)
	var failure *StreamFailoverError
	if !errors.As(err, &failure) || failure.StatusCode != http.StatusTooManyRequests || string(failure.Response) != responseJSON || len(failure.StreamBody) != 0 {
		t.Fatalf("error = %#v", err)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("body was committed before failover: %q", recorder.Body.String())
	}

	final := httptest.NewRecorder()
	if err := WriteStreamFailoverError(final, source, failure, false); err != nil {
		t.Fatal(err)
	}
	if final.Code != http.StatusTooManyRequests || final.Body.String() != responseJSON {
		t.Fatalf("status = %d, body = %q", final.Code, final.Body.String())
	}
}

func TestCopyResponseDoesNotFailoverAfterVisibleOutput(t *testing.T) {
	body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"visible\"}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"rate_limit_exceeded\"}}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	if _, err := CopyResponse(recorder, source, time.Now()); err != nil {
		t.Fatal(err)
	}
	if recorder.Body.String() != body {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestCopyResponsePassesThroughNonRetryableFailureBeforeOutput(t *testing.T) {
	body := "data: {\"type\":\"response.created\"}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"content_policy_violation\",\"message\":\"not allowed by safety policy\"}}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	if _, err := CopyResponse(recorder, source, time.Now()); err != nil {
		t.Fatal(err)
	}
	if recorder.Body.String() != body {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestCopyResponseParsesImageAndWebSearchUsage(t *testing.T) {
	body := `data: {"type":"response.completed","response":{"model":"gpt-5.6-sol","usage":{"input_tokens":20,"output_tokens":30,"input_tokens_details":{"image_tokens":4},"output_tokens_details":{"image_tokens":12}},"output":[{"type":"image_generation_call","status":"completed"},{"type":"web_search_call","status":"completed"},{"type":"web_search_call","status":"failed"}]}}` + "\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	metrics, err := CopyResponse(httptest.NewRecorder(), source, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.ImageInputTokens != 4 || metrics.ImageOutputTokens != 12 || metrics.ImageCount != 1 || metrics.WebSearchCalls != 1 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestCopyResponsePreservesSSEBodyAndContentType(t *testing.T) {
	body := "data: {\"type\":\"response.output_text.delta\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":3,\"output_tokens\":5,\"input_tokens_details\":{\"cached_tokens\":1,\"cache_write_tokens\":2}}}}\n\n"
	source := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       ioNopCloser{Reader: strings.NewReader(body)},
	}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponse(recorder, source, time.Now().Add(-time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Body.String(); got != body {
		t.Fatalf("body = %q", got)
	}
	if metrics.TTFT <= 0 || metrics.Duration <= 0 {
		t.Fatalf("metrics = %+v", metrics)
	}
	if metrics.InputTokens != 3 || metrics.OutputTokens != 5 || metrics.CachedTokens != 1 || metrics.CacheCreationTokens != 2 {
		t.Fatalf("usage metrics = %+v", metrics)
	}
}

func TestCopyResponseConvertsSSEToJSONForNonStreamingCaller(t *testing.T) {
	responseJSON := `{"id":"resp_1","object":"response","status":"completed","output":[],"usage":{"input_tokens":3,"output_tokens":5,"input_tokens_details":{"cached_tokens":1}}}`
	body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":" + responseJSON + "}\n\n"
	source := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponseForRequest(recorder, source, time.Now().Add(-time.Millisecond), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Body.String(); got != responseJSON {
		t.Fatalf("body = %q", got)
	}
	if metrics.InputTokens != 3 || metrics.OutputTokens != 5 || metrics.CachedTokens != 1 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestCopyResponseSynthesizesFailureForIncompleteStream(t *testing.T) {
	source := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"req-123"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n")),
	}
	recorder := httptest.NewRecorder()
	_, err := CopyResponse(recorder, source, time.Now())
	if !errors.Is(err, ErrIncompleteStream) {
		t.Fatalf("error = %v", err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "event: response.failed") || !strings.Contains(body, `"id":"resp_req123"`) {
		t.Fatalf("body = %q", body)
	}
}

func TestCopyResponseExtractsUsageFromJSON(t *testing.T) {
	source := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":8,"output_tokens":2,"input_tokens_details":{"cached_tokens":4,"cache_write_tokens":3}}}`)),
	}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponseForRequest(recorder, source, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.InputTokens != 8 || metrics.OutputTokens != 2 || metrics.CachedTokens != 4 || metrics.CacheCreationTokens != 3 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestParseResponseUsageAcceptsOnlyTerminalEvents(t *testing.T) {
	terminalEvents := []string{
		"response.completed",
		"response.done",
		"response.failed",
		"response.incomplete",
		"response.cancelled",
		"response.canceled",
	}
	for _, eventType := range terminalEvents {
		t.Run(eventType, func(t *testing.T) {
			line := []byte("data: {\"type\":\"" + eventType + "\",\"response\":{\"usage\":{\"input_tokens\":13,\"output_tokens\":8,\"input_tokens_details\":{\"cached_tokens\":5,\"cache_write_tokens\":3}}}}\n")
			usage, ok := parseResponseUsage(line)
			if !ok || usage.Usage.InputTokens != 13 || usage.Usage.OutputTokens != 8 || usage.Usage.InputTokenDetails.CachedTokens != 5 || usage.Usage.InputTokenDetails.CacheWriteTokens != 3 {
				t.Fatalf("parseResponseUsage() = %+v, %v", usage, ok)
			}
		})
	}
	if _, ok := parseResponseUsage([]byte(`data: {"type":"response.output_text.delta","response":{"usage":{"input_tokens":13}}}`)); ok {
		t.Fatal("non-terminal event usage was accepted")
	}
	if _, ok := parseResponseUsage([]byte(`{"type":"response.completed","response":{"usage":{"input_tokens":13}}}`)); ok {
		t.Fatal("non-SSE payload was accepted")
	}
}

type ioNopCloser struct{ *strings.Reader }

func (ioNopCloser) Close() error { return nil }
