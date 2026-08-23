package openai

import (
	"bufio"
	"bytes"
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

func TestProbeQuotaQueriesResponsesEndpointAndParsesStandardWindows(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		headers := make(http.Header)
		headers.Set("x-codex-primary-used-percent", "48")
		headers.Set("x-codex-primary-reset-after-seconds", "288000")
		headers.Set("x-codex-primary-window-minutes", "10080")
		headers.Set("x-codex-secondary-used-percent", "12.5")
		headers.Set("x-codex-secondary-reset-after-seconds", "7200")
		headers.Set("x-codex-secondary-window-minutes", "300")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     headers,
			Body:       io.NopCloser(strings.NewReader("data: {}\n\n")),
			Request:    req,
		}, nil
	})}

	observedAt := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)
	gateway := NewGateway(client)
	gateway.now = func() time.Time { return observedAt }
	signals, err := gateway.ProbeQuota(context.Background(), "access-token", "account-id", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 2 {
		t.Fatalf("got %d signals, want 2", len(signals))
	}
	if signals[0].WindowType != domain.Window7D || signals[0].AccountUsedMicros != 48_000_000 {
		t.Fatalf("primary signal = %#v", signals[0])
	}
	if signals[1].WindowType != domain.Window5H || signals[1].AccountUsedMicros != 12_500_000 {
		t.Fatalf("secondary signal = %#v", signals[1])
	}
	if got := signals[0].ResetAt.Sub(signals[0].WindowStart); got != 7*24*time.Hour {
		t.Fatalf("7d window duration = %v", got)
	}
	if got := signals[0].ResetAt; !got.Equal(observedAt.Add(288000 * time.Second)) {
		t.Fatalf("7d reset_at = %v", got)
	}
	assertProbeRequest(t, captured)
}

func TestProbeQuotaAcceptsCompleteHeadersBeforeErrorStatus(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		headers := make(http.Header)
		headers.Set("x-codex-primary-used-percent", "48")
		headers.Set("x-codex-primary-reset-after-seconds", "288000")
		headers.Set("x-codex-primary-window-minutes", "10080")
		headers.Set("x-codex-secondary-used-percent", "12.5")
		headers.Set("x-codex-secondary-reset-after-seconds", "7200")
		headers.Set("x-codex-secondary-window-minutes", "300")
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: headers, Body: io.NopCloser(strings.NewReader("rate limited")), Request: req}, nil
	})}

	signals, err := NewGateway(client).ProbeQuota(context.Background(), "access-token", "account-id", "")
	if err != nil || len(signals) != 2 {
		t.Fatalf("signals = %#v, error = %v", signals, err)
	}
}

func TestProbeQuotaRejectsNonRateLimitErrorWithCompleteHeaders(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				headers := make(http.Header)
				headers.Set("x-codex-primary-used-percent", "48")
				headers.Set("x-codex-primary-reset-after-seconds", "288000")
				headers.Set("x-codex-primary-window-minutes", "10080")
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader("request failed")), Request: req}, nil
			})}

			signals, err := NewGateway(client).ProbeQuota(context.Background(), "access-token", "account-id", "")
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("status %d", status)) {
				t.Fatalf("signals = %#v, error = %v", signals, err)
			}
			if signals != nil {
				t.Fatalf("signals = %#v, want nil", signals)
			}
		})
	}
}

func TestProbeQuotaReturnsStatusErrorWithoutWeeklyWindow(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("no weekly quota header")),
					Request:    req,
				}, nil
			})}

			signals, err := NewGateway(client).ProbeQuota(context.Background(), "access-token", "account-id", "")
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("status %d without a valid 7d quota signal", status)) {
				t.Fatalf("error = %v, want status %d", err, status)
			}
			if signals != nil {
				t.Fatalf("signals = %#v, want nil", signals)
			}
		})
	}
}

func TestProbeQuotaAcceptsWeeklyWindowWithoutFiveHourWindow(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		headers := make(http.Header)
		headers.Set("x-codex-primary-used-percent", "19")
		headers.Set("x-codex-primary-reset-after-seconds", "604800")
		headers.Set("x-codex-primary-window-minutes", "10080")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     headers,
			Body:       io.NopCloser(strings.NewReader("single 7d window")),
			Request:    req,
		}, nil
	})}

	signals, err := NewGateway(client).ProbeQuota(context.Background(), "access-token", "account-id", "")
	if err != nil {
		t.Fatalf("signals = %#v, error = %v", signals, err)
	}
	if len(signals) != 1 || signals[0].WindowType != domain.Window7D || signals[0].AccountUsedMicros != 19_000_000 {
		t.Fatalf("signals = %#v, want one weekly signal", signals)
	}
}

