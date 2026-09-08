package openai

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type TimingOptions struct {
	Enabled       bool
	SlowThreshold time.Duration
	SampleEvery   int
}

type timingPolicy struct {
	options TimingOptions
	logger  *slog.Logger
	count   atomic.Uint64
}

type requestTimingKey struct{}
type attemptTimingKey struct{}

type RequestTiming struct {
	policy        *timingPolicy
	started       time.Time
	requestID     string
	endpoint      string
	bodyRead      time.Duration
	bodyBytes     int
	metric        domain.GatewayMetric
	hadError      bool
	attempts      []*attemptTiming
	attemptCount  int
	activeAttempt *attemptTiming
}

type attemptTiming struct {
	mu                      sync.Mutex
	started                 time.Time
	bodyBytes               int
	marks                   map[string]time.Time
	reused                  bool
	writes                  int
	writeFailed             bool
	status                  int
	downstreamBeforeContent time.Duration
	result                  *attemptTimingResult
}

type attemptTimingResult struct {
	requestID string
	planID    string
	apiKeyID  string
	accountID string
	status    int
}

func (g *Gateway) ConfigureTiming(logger *slog.Logger, options TimingOptions) {
	if !options.Enabled {
		g.timing.Store(nil)
		return
	}
	g.timing.Store(&timingPolicy{options: options, logger: logger})
}

func (g *Gateway) BeginRequestTiming(ctx context.Context, requestID, endpoint string) (context.Context, *RequestTiming) {
	policy := g.timing.Load()
	if policy == nil {
		return ctx, nil
	}
	timing := &RequestTiming{policy: policy, started: time.Now(), requestID: boundedTimingText(requestID), endpoint: endpoint}
	return context.WithValue(ctx, requestTimingKey{}, timing), timing
}

func (timing *RequestTiming) BodyRead(started time.Time, bytes int) {
	if timing != nil {
		timing.bodyRead = time.Since(started)
		timing.bodyBytes = bytes
	}
}

func RecordRequestTimingMetric(ctx context.Context, metric domain.GatewayMetric) {
	if timing, ok := ctx.Value(requestTimingKey{}).(*RequestTiming); ok {
		timing.metric = metric
		timing.hadError = timing.hadError || metric.StatusCode >= 400
		if attempt := timing.activeAttempt; attempt != nil {
			attempt.mu.Lock()
			attempt.result = &attemptTimingResult{
				requestID: boundedTimingText(metric.RequestID), planID: metric.PlanID,
				apiKeyID: metric.APIKeyID, accountID: metric.AccountID, status: metric.StatusCode,
			}
			attempt.mu.Unlock()
			timing.activeAttempt = nil
		}
	}
}

func (timing *RequestTiming) Finish() {
	if timing == nil {
		return
	}
	duration := time.Since(timing.started)
	slow := duration >= timing.policy.options.SlowThreshold
	sampled := false
	if !slow && !timing.hadError && timing.policy.options.SampleEvery > 0 {
		sampled = timing.policy.count.Add(1)%uint64(timing.policy.options.SampleEvery) == 0
	}
	if !slow && !timing.hadError && !sampled {
		return
	}
	attempts := make([]map[string]any, 0, len(timing.attempts))
	for _, attempt := range timing.attempts {
		attempts = append(attempts, attempt.snapshot(timing.started))
	}
	timing.policy.logger.Info("gateway request timing",
		"request_id", timing.requestID, "metric_request_id", boundedTimingText(timing.metric.RequestID),
		"endpoint", timing.endpoint, "plan_id", timing.metric.PlanID,
		"api_key_id", timing.metric.APIKeyID, "account_id", timing.metric.AccountID,
		"last_metric_status", timing.metric.StatusCode, "had_error", timing.hadError,
		"slow", slow, "sampled", sampled, "total_ms", duration.Milliseconds(),
		"body_read_ms", timing.bodyRead.Milliseconds(), "body_bytes", timing.bodyBytes,
		"attempt_count", timing.attemptCount, "attempts", attempts)
}

