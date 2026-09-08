package openai

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestRequestTimingDisabled(t *testing.T) {
	gateway := NewGateway(http.DefaultClient)
	defer gateway.Close()
	ctx := context.Background()
	got, timing := gateway.BeginRequestTiming(ctx, "request", "/responses")
	if got != ctx || timing != nil {
		t.Fatal("disabled tracing changed context")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test", nil)
	if traceTimingRequest(req, 0) != req {
		t.Fatal("disabled tracing cloned request")
	}
	timing.BodyRead(time.Now(), 0)
	timing.Finish()
}

func TestRequestTimingSampling(t *testing.T) {
	for _, test := range []struct {
		name        string
		sampleEvery int
		status      int
		slow        bool
		want        int
	}{
		{"none", 0, 200, false, 0}, {"all", 1, 200, false, 4},
		{"half", 2, 200, false, 2}, {"error", 0, 499, false, 4},
		{"slow", 0, 200, true, 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			gateway := NewGateway(http.DefaultClient)
			defer gateway.Close()
			gateway.ConfigureTiming(slog.New(slog.NewJSONHandler(&output, nil)), TimingOptions{Enabled: true, SlowThreshold: time.Hour, SampleEvery: test.sampleEvery})
			for range 4 {
				ctx, timing := gateway.BeginRequestTiming(context.Background(), "request", "/responses")
				if test.slow {
					timing.started = time.Now().Add(-2 * time.Hour)
				}
				RecordRequestTimingMetric(ctx, domain.GatewayMetric{StatusCode: test.status})
				timing.Finish()
			}
			if got := bytes.Count(output.Bytes(), []byte("\n")); got != test.want {
				t.Fatalf("logs = %d, want %d", got, test.want)
			}
		})
	}
}

func TestRequestTimingForwardAndCopy(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(map[bool]string{true: "stream", false: "json"}[stream], func(t *testing.T) {
			var output bytes.Buffer
			payload := "data: {\"type\":\"response.created\"}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"private-output\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"response\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"private-output\"}]}],\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n"
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				trace := httptrace.ContextClientTrace(req.Context())
				trace.GetConn("private-host")
				trace.GotConn(httptrace.GotConnInfo{Reused: true})
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatal(err)
				}
				attempt, _ := req.Context().Value(attemptTimingKey{}).(*attemptTiming)
				if attempt.bodyBytes != len(body) {
					t.Fatal("wrong actual upstream body size")
				}
				trace.WroteRequest(httptrace.WroteRequestInfo{})
				trace.GotFirstResponseByte()
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
			})}
			gateway := NewGateway(client)
			defer gateway.Close()
			gateway.ConfigureTiming(slog.New(slog.NewJSONHandler(&output, nil)), TimingOptions{Enabled: true, SlowThreshold: time.Hour, SampleEvery: 1})
			ctx, timing := gateway.BeginRequestTiming(context.Background(), "request\n"+strings.Repeat("a", 200), "/responses")
			inbound := httptest.NewRequest(http.MethodPost, "/responses", nil)
			upstream, err := gateway.Forward(ctx, inbound, []byte(`{"model":"test","input":"private-input"}`), RequestBilling{Model: "test", Stream: stream}, "private-token", "chatgpt", "key", "")
			if err != nil {
				t.Fatal(err)
			}
			defer upstream.Body.Close()
			recorder := httptest.NewRecorder()
			metrics, err := CopyResponseForRequest(recorder, upstream, time.Now(), stream)
			if err != nil || metrics.OutputTokens != 1 || !strings.Contains(recorder.Body.String(), "private-output") {
				t.Fatalf("copy changed: metrics=%+v err=%v body=%s", metrics, err, recorder.Body.String())
			}
			RecordRequestTimingMetric(ctx, domain.GatewayMetric{StatusCode: 200, RequestID: "metric"})
			timing.Finish()
			var summary struct {
				RequestID string           `json:"request_id"`
				Attempts  []map[string]any `json:"attempts"`
			}
			if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
				t.Fatal(err)
			}
			if len(summary.RequestID) > 128 || len(summary.Attempts) != 1 {
				t.Fatalf("summary = %+v", summary)
			}
			for _, field := range []string{"get_conn_ms", "got_conn_ms", "request_written_ms", "first_response_byte_ms", "response_headers_ms", "first_sse_line_ms", "first_content_ms"} {
				if _, ok := summary.Attempts[0][field]; !ok {
					t.Errorf("missing %s", field)
				}
			}
			for _, secret := range []string{"private-input", "private-output", "private-token", "private-host"} {
				if strings.Contains(output.String(), secret) {
					t.Errorf("logged %s", secret)
				}
			}
		})
	}
}