func TestProbeQuotaRejectsFiveHourWindowWithoutWeeklyWindow(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		headers := make(http.Header)
		headers.Set("x-codex-primary-used-percent", "19")
		headers.Set("x-codex-primary-reset-after-seconds", "18000")
		headers.Set("x-codex-primary-window-minutes", "300")
		return &http.Response{StatusCode: http.StatusOK, Header: headers, Body: io.NopCloser(strings.NewReader("single 5h window")), Request: req}, nil
	})}

	signals, err := NewGateway(client).ProbeQuota(context.Background(), "access-token", "account-id", "")
	if err == nil || !strings.Contains(err.Error(), "without a valid 7d quota signal") {
		t.Fatalf("signals = %#v, error = %v", signals, err)
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
		"Originator":         codexDefaultOriginator,
		"Version":            codexProbeVersion,
		"User-Agent":         codexProbeUserAgent,
	}
	for key, want := range wantHeaders {
		if got := req.Header.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if got := req.Header.Get(codexRoutingHintHeader); got != "" {
		t.Errorf("%s = %q, want empty for the global quota probe", codexRoutingHintHeader, got)
	}
	deadline, ok := req.Context().Deadline()
	if !ok {
		t.Fatal("probe request context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 19*time.Second || remaining > codexProbeTimeout {
		t.Errorf("probe deadline remaining = %v", remaining)
	}
	var payload codexProbePayload
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
		"max_output_tokens":100,
		"temperature":0.2,
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
	for _, field := range []string{"metadata", "stream_options", "max_output_tokens", "temperature"} {
		if _, exists := payload[field]; exists {
			t.Fatalf("unsupported field %q was preserved", field)
		}
	}
	if payload["prompt_cache_key"] != "session-a" {
		t.Fatalf("prompt_cache_key = %v", payload["prompt_cache_key"])
	}
}

func TestPrepareRequestNormalizesCodexInputShapes(t *testing.T) {
	for _, test := range []struct {
		name    string
		input   string
		wantLen int
		wantMsg string
	}{
		{name: "string", input: `"hello"`, wantLen: 1, wantMsg: "hello"},
		{name: "empty string", input: `"  "`, wantLen: 0},
		{name: "object", input: `{"type":"message","role":"user","content":"hi"}`, wantLen: 1, wantMsg: "hi"},
		{name: "array", input: `[{"type":"message","role":"user","content":"kept"}]`, wantLen: 1, wantMsg: "kept"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, _, err := PrepareRequest([]byte(`{"model":"gpt-5.4","input":`+test.input+`}`), false)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			input, ok := payload["input"].([]any)
			if !ok || len(input) != test.wantLen {
				t.Fatalf("input = %#v", payload["input"])
			}
			if test.wantLen > 0 {
				item, ok := input[0].(map[string]any)
				if !ok || item["content"] != test.wantMsg {
					t.Fatalf("input item = %#v", input[0])
				}
			}
		})
	}
}