func boundedTimingText(value string) string {
	return strings.Map(func(char rune) rune {
		if char < 32 || char == 127 {
			return -1
		}
		return char
	}, value[:min(len(value), 128)])
}

func traceTimingRequest(req *http.Request, bodyBytes int) *http.Request {
	timing, ok := req.Context().Value(requestTimingKey{}).(*RequestTiming)
	if !ok {
		return req
	}
	attempt := &attemptTiming{started: time.Now(), bodyBytes: bodyBytes, marks: make(map[string]time.Time, 10)}
	timing.activeAttempt = attempt
	timing.attemptCount++
	if len(timing.attempts) < 16 {
		timing.attempts = append(timing.attempts, attempt)
	}
	trace := &httptrace.ClientTrace{
		GetConn: func(string) { attempt.mark("get_conn_ms", time.Now()) },
		GotConn: func(info httptrace.GotConnInfo) {
			attempt.mu.Lock()
			attempt.marks["got_conn_ms"] = time.Now()
			attempt.reused = info.Reused
			attempt.mu.Unlock()
		},
		DNSStart:          func(httptrace.DNSStartInfo) { attempt.mark("dns_start_ms", time.Now()) },
		DNSDone:           func(httptrace.DNSDoneInfo) { attempt.mark("dns_done_ms", time.Now()) },
		TLSHandshakeStart: func() { attempt.mark("tls_start_ms", time.Now()) },
		TLSHandshakeDone:  func(tls.ConnectionState, error) { attempt.mark("tls_done_ms", time.Now()) },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			attempt.mu.Lock()
			attempt.writes++
			attempt.writeFailed = info.Err != nil
			if info.Err == nil {
				attempt.marks["request_written_ms"] = time.Now()
			} else {
				delete(attempt.marks, "request_written_ms")
			}
			attempt.mu.Unlock()
		},
		GotFirstResponseByte: func() { attempt.mark("first_response_byte_ms", time.Now()) },
	}
	ctx := context.WithValue(req.Context(), attemptTimingKey{}, attempt)
	return req.WithContext(httptrace.WithClientTrace(ctx, trace))
}

func responseTiming(src *http.Response) *attemptTiming {
	if src.Request == nil {
		return nil
	}
	timing, _ := src.Request.Context().Value(attemptTimingKey{}).(*attemptTiming)
	return timing
}

func (attempt *attemptTiming) mark(name string, at time.Time) {
	if attempt != nil {
		attempt.mu.Lock()
		attempt.marks[name] = at
		attempt.mu.Unlock()
	}
}

func (attempt *attemptTiming) snapshot(started time.Time) map[string]any {
	attempt.mu.Lock()
	defer attempt.mu.Unlock()
	fields := map[string]any{
		"start_ms": attempt.started.Sub(started).Milliseconds(), "request_bytes": attempt.bodyBytes,
		"connection_reused": attempt.reused, "write_callbacks": attempt.writes,
		"write_failed": attempt.writeFailed, "upstream_status": attempt.status,
		"downstream_before_content_ms": attempt.downstreamBeforeContent.Milliseconds(),
	}
	if attempt.result != nil {
		fields["metric_request_id"] = attempt.result.requestID
		fields["plan_id"] = attempt.result.planID
		fields["api_key_id"] = attempt.result.apiKeyID
		fields["account_id"] = attempt.result.accountID
		fields["metric_status"] = attempt.result.status
	}
	if content, ok := attempt.marks["first_content_ms"]; ok {
		fields["first_content_excluding_downstream_ms"] = (content.Sub(attempt.started) - attempt.downstreamBeforeContent).Milliseconds()
	}
	for name, at := range attempt.marks {
		fields[name] = at.Sub(attempt.started).Milliseconds()
	}
	return fields
}

func (attempt *attemptTiming) recordDownstreamBeforeContent(started time.Time) {
	duration := time.Since(started)
	attempt.mu.Lock()
	attempt.downstreamBeforeContent += duration
	attempt.mu.Unlock()
}
