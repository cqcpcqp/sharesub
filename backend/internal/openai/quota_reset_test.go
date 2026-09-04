package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestQueryQuotaResetCreditsUsesWhamContract(t *testing.T) {
	expiresAt := time.Date(2026, 8, 12, 5, 9, 0, 0, time.UTC)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/backend-api/wham/rate-limit-reset-credits" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		assertQuotaResetHeaders(t, r)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"available_count":2,"credits":[{"reset_type":"codex_rate_limits","status":"available","expires_at":"2026-08-12T05:09:00Z"},{"reset_type":"codex_rate_limits","status":"redeemed","expires_at":"2026-08-11T05:09:00Z"},{"reset_type":"other","status":"available","expires_at":"2026-08-13T05:09:00Z"}]}`)),
			Request:    r,
		}, nil
	})}

	gateway := NewGateway(client)
	gateway.now = func() time.Time { return time.Date(2026, 8, 6, 11, 30, 0, 0, time.UTC) }
	credits, err := gateway.QueryQuotaResetCredits(context.Background(), "access-token", "account-id", "")
	if err != nil {
		t.Fatal(err)
	}
	if credits.AvailableCount != 1 || len(credits.Credits) != 1 || !credits.Credits[0].ExpiresAt.Equal(expiresAt) {
		t.Fatalf("credits = %+v", credits)
	}
}

func TestConsumeQuotaResetCreditSendsIdempotencyID(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/backend-api/wham/rate-limit-reset-credits/consume" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		assertQuotaResetHeaders(t, r)
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q", got)
		}
		var input struct {
			RedeemRequestID string `json:"redeem_request_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(input.RedeemRequestID) {
			t.Fatalf("redeem_request_id = %q", input.RedeemRequestID)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"ok","credit":{"id":"credit-1","reset_type":"codex_rate_limits","status":"redeemed","granted_at":"2026-08-01T00:00:00Z","expires_at":"2026-08-12T05:09:00Z","redeem_started_at":"2026-08-06T11:30:00Z","redeemed_at":"2026-08-06T11:30:01Z"},"windows_reset":2}`)),
			Request:    r,
		}, nil
	})}

	gateway := NewGateway(client)
	result, err := gateway.ConsumeQuotaResetCredit(context.Background(), "access-token", "account-id", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "ok" || result.WindowsReset != 2 || result.Credit == nil || result.Credit.ID != "credit-1" {
		t.Fatalf("result = %+v", result)
	}
}

func TestQuotaResetEndpointsRejectUpstreamErrors(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("denied")), Request: r}, nil
	})}
	gateway := NewGateway(client)
	if _, err := gateway.QueryQuotaResetCredits(context.Background(), "access-token", "account-id", ""); err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("query error = %v", err)
	}
	if _, err := gateway.ConsumeQuotaResetCredit(context.Background(), "access-token", "account-id", ""); err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("reset error = %v", err)
	}
}

func TestQueryQuotaResetCreditsBacksOffUpstreamRateLimit(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{
			"Retry-After": []string{"30"}, "X-Request-Id": []string{"req-1"},
		}, Body: io.NopCloser(strings.NewReader("rate limited")), Request: r}, nil
	})}
	gateway := NewGateway(client)
	now := time.Date(2026, 9, 4, 2, 0, 0, 0, time.UTC)
	gateway.now = func() time.Time { return now }
	_, err := gateway.QueryQuotaResetCredits(context.Background(), "access-token", "account-id", "")
	var upstreamErr *QuotaResetUpstreamError
	if !strings.Contains(err.Error(), "status 429") || !errors.As(err, &upstreamErr) || upstreamErr.RetryAfter != 30*time.Second || upstreamErr.XRequestID != "req-1" {
		t.Fatalf("error = %v", err)
	}
	now = now.Add(time.Second)
	_, err = gateway.QueryQuotaResetCredits(context.Background(), "access-token", "account-id", "")
	if !errors.As(err, &upstreamErr) || !upstreamErr.LocallyDeferred || calls != 1 {
		t.Fatalf("deferred error = %v, calls = %d", err, calls)
	}
}

func assertQuotaResetHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	want := map[string]string{
		"Authorization":      "Bearer access-token",
		"Chatgpt-Account-Id": "account-id",
		"OpenAI-Beta":        "codex-1",
		"OAI-Language":       "zh-CN",
		"Originator":         codexDefaultOriginator,
		"Version":            codexProbeVersion,
		"User-Agent":         codexProbeUserAgent,
		"Accept":             "application/json",
		"Sec-Fetch-Site":     "none",
		"Sec-Fetch-Mode":     "no-cors",
		"Sec-Fetch-Dest":     "empty",
		"Priority":           "u=4, i",
	}
	for key, value := range want {
		if got := r.Header.Get(key); got != value {
			t.Fatalf("%s = %q, want %q", key, got, value)
		}
	}
}