func TestPrepareRequestAddsEncryptedReasoningInclude(t *testing.T) {
	body, _, err := PrepareRequest([]byte(`{
		"model":"gpt-5.4",
		"input":[],
		"reasoning":{"effort":"high","summary":"auto"},
		"include":["message.output_text.logprobs"]
	}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	include, ok := payload["include"].([]any)
	if !ok || len(include) != 2 || include[0] != "message.output_text.logprobs" || include[1] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v", payload["include"])
	}
	reasoning := payload["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning = %#v", reasoning)
	}
}

func TestPrepareRequestPreservesNativeResponsesToolsAndContinuation(t *testing.T) {
	body, _, err := PrepareRequest([]byte(`{
		"model":"gpt-5.4",
		"input":[
			{"type":"function_call","call_id":"call_123","name":"search","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_123","output":"result"}
		],
		"tools":[{"type":"function","name":"search","description":"Search","parameters":{"type":"object"}},{"type":"image_generation"}],
		"tool_choice":{"type":"function","name":"search"},
		"parallel_tool_calls":true
	}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	input := payload["input"].([]any)
	if input[0].(map[string]any)["call_id"] != "call_123" || input[1].(map[string]any)["call_id"] != "call_123" {
		t.Fatalf("tool continuation = %#v", input)
	}
	tools := payload["tools"].([]any)
	if len(tools) != 2 || tools[0].(map[string]any)["name"] != "search" || tools[1].(map[string]any)["type"] != "image_generation" || payload["parallel_tool_calls"] != true {
		t.Fatalf("tools payload = %#v", payload)
	}
}

func TestPrepareRequestNormalizesNullToolParameterTypes(t *testing.T) {
	body, _, err := PrepareRequest([]byte(`{
		"model":"gpt-5.4",
		"tools":[
			{"type":"function","name":"native","parameters":{"type":null,"properties":{}}},
			{"type":"function","function":{"name":"legacy","parameters":{"type":null}}},
			{"type":"function","name":"missing","parameters":{"properties":{}}}
		],
		"input":[{"type":"additional_tools","tools":[{"type":"namespace","tools":[
			{"type":"function","name":"nested","parameters":{"type":null}}
		]}]}]
	}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	tools := payload["tools"].([]any)
	if tools[0].(map[string]any)["parameters"].(map[string]any)["type"] != "object" {
		t.Fatalf("native parameters = %#v", tools[0])
	}
	legacy := tools[1].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)
	if legacy["type"] != "object" {
		t.Fatalf("legacy parameters = %#v", legacy)
	}
	missing := tools[2].(map[string]any)["parameters"].(map[string]any)
	if _, exists := missing["type"]; exists {
		t.Fatalf("missing schema type was invented: %#v", missing)
	}
	input := payload["input"].([]any)
	namespaceTools := input[0].(map[string]any)["tools"].([]any)[0].(map[string]any)["tools"].([]any)
	if namespaceTools[0].(map[string]any)["parameters"].(map[string]any)["type"] != "object" {
		t.Fatalf("nested parameters = %#v", namespaceTools[0])
	}
}

func TestPrepareRequestValidatesExplicitCodexInstructions(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-5.4-codex","instructions":"  ","input":[]}`,
		`{"model":"gpt-5.4-codex","instructions":{"text":"invalid"},"input":[]}`,
	} {
		if _, _, err := PrepareRequest([]byte(body), false); err == nil {
			t.Fatalf("request %s was accepted", body)
		}
	}
	if _, _, err := PrepareRequest([]byte(`{"model":"gpt-5.4-codex","input":[]}`), false); err != nil {
		t.Fatalf("missing optional instructions was rejected: %v", err)
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

func TestPrepareAlphaSearchRequestPreservesWireAndRemovesRejectedFields(t *testing.T) {
	original := []byte(`{"id":"search-session","model":"gpt-5.6-sol","commands":{"search_query":[{"q":"news"}]},"future_field":{"keep":true}}`)
	body, metadata, err := PrepareAlphaSearchRequest(original)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Model != "gpt-5.6-sol" || string(body) != string(original) {
		t.Fatalf("body = %s, metadata = %+v", body, metadata)
	}

	body, metadata, err = PrepareAlphaSearchRequest([]byte(`{"model":"gpt-5.6-sol","prompt_cache_key":"session","prompt_cache_retention":"24h","future_field":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if metadata.Model != "gpt-5.6-sol" || payload["future_field"] != true {
		t.Fatalf("body = %s, metadata = %+v", body, metadata)
	}
	for _, field := range alphaSearchUnsupportedFields {
		if _, exists := payload[field]; exists {
			t.Fatalf("unsupported field %q was forwarded", field)
		}
	}
}

func TestPrepareAlphaSearchRequestRejectsInvalidRequests(t *testing.T) {
	for _, body := range []string{``, `[]`, `{}`, `{"model":1}`, `{"model":""}`} {
		if _, _, err := PrepareAlphaSearchRequest([]byte(body)); err == nil {
			t.Fatalf("request %q was accepted", body)
		}
	}
}

func TestForwardAlphaSearchUsesStandaloneSearchProtocol(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"output":"result"}`)), Request: req}, nil
	})}
	inbound := httptest.NewRequest(http.MethodPost, "http://gateway.test/v1/alpha/search?feature=standalone", nil)
	inbound.Header.Set("Originator", "untrusted-client")
	inbound.Header.Set("Version", "0.144.1")
	inbound.Header.Set("User-Agent", "untrusted-client/9.9.9")
	inbound.Header.Set("X-Codex-Turn-Metadata", `{"turn_id":"turn-1"}`)

	response, err := NewGateway(client).ForwardAlphaSearch(context.Background(), inbound, []byte(`{"model":"gpt-5.6-sol"}`), "access", "account", "")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if captured.URL.String() != codexAlphaSearchURL+"?feature=standalone" || captured.Host != "chatgpt.com" {
		t.Fatalf("request target = %s (Host %q)", captured.URL, captured.Host)
	}
	wantHeaders := map[string]string{
		"Authorization":         "Bearer access",
		"Chatgpt-Account-Id":    "account",
		"Content-Type":          "application/json",
		"Accept":                "application/json",
		"Originator":            codexDefaultOriginator,
		"Version":               codexProbeVersion,
		"User-Agent":            codexProbeUserAgent,
		"X-Codex-Turn-Metadata": `{"turn_id":"turn-1"}`,
	}
	for key, want := range wantHeaders {
		if got := captured.Header.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	for _, absent := range []string{"OpenAI-Beta", "Session_Id", "Conversation_Id"} {
		if got := captured.Header.Get(absent); got != "" {
			t.Errorf("%s = %q, want empty", absent, got)
		}
	}
}

func TestForwardAlphaSearchNormalizesInvalidIdentityAndOldVersion(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
	})}
	inbound := httptest.NewRequest(http.MethodPost, "http://gateway.test/alpha/search", nil)
	inbound.Header.Set("Version", "0.137.0")
	inbound.Header.Set("User-Agent", "Mozilla/5.0")
	response, err := NewGateway(client).ForwardAlphaSearch(context.Background(), inbound, []byte(`{"model":"gpt-5.6-sol"}`), "access", "", "")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if captured.Header.Get("Version") != codexProbeVersion || captured.Header.Get("Originator") != codexDefaultOriginator || captured.Header.Get("User-Agent") != codexProbeUserAgent {
		t.Fatalf("identity headers = %#v", captured.Header)
	}
	if captured.Header.Get("Chatgpt-Account-Id") != "" {
		t.Fatalf("empty account id produced header %q", captured.Header.Get("Chatgpt-Account-Id"))
	}
}