func TestRequestTimingConcurrentCallbacksAndBoundedRetries(t *testing.T) {
	var output bytes.Buffer
	gateway := NewGateway(http.DefaultClient)
	defer gateway.Close()
	gateway.ConfigureTiming(slog.New(slog.NewJSONHandler(&output, nil)), TimingOptions{Enabled: true, SlowThreshold: time.Hour})
	ctx, timing := gateway.BeginRequestTiming(context.Background(), "request", "/responses")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test", nil)
	var workers sync.WaitGroup
	for range 20 {
		traced := traceTimingRequest(req, 10)
		trace := httptrace.ContextClientTrace(traced.Context())
		workers.Go(func() {
			trace.GetConn("host")
			trace.DNSStart(httptrace.DNSStartInfo{})
			trace.DNSDone(httptrace.DNSDoneInfo{})
			trace.TLSHandshakeStart()
			trace.TLSHandshakeDone(tls.ConnectionState{}, nil)
			trace.GotConn(httptrace.GotConnInfo{})
			trace.WroteRequest(httptrace.WroteRequestInfo{Err: errors.New("private-error")})
			trace.GotFirstResponseByte()
		})
	}
	RecordRequestTimingMetric(ctx, domain.GatewayMetric{StatusCode: 499})
	RecordRequestTimingMetric(ctx, domain.GatewayMetric{StatusCode: 200})
	timing.Finish()
	workers.Wait()
	if timing.attemptCount != 20 || len(timing.attempts) != 16 || bytes.Count(output.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("unbounded or missing summary: %s", output.String())
	}
	if !strings.Contains(output.String(), `"had_error":true`) || strings.Contains(output.String(), "private-error") {
		t.Fatalf("error policy: %s", output.String())
	}
}

func TestRequestTimingRealTransport(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	}))
	defer upstream.Close()
	gateway := NewGateway(upstream.Client())
	defer gateway.Close()
	gateway.ConfigureTiming(slog.New(slog.NewJSONHandler(io.Discard, nil)), TimingOptions{Enabled: true, SlowThreshold: time.Hour})
	ctx, timing := gateway.BeginRequestTiming(context.Background(), "request", "/responses")
	for range 2 {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, upstream.URL, strings.NewReader("body"))
		resp, err := upstream.Client().Do(traceTimingRequest(req, 4))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	first := timing.attempts[0].snapshot(timing.started)
	second := timing.attempts[1].snapshot(timing.started)
	if _, ok := first["tls_done_ms"]; !ok || second["connection_reused"] != true {
		t.Fatalf("connection trace: first=%v second=%v", first, second)
	}
	for _, snapshot := range []map[string]any{first, second} {
		for _, name := range []string{"request_written_ms", "first_response_byte_ms"} {
			if _, ok := snapshot[name]; !ok {
				t.Errorf("missing real callback %s", name)
			}
		}
	}
}

type timingDelayedWriter struct {
	*httptest.ResponseRecorder
	delayWrite bool
	delayFlush bool
	failWrite  bool
}

func (writer *timingDelayedWriter) Write(payload []byte) (int, error) {
	if writer.delayWrite {
		writer.delayWrite = false
		time.Sleep(30 * time.Millisecond)
	}
	if writer.failWrite {
		return 0, io.ErrClosedPipe
	}
	return writer.ResponseRecorder.Write(payload)
}

func (writer *timingDelayedWriter) Flush() {
	if writer.delayFlush {
		writer.delayFlush = false
		time.Sleep(30 * time.Millisecond)
	}
	writer.ResponseRecorder.Flush()
}

