package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/sharesub/sharesub/backend/internal/security"
)

type timingFailedBody struct{}

func (timingFailedBody) Read([]byte) (int, error) { return 0, context.DeadlineExceeded }
func (timingFailedBody) Close() error             { return nil }

func TestGatewayTimingHandlerOutcomes(t *testing.T) {
	for _, mode := range []string{"success", "retry", "body_failure", "upstream_failure", "canceled"} {
		t.Run(mode, func(t *testing.T) {
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
			credential.AccessTokenCiphertext, err = manager.Encrypt("private-token", []byte("owner:chatgpt:access"))
			if err != nil {
				t.Fatal(err)
			}
			store := &gatewayHandlerStore{credential: credential}
			var calls int
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := &http.Client{Transport: gatewayTestRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if mode == "upstream_failure" {
					return nil, context.DeadlineExceeded
				}
				if mode == "canceled" {
					cancel()
					return nil, context.Canceled
				}
				body := `data: {"type":"response.completed","response":{"id":"resp_ok","model":"gpt-5.4","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":2,"output_tokens":1}}}` + "\n\n"
				if mode == "retry" && calls == 1 {
					body = `data: {"type":"response.failed","response":{"error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}` + "\n\n"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			var output bytes.Buffer
			gateway := openai.NewGateway(client)
			defer gateway.Close()
			gateway.ConfigureTiming(slog.New(slog.NewJSONHandler(&output, nil)), openai.TimingOptions{Enabled: true, SlowThreshold: time.Hour, SampleEvery: 1})
			server := New(application.NewService(store, manager, nil, 0, "", ""), gateway, slog.New(slog.NewJSONHandler(io.Discard, nil)))
			defer server.Close()
			server.requestScopedRetryDelay = func(int) time.Duration { return 0 }
			payload := `{"model":"gpt-5.4","input":"private-input","stream":true}`
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(payload)).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer sk-sharesub-private-key")
			req.Header.Set("X-Request-Id", "request")
			if mode == "body_failure" {
				req.Body = timingFailedBody{}
			}
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, req)
			if bytes.Count(output.Bytes(), []byte("\n")) != 1 {
				t.Fatalf("expected one summary: %s; status=%d body=%s", output.String(), recorder.Code, recorder.Body.String())
			}
			var summary struct {
				RequestID    string `json:"request_id"`
				PlanID       string `json:"plan_id"`
				APIKeyID     string `json:"api_key_id"`
				AccountID    string `json:"account_id"`
				HadError     bool   `json:"had_error"`
				Status       int    `json:"last_metric_status"`
				AttemptCount int    `json:"attempt_count"`
				BodyBytes    int    `json:"body_bytes"`
				Attempts     []struct {
					RequestID string `json:"metric_request_id"`
					AccountID string `json:"account_id"`
					PlanID    string `json:"plan_id"`
					Status    int    `json:"metric_status"`
				} `json:"attempts"`
			}
			if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
				t.Fatal(err)
			}
			if summary.RequestID != "request" || summary.PlanID != "plan" || summary.APIKeyID != "key" || summary.AccountID != "account" || summary.AttemptCount != calls {
				t.Fatalf("correlation failed: %+v", summary)
			}
			if mode == "success" || mode == "retry" {
				if recorder.Code != 200 || summary.Status != 200 || summary.BodyBytes != len(payload) || !strings.Contains(recorder.Body.String(), "resp_ok") {
					t.Fatalf("response changed: %+v, body=%s", summary, recorder.Body.String())
				}
			}
			if summary.HadError != (mode != "success") {
				t.Fatalf("error policy: %+v", summary)
			}
			if mode == "retry" && calls != 2 {
				t.Fatalf("retry calls = %d", calls)
			}
			if len(summary.Attempts) != calls {
				t.Fatalf("attempt summaries = %+v", summary.Attempts)
			}
			store.mu.Lock()
			metrics := append([]domain.GatewayMetric(nil), store.metrics...)
			store.mu.Unlock()
			for index, attempt := range summary.Attempts {
				if attempt.AccountID != "account" || attempt.PlanID != "plan" || attempt.RequestID != metrics[index].RequestID || attempt.Status != metrics[index].StatusCode {
					t.Fatalf("attempt %d correlation = %+v, metric = %+v", index, attempt, metrics[index])
				}
			}
			if mode == "canceled" && summary.Status != 499 {
				t.Fatalf("canceled status = %d", summary.Status)
			}
			for _, secret := range []string{"private-token", "private-key", "private-input"} {
				if strings.Contains(output.String(), secret) {
					t.Errorf("logged secret %s", secret)
				}
			}
		})
	}
}