func TestCopyAlphaSearchResponseCountsOnlySuccess(t *testing.T) {
	for _, test := range []struct {
		status int
		want   int64
	}{
		{status: http.StatusOK, want: 1},
		{status: http.StatusNotFound, want: 0},
	} {
		source := &http.Response{StatusCode: test.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"output":"result"}`))}
		metrics, err := CopyAlphaSearchResponse(httptest.NewRecorder(), source, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if metrics.WebSearchCalls != test.want {
			t.Fatalf("status %d web search calls = %d, want %d", test.status, metrics.WebSearchCalls, test.want)
		}
	}
}

func TestForwardCompactUsesCompactEndpointAndHeaders(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_1"}`)), Request: req}, nil
	})}
	inbound := httptest.NewRequest(http.MethodPost, "http://gateway.test/v1/responses/compact", nil)
	inbound.Header.Set("Originator", "untrusted-client")
	inbound.Header.Set("Version", "9.9.9")
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
	if captured.Header.Get("Accept") != "application/json" || captured.Header.Get("Version") != codexProbeVersion || captured.Header.Get("Originator") != codexDefaultOriginator {
		t.Fatalf("compact headers = %#v", captured.Header)
	}
	if captured.Header.Get("OpenAI-Beta") != "" {
		t.Fatalf("legacy responses beta was forwarded: %q", captured.Header.Get("OpenAI-Beta"))
	}
	if captured.Header.Get("User-Agent") != codexProbeUserAgent {
		t.Fatalf("user-agent = %q", captured.Header.Get("User-Agent"))
	}
	if got, want := captured.Header.Get("Session_Id"), isolateSession("key", "compact-session"); got != want {
		t.Fatalf("session_id = %q, want %q", got, want)
	}
}

func TestForwardRemovesOnlyLegacyResponsesBeta(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: http.NoBody, Request: req}, nil
	})}
	inbound := httptest.NewRequest(http.MethodPost, "http://gateway.test/v1/responses", nil)
	inbound.Header.Set("Originator", "untrusted-client")
	inbound.Header.Set("Version", "9.9.9")
	inbound.Header.Set("User-Agent", "untrusted-client/9.9.9")
	inbound.Header.Add("OpenAI-Beta", "responses=experimental, future_feature=v1")
	inbound.Header.Add("OpenAI-Beta", "another_feature=v2, RESPONSES=EXPERIMENTAL")
	response, err := NewGateway(client).Forward(context.Background(), inbound, []byte(`{"model":"gpt-5.4","input":[],"stream":true,"store":false}`), RequestBilling{Model: "gpt-5.4", Stream: true}, "access", "account", "key", "")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	values := captured.Header.Values("OpenAI-Beta")
	if len(values) != 2 || values[0] != "future_feature=v1" || values[1] != "another_feature=v2" {
		t.Fatalf("OpenAI-Beta = %#v", values)
	}
	if captured.Header.Get("Originator") != codexDefaultOriginator || captured.Header.Get("Version") != codexProbeVersion || captured.Header.Get("User-Agent") != codexProbeUserAgent {
		t.Fatalf("identity headers = %#v", captured.Header)
	}
}