func TestRequestTimingSeparatesDownstreamWait(t *testing.T) {
	for _, mode := range []string{"write_before_content", "flush_before_content", "failed_write", "flush_after_content", "no_content"} {
		t.Run(mode, func(t *testing.T) {
			gateway := NewGateway(http.DefaultClient)
			defer gateway.Close()
			gateway.ConfigureTiming(slog.New(slog.NewJSONHandler(io.Discard, nil)), TimingOptions{Enabled: true, SlowThreshold: time.Hour})
			ctx, timing := gateway.BeginRequestTiming(context.Background(), "request", "/responses")
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test", nil)
			req = traceTimingRequest(req, 0)
			payload := ""
			if mode != "flush_after_content" {
				payload += "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"message\",\"content\":[]}}\n\n"
			}
			if mode != "no_content" {
				payload += "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ready immediately\"}\n\n"
			}
			payload += "data: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n"
			upstream := &http.Response{Request: req, StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}
			writer := &timingDelayedWriter{
				ResponseRecorder: httptest.NewRecorder(),
				delayWrite:       mode == "write_before_content" || mode == "failed_write",
				delayFlush:       mode == "flush_before_content" || mode == "flush_after_content" || mode == "no_content",
				failWrite:        mode == "failed_write",
			}
			metrics, err := CopyResponseForRequest(writer, upstream, time.Now(), true)
			if err != nil {
				t.Fatal(err)
			}
			if metrics.ClientDisconnected != (mode == "failed_write") {
				t.Fatalf("delivery behavior changed: %+v", metrics)
			}
			fields := timing.attempts[0].snapshot(timing.started)
			downstream := fields["downstream_before_content_ms"].(int64)
			if mode == "flush_after_content" {
				if downstream != 0 {
					t.Fatalf("counted post-content write: %v", fields)
				}
			} else if downstream < 25 {
				t.Fatalf("missing downstream wait: %v", fields)
			}
			if mode == "no_content" {
				if _, ok := fields["first_content_excluding_downstream_ms"]; ok {
					t.Fatalf("invented content event: %v", fields)
				}
			} else {
				content := fields["first_content_ms"].(int64)
				adjusted := fields["first_content_excluding_downstream_ms"].(int64)
				if adjusted < 0 || content-adjusted < downstream-1 || content-adjusted > downstream+1 {
					t.Fatalf("incorrect stage separation: %v", fields)
				}
			}
		})
	}
}

func TestRequestTimingKeepsPerAttemptCorrelation(t *testing.T) {
	var output bytes.Buffer
	gateway := NewGateway(http.DefaultClient)
	defer gateway.Close()
	gateway.ConfigureTiming(slog.New(slog.NewJSONHandler(&output, nil)), TimingOptions{Enabled: true, SlowThreshold: time.Hour})
	ctx, timing := gateway.BeginRequestTiming(context.Background(), "request", "/responses")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test", nil)
	for _, metric := range []domain.GatewayMetric{
		{RequestID: "failed-request", PlanID: "plan-a", APIKeyID: "key", AccountID: "account-a", StatusCode: 502},
		{RequestID: "successful-request", PlanID: "plan-b", APIKeyID: "key", AccountID: "account-b", StatusCode: 200},
	} {
		traceTimingRequest(req, 20)
		RecordRequestTimingMetric(ctx, metric)
	}
	RecordRequestTimingMetric(ctx, domain.GatewayMetric{RequestID: "policy-error", PlanID: "plan-c", AccountID: "account-c", StatusCode: 403})
	timing.Finish()
	var summary struct {
		AccountID string `json:"account_id"`
		Attempts  []struct {
			RequestID string `json:"metric_request_id"`
			PlanID    string `json:"plan_id"`
			APIKeyID  string `json:"api_key_id"`
			AccountID string `json:"account_id"`
			Status    int    `json:"metric_status"`
		} `json:"attempts"`
	}
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.AccountID != "account-c" || len(summary.Attempts) != 2 {
		t.Fatalf("summary = %+v", summary)
	}
	first, second := summary.Attempts[0], summary.Attempts[1]
	if first.RequestID != "failed-request" || first.PlanID != "plan-a" || first.AccountID != "account-a" || first.Status != 502 || first.APIKeyID != "key" {
		t.Fatalf("first attempt overwritten: %+v", first)
	}
	if second.RequestID != "successful-request" || second.PlanID != "plan-b" || second.AccountID != "account-b" || second.Status != 200 || second.APIKeyID != "key" {
		t.Fatalf("second attempt overwritten by pre-forward error: %+v", second)
	}
}

func BenchmarkRequestTiming(b *testing.B) {
	for _, mode := range []string{"disabled", "enabled_unsampled", "enabled_logged"} {
		b.Run(mode, func(b *testing.B) {
			gateway := NewGateway(http.DefaultClient)
			defer gateway.Close()
			options := TimingOptions{Enabled: mode != "disabled", SlowThreshold: time.Hour}
			if mode == "enabled_logged" {
				options.SampleEvery = 1
			}
			gateway.ConfigureTiming(slog.New(slog.NewJSONHandler(io.Discard, nil)), options)
			req, _ := http.NewRequest(http.MethodPost, "https://example.test", nil)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				ctx, timing := gateway.BeginRequestTiming(context.Background(), "request", "/responses")
				active := req
				if timing != nil {
					active = req.WithContext(ctx)
				}
				traced := traceTimingRequest(active, 1024)
				if trace := httptrace.ContextClientTrace(traced.Context()); trace != nil {
					trace.GetConn("host")
					trace.GotConn(httptrace.GotConnInfo{Reused: true})
					trace.WroteRequest(httptrace.WroteRequestInfo{})
					trace.GotFirstResponseByte()
				}
				RecordRequestTimingMetric(ctx, domain.GatewayMetric{StatusCode: 200})
				timing.Finish()
			}
		})
	}
}