func TestForwardPreservesRemoteCompactionV2ResponsesWire(t *testing.T) {
	var captured *http.Request
	var capturedBody []byte
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: http.NoBody, Request: req}, nil
	})}
	inbound := httptest.NewRequest(http.MethodPost, "http://gateway.test/v1/responses", nil)
	inbound.Header.Set("X-Codex-Beta-Features", "responses_websockets_v2, remote_compaction_v2")
	body := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"message","role":"user","content":"hello"},{"type":"compaction_trigger"}],"reasoning":{"effort":"max","context":"all_turns"}}`)
	forwardBody, metadata, err := PrepareRequest(body, false)
	if err != nil {
		t.Fatal(err)
	}
	response, err := NewGateway(client).Forward(context.Background(), inbound, forwardBody, metadata, "access", "account", "key", "")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	if captured.URL.String() != codexResponsesURL {
		t.Fatalf("target = %q, want native Responses endpoint", captured.URL)
	}
	if got := captured.Header.Get("X-Codex-Beta-Features"); got != "responses_websockets_v2, remote_compaction_v2" {
		t.Fatalf("X-Codex-Beta-Features = %q", got)
	}
	var payload map[string]any
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatal(err)
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) != 2 || input[1].(map[string]any)["type"] != "compaction_trigger" {
		t.Fatalf("remote compaction input = %#v", payload["input"])
	}
	reasoning, ok := payload["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "max" || reasoning["context"] != "all_turns" || payload["stream"] != true {
		t.Fatalf("remote compaction payload = %#v", payload)
	}
}

func TestForwardOverridesMultipartContentTypeAfterBodyNormalization(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	inbound := httptest.NewRequest(http.MethodPost, "http://gateway.test/v1/images/edits", nil)
	inbound.Header.Set("Content-Type", "multipart/form-data; boundary=original")

	metadata := RequestBilling{Model: "gpt-image-2", PromptCacheKey: "openai-images|edit"}
	response, err := NewGateway(client).Forward(context.Background(), inbound, []byte(`{"model":"gpt-5.4-mini"}`), metadata, "access", "account", "key", "")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if got := captured.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if captured.Header.Get("OpenAI-Beta") != "" || captured.Header.Get("Accept") != "text/event-stream" || captured.Header.Get("Version") != codexProbeVersion {
		t.Fatalf("images headers = %#v", captured.Header)
	}
	if got, want := captured.Header.Get("Session_Id"), isolateSession("key", metadata.PromptCacheKey); got != want {
		t.Fatalf("session_id = %q, want %q", got, want)
	}
	if got, want := captured.Header.Get("Conversation_Id"), isolateSession("key", metadata.PromptCacheKey); got != want {
		t.Fatalf("conversation_id = %q, want %q", got, want)
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
	if captured.URL.String() != codexModelsURL+"?client_version="+codexProbeVersion || captured.Host != "chatgpt.com" {
		t.Fatalf("request target = %s (Host %q)", captured.URL, captured.Host)
	}
	wantHeaders := map[string]string{
		"Authorization":      "Bearer access",
		"Chatgpt-Account-Id": "account",
		"Accept":             "application/json",
		"Originator":         codexDefaultOriginator,
		"Version":            codexProbeVersion,
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
		{name: "capacity error", line: `data: {"type":"error","error":{"code":"server_is_overloaded","message":"overloaded"}}`, want: false},
		{name: "rate limit error", line: `data: {"type":"error","error":{"code":"rate_limit_exceeded","message":"limited"}}`, want: false},
		{name: "policy error", line: `data: {"type":"error","error":{"code":"content_policy_violation","message":"not allowed by safety policy"}}`, want: true},
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

func TestIsClientVisibleOutputEventRequiresContent(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{name: "text delta", line: `data: {"type":"response.output_text.delta","delta":"hi"}`, want: true},
		{name: "empty delta", line: `data: {"type":"response.output_text.delta","delta":""}`},
		{name: "structural item", line: `data: {"type":"response.output_item.added","item":{"type":"message"}}`},
		{name: "item content", line: `data: {"type":"response.output_item.done","item":{"content":[{"text":"hi"}]}}`, want: true},
		{name: "text done", line: `data: {"type":"response.output_text.done","text":"hi"}`, want: true},
		{name: "audio transcript done", line: `data: {"type":"response.audio_transcript.done","transcript":"hello"}`, want: true},
		{name: "refusal done", line: `data: {"type":"response.refusal.done","refusal":"blocked"}`, want: true},
		{name: "refusal content part", line: `data: {"type":"response.content_part.done","part":{"type":"refusal","refusal":"blocked"}}`, want: true},
		{name: "function done", line: `data: {"type":"response.function_call_arguments.done","arguments":"{}"}`, want: true},
		{name: "completed without output", line: `data: {"type":"response.completed","response":{"output":[]}}`},
		{name: "completed with output", line: `data: {"type":"response.completed","response":{"output":[{"arguments":"{}"}]}}`, want: true},
		{name: "completed with refusal", line: `data: {"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"refusal","refusal":"blocked"}]}]}}`, want: true},
		{name: "policy error", line: `data: {"type":"error","error":{"message":"blocked"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isClientVisibleOutputEvent([]byte(test.line)); got != test.want {
				t.Fatalf("isClientVisibleOutputEvent() = %v, want %v", got, test.want)
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

type terminalCancelResponseWriter struct {
	header       http.Header
	cancel       context.CancelFunc
	terminalSeen bool
}

func (w *terminalCancelResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *terminalCancelResponseWriter) WriteHeader(int) {}

func (w *terminalCancelResponseWriter) Write(body []byte) (int, error) {
	if bytes.Contains(body, []byte(`"type":"response.completed"`)) {
		w.terminalSeen = true
	} else if w.terminalSeen && len(bytes.TrimSpace(body)) == 0 {
		w.cancel()
	}
	return len(body), nil
}

func (w *terminalCancelResponseWriter) Flush() {}

type terminalBoundaryFailResponseWriter struct {
	header       http.Header
	terminalSeen bool
}

func (w *terminalBoundaryFailResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *terminalBoundaryFailResponseWriter) WriteHeader(int) {}

func (w *terminalBoundaryFailResponseWriter) Write(body []byte) (int, error) {
	if w.terminalSeen && len(bytes.TrimSpace(body)) == 0 {
		return 0, errors.New("client disconnected before terminal event boundary")
	}
	if bytes.Contains(body, []byte(`"type":"response.completed"`)) {
		w.terminalSeen = true
	}
	return len(body), nil
}

func (w *terminalBoundaryFailResponseWriter) Flush() {}

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
	if metrics.ResponseDelivered {
		t.Fatalf("terminal response must not be marked delivered after downstream write failure: %+v", metrics)
	}
	if metrics.InputTokens != 11 || metrics.OutputTokens != 7 || metrics.CachedTokens != 3 || metrics.CacheCreationTokens != 2 {
		t.Fatalf("terminal usage was not drained: %+v", metrics)
	}
}

func TestCopyResponseMarksTerminalDeliveredBeforeRequestCancellation(t *testing.T) {
	body := "data: {\"type\":\"response.created\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5.6-sol\",\"usage\":{\"input_tokens\":11,\"output_tokens\":7}}}\n\n"
	ctx, cancel := context.WithCancel(context.Background())
	destination := &terminalCancelResponseWriter{cancel: cancel}
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}

	metrics, err := CopyResponse(destination, source, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("request context was not canceled after terminal delivery")
	}
	if metrics.ClientDisconnected || !metrics.ResponseDelivered {
		t.Fatalf("terminal delivery metrics = %+v", metrics)
	}
}

func TestCopyResponseRequiresTerminalEventBoundaryForDelivery(t *testing.T) {
	body := "data: {\"type\":\"response.created\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5.6-sol\",\"usage\":{\"input_tokens\":11,\"output_tokens\":7}}}\n\n"
	destination := &terminalBoundaryFailResponseWriter{}
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}

	metrics, err := CopyResponse(destination, source, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !metrics.ClientDisconnected || metrics.ResponseDelivered {
		t.Fatalf("terminal boundary failure metrics = %+v", metrics)
	}
}

func TestCopyResponseMarksNonStreamingResponsesDelivered(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{
			name:        "SSE converted to JSON",
			contentType: "text/event-stream",
			body:        "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"type\":\"message\"}],\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n",
		},
		{
			name:        "buffered JSON",
			contentType: "application/json",
			body:        `{"id":"resp_1","status":"completed","output":[{"type":"message"}],"usage":{"input_tokens":2,"output_tokens":1}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{test.contentType}}, Body: io.NopCloser(strings.NewReader(test.body))}
			destination := httptest.NewRecorder()

			metrics, err := CopyResponseForRequest(destination, source, time.Now(), false)
			if err != nil {
				t.Fatal(err)
			}
			if metrics.ClientDisconnected || !metrics.ResponseDelivered {
				t.Fatalf("response delivery metrics = %+v", metrics)
			}
		})
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

func TestCopyResponseCapacityErrorFrameBeforeFailedStillFailsOver(t *testing.T) {
	body := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n" +
		"event: response.in_progress\n" +
		"data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"resp_1\"}}\n\n" +
		"event: error\n" +
		"data: {\"type\":\"error\",\"error\":{\"type\":\"service_unavailable_error\",\"code\":\"server_is_overloaded\",\"message\":\"Our servers are currently overloaded. Please try again later.\"}}\n\n" +
		"event: response.failed\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_1\",\"status\":\"failed\",\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"Our servers are currently overloaded. Please try again later.\"}}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponse(recorder, source, time.Now())
	var failure *StreamFailoverError
	if !errors.As(err, &failure) || failure.StatusCode != http.StatusServiceUnavailable || !failure.RequestScopedTransient {
		t.Fatalf("error = %#v", err)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("body was committed before failover: %q", recorder.Body.String())
	}
	if metrics.ErrorCode != "server_is_overloaded" || !strings.Contains(string(failure.StreamBody), "server_is_overloaded") {
		t.Fatalf("metrics = %+v, failover body = %q", metrics, failure.StreamBody)
	}

	final := httptest.NewRecorder()
	if err := WriteStreamFailoverError(final, source, failure, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(final.Body.String(), "server_is_overloaded") || strings.Count(final.Body.String(), `"code":"server_error"`) != 2 {
		t.Fatalf("client body = %q", final.Body.String())
	}
}

func TestCopyResponseRewritesCapacityErrorsAfterVisibleOutput(t *testing.T) {
	body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"visible\"}\n\n" +
		"data: {\"type\":\"error\",\"error\":{\"type\":\"service_unavailable_error\",\"code\":\"server_is_overloaded\",\"message\":\"overloaded\"}}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"overloaded\"}}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponse(recorder, source, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(recorder.Body.String(), "server_is_overloaded") || strings.Count(recorder.Body.String(), `"code":"server_error"`) != 2 {
		t.Fatalf("client body = %q", recorder.Body.String())
	}
	if metrics.ErrorCode != "server_is_overloaded" || metrics.ErrorStatusCode != http.StatusServiceUnavailable {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestCapacityErrorRewriteHandlesOnlyCapacityCodes(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{code: "server_is_overloaded", want: "server_error"},
		{code: "slow_down", want: "server_error"},
		{code: "rate_limit_exceeded", want: "rate_limit_exceeded"},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			body := []byte("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"" + test.code + "\",\"message\":\"message\"}}}\n")
			got := sanitizeCapacityShedSSEForClient(body)
			if !bytes.Contains(got, []byte(`"code":"`+test.want+`"`)) {
				t.Fatalf("body = %q", got)
			}
			if test.code == "rate_limit_exceeded" && !bytes.Equal(got, body) {
				t.Fatalf("non-capacity body changed: %q", got)
			}
		})
	}
}

func TestWriteStreamFailoverErrorRewritesNonStreamingCapacityCode(t *testing.T) {
	failure := &StreamFailoverError{
		StatusCode: http.StatusServiceUnavailable,
		Response:   json.RawMessage(`{"id":"resp_1","error":{"code":"server_is_overloaded","message":"overloaded"}}`),
	}
	source := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	recorder := httptest.NewRecorder()
	if err := WriteStreamFailoverError(recorder, source, failure, false); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), "server_is_overloaded") || !strings.Contains(recorder.Body.String(), `"code":"server_error"`) {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
}

func TestCopyBufferedResponseRewritesCapacityCodeForClient(t *testing.T) {
	body := `{"error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"overloaded"}}`
	source := &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponse(recorder, source, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(recorder.Body.String(), "server_is_overloaded") || !strings.Contains(recorder.Body.String(), `"code":"server_error"`) {
		t.Fatalf("client body = %q", recorder.Body.String())
	}
	if metrics.ErrorCode != "server_is_overloaded" || metrics.ErrorStatusCode != http.StatusServiceUnavailable {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestWriteDrainedResponseRewritesCapacityCodeForClient(t *testing.T) {
	body := []byte(`{"error":{"code":"slow_down","message":"overloaded"}}`)
	source := &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Content-Type": []string{"application/json"}}}
	recorder := httptest.NewRecorder()
	if err := WriteDrainedResponse(recorder, source, body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(recorder.Body.String(), "slow_down") || !strings.Contains(recorder.Body.String(), `"code":"server_error"`) {
		t.Fatalf("client body = %q", recorder.Body.String())
	}
	if !bytes.Contains(body, []byte(`"code":"slow_down"`)) {
		t.Fatalf("original body changed: %q", body)
	}
}

func TestCopyResponseSanitizesPendingCapacityErrorAtBufferLimit(t *testing.T) {
	capacityError := "data: {\"type\":\"error\",\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"overloaded\"}}\n\n"
	failure := "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"overloaded\"}}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(capacityError + failure))}
	recorder := httptest.NewRecorder()
	metrics, err := copySSEWithPendingLimit(recorder, source, time.Now(), len(capacityError)+1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(recorder.Body.String(), "server_is_overloaded") || strings.Count(recorder.Body.String(), `"code":"server_error"`) != 2 {
		t.Fatalf("client body = %q", recorder.Body.String())
	}
	if metrics.ErrorCode != "server_is_overloaded" {
		t.Fatalf("metrics = %+v", metrics)
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

func TestCopyResponseReturnsNonRetryableTerminalFailureWithSemanticStatus(t *testing.T) {
	responseJSON := `{"id":"resp_failed","status":"failed","error":{"type":"invalid_request_error","code":"content_policy_violation","message":"blocked"}}`
	body := "data: {\"type\":\"response.failed\",\"response\":" + responseJSON + "}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponseForRequest(recorder, source, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusBadRequest || recorder.Body.String() != responseJSON {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if metrics.ErrorCode != "content_policy_violation" || metrics.ErrorStatusCode != http.StatusBadRequest {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestCopyResponsePreservesIncompleteResponseForNonStreamingCaller(t *testing.T) {
	responseJSON := `{"id":"resp_incomplete","status":"incomplete","output":[],"incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":4,"output_tokens":8}}`
	body := "data: {\"type\":\"response.incomplete\",\"response\":" + responseJSON + "}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponseForRequest(recorder, source, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || recorder.Body.String() != responseJSON {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if metrics.InputTokens != 4 || metrics.OutputTokens != 8 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestCopyResponseHandlesStandaloneErrorEventForNonStreamingCaller(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"code\":\"invalid_request\",\"message\":\"invalid input\"}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponseForRequest(recorder, source, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"invalid_request"`) {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if metrics.ErrorCode != "invalid_request" || metrics.ErrorMessage != "invalid input" || metrics.ErrorStatusCode != http.StatusBadRequest {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestCopyResponseTreatsStandaloneRetryableErrorAsTerminal(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"code\":\"rate_limit_exceeded\",\"message\":\"limited\"}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponse(recorder, source, time.Now())
	var failure *StreamFailoverError
	if !errors.As(err, &failure) || failure.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(failure.StreamBody), `"type":"error"`) {
		t.Fatalf("error = %#v", err)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("body was committed before terminal classification: %q", recorder.Body.String())
	}
	if metrics.ErrorCode != "rate_limit_exceeded" || metrics.ErrorStatusCode != http.StatusTooManyRequests {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestCopyResponseMarksStandaloneCapacityErrorRequestScoped(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{\"type\":\"service_unavailable_error\",\"code\":\"slow_down\",\"message\":\"overloaded\"}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	_, err := CopyResponse(recorder, source, time.Now())
	var failure *StreamFailoverError
	if !errors.As(err, &failure) || !failure.RequestScopedTransient {
		t.Fatalf("error = %#v", err)
	}
}

func TestCopyResponseCarriesTopLevelErrorIntoTerminalResponse(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{\"type\":\"service_unavailable_error\",\"code\":\"server_is_overloaded\",\"message\":\"overloaded\"}}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_1\",\"status\":\"failed\"}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponse(recorder, source, time.Now())
	var failure *StreamFailoverError
	if !errors.As(err, &failure) || failure.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("error = %#v", err)
	}
	if recorder.Body.Len() != 0 || metrics.ErrorCode != "server_is_overloaded" || metrics.ErrorStatusCode != http.StatusServiceUnavailable {
		t.Fatalf("body = %q, metrics = %+v", recorder.Body.String(), metrics)
	}
}

func TestCopyResponseCarriesTopLevelErrorIntoNonStreamingTerminalResponse(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"code\":\"invalid_request\",\"message\":\"invalid input\"}}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_1\",\"status\":\"failed\"}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	metrics, err := CopyResponseForRequest(recorder, source, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	var response struct {
		ID     string         `json:"id"`
		Status string         `json:"status"`
		Error  *responseError `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != "resp_1" || response.Status != "failed" || response.Error == nil || response.Error.Type != "invalid_request_error" || response.Error.Code != "invalid_request" || response.Error.Message != "invalid input" {
		t.Fatalf("response = %+v", response)
	}
	if metrics.ErrorCode != "invalid_request" || metrics.ErrorMessage != "invalid input" || metrics.ErrorStatusCode != http.StatusBadRequest {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestCopyResponseFailsOverEmptyCompleted(t *testing.T) {
	body := "data: {\"type\":\"response.created\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_empty\",\"status\":\"completed\"}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	_, err := CopyResponseForRequest(recorder, source, time.Now(), true)
	var failure *StreamFailoverError
	if !errors.As(err, &failure) || failure.StatusCode != http.StatusBadGateway {
		t.Fatalf("error = %#v", err)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("empty response was committed: %q", recorder.Body.String())
	}
}

func TestCopyResponseAllowsCompletedWithUsage(t *testing.T) {
	body := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_usage\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	if _, err := CopyResponseForRequest(recorder, source, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recorder.Body.String(), "response.completed") {
		t.Fatalf("completed response missing: %q", recorder.Body.String())
	}
}

func TestCopyResponseFirstOutputTimeout(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}
	recorder := httptest.NewRecorder()
	started := time.Now()
	_, err := CopyResponseForRequest(recorder, source, started, true, 20*time.Millisecond)
	var failure *StreamFailoverError
	if !errors.As(err, &failure) || failure.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("error = %#v", err)
	}
	if time.Since(started) > time.Second || recorder.Body.Len() != 0 {
		t.Fatalf("timeout duration=%v body=%q", time.Since(started), recorder.Body.String())
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
	body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
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
