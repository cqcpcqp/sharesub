package openai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

const (
	chatGPTRateLimitResetCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	chatGPTRateLimitResetConsumeURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"
	quotaResetRequestTimeout        = 20 * time.Second
	maxQuotaResetResponseBytes      = 1 << 20
	defaultQuotaResetQueryBackoff   = 10 * time.Second
	maximumQuotaResetQueryBackoff   = time.Minute
)

type QuotaResetUpstreamError struct {
	Operation       string
	StatusCode      int
	RetryAfter      time.Duration
	XRequestID      string
	OpenAIRequestID string
	LocallyDeferred bool
}

func (e *QuotaResetUpstreamError) Error() string {
	return fmt.Sprintf("%s quota reset request returned status %d", e.Operation, e.StatusCode)
}

type quotaResetCreditPayload struct {
	ResetType string    `json:"reset_type"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}

type quotaResetCreditsPayload struct {
	AvailableCount int                       `json:"available_count"`
	Credits        []quotaResetCreditPayload `json:"credits"`
}

func (g *Gateway) QueryQuotaResetCredits(ctx context.Context, accessToken, chatgptAccountID, proxyURL string) (domain.QuotaResetCredits, error) {
	if retryAfter := g.quotaResetQueryRetryAfter(chatgptAccountID); retryAfter > 0 {
		return domain.QuotaResetCredits{}, &QuotaResetUpstreamError{Operation: "query", StatusCode: http.StatusTooManyRequests, RetryAfter: retryAfter, LocallyDeferred: true}
	}
	requestCtx, cancel := context.WithTimeout(ctx, quotaResetRequestTimeout)
	defer cancel()
	req, err := newQuotaResetRequest(requestCtx, http.MethodGet, g.quotaResetCreditsURL, accessToken, chatgptAccountID, nil)
	if err != nil {
		return domain.QuotaResetCredits{}, err
	}
	resp, err := g.doQuotaResetRequest(req, proxyURL)
	if err != nil {
		return domain.QuotaResetCredits{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		upstreamErr := newQuotaResetUpstreamError("query", resp, g.now())
		if upstreamErr.StatusCode == http.StatusTooManyRequests {
			g.backoffQuotaResetQuery(chatgptAccountID, upstreamErr.RetryAfter)
		}
		return domain.QuotaResetCredits{}, upstreamErr
	}
	var payload quotaResetCreditsPayload
	if err := decodeQuotaResetResponse(resp.Body, &payload); err != nil {
		return domain.QuotaResetCredits{}, fmt.Errorf("decode quota reset credits: %w", err)
	}
	credits := make([]domain.QuotaResetCredit, 0, len(payload.Credits))
	for _, credit := range payload.Credits {
		if credit.ResetType == "codex_rate_limits" && credit.Status == "available" {
			credits = append(credits, domain.QuotaResetCredit{ExpiresAt: credit.ExpiresAt})
		}
	}
	return domain.QuotaResetCredits{AvailableCount: len(credits), Credits: credits, FetchedAt: g.now()}, nil
}

func newQuotaResetUpstreamError(operation string, resp *http.Response, now time.Time) *QuotaResetUpstreamError {
	err := &QuotaResetUpstreamError{Operation: operation, StatusCode: resp.StatusCode, XRequestID: strings.TrimSpace(resp.Header.Get("X-Request-Id")), OpenAIRequestID: strings.TrimSpace(resp.Header.Get("Openai-Request-Id"))}
	if resp.StatusCode == http.StatusTooManyRequests {
		err.RetryAfter = quotaResetRetryDelay(resp.Header.Get("Retry-After"), now)
	}
	return err
}
func quotaResetRetryDelay(value string, now time.Time) time.Duration {
	delay := defaultQuotaResetQueryBackoff
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds > 0 {
		delay = time.Duration(seconds) * time.Second
	} else if retryAt, err := http.ParseTime(strings.TrimSpace(value)); err == nil && retryAt.After(now) {
		delay = retryAt.Sub(now)
	}
	if delay > maximumQuotaResetQueryBackoff {
		return maximumQuotaResetQueryBackoff
	}
	return delay
}
func (g *Gateway) quotaResetQueryRetryAfter(accountID string) time.Duration {
	now := g.now()
	g.quotaResetMu.Lock()
	defer g.quotaResetMu.Unlock()
	for id, until := range g.quotaResetBackoffs {
		if !now.Before(until) {
			delete(g.quotaResetBackoffs, id)
		}
	}
	return g.quotaResetBackoffs[accountID].Sub(now)
}
func (g *Gateway) backoffQuotaResetQuery(accountID string, delay time.Duration) {
	if strings.TrimSpace(accountID) == "" || delay <= 0 {
		return
	}
	now := g.now()
	g.quotaResetMu.Lock()
	defer g.quotaResetMu.Unlock()
	if g.quotaResetBackoffs == nil {
		g.quotaResetBackoffs = make(map[string]time.Time)
	}
	until := now.Add(delay)
	if until.After(g.quotaResetBackoffs[accountID]) {
		g.quotaResetBackoffs[accountID] = until
	}
}

func (g *Gateway) ConsumeQuotaResetCredit(ctx context.Context, accessToken, chatgptAccountID, proxyURL string) (domain.QuotaResetResult, error) {
	redeemRequestID, err := generateRedeemRequestID()
	if err != nil {
		return domain.QuotaResetResult{}, err
	}
	body, err := json.Marshal(map[string]string{"redeem_request_id": redeemRequestID})
	if err != nil {
		return domain.QuotaResetResult{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, quotaResetRequestTimeout)
	defer cancel()
	req, err := newQuotaResetRequest(requestCtx, http.MethodPost, g.quotaResetConsumeURL, accessToken, chatgptAccountID, body)
	if err != nil {
		return domain.QuotaResetResult{}, err
	}
	resp, err := g.doQuotaResetRequest(req, proxyURL)
	if err != nil {
		return domain.QuotaResetResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.QuotaResetResult{}, fmt.Errorf("consume quota reset credit returned status %d", resp.StatusCode)
	}
	var result domain.QuotaResetResult
	if err := decodeQuotaResetResponse(resp.Body, &result); err != nil {
		return domain.QuotaResetResult{}, fmt.Errorf("decode quota reset result: %w", err)
	}
	return result, nil
}

func newQuotaResetRequest(ctx context.Context, method, targetURL, accessToken, chatgptAccountID string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Chatgpt-Account-Id", chatgptAccountID)
	req.Header.Set("OpenAI-Beta", "codex-1")
	req.Header.Set("OAI-Language", "zh-CN")
	applyCodexOAuthIdentity(req.Header, "")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-Mode", "no-cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Priority", "u=4, i")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (g *Gateway) doQuotaResetRequest(req *http.Request, proxyURL string) (*http.Response, error) {
	client, err := g.clientForProxy(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("configure account proxy: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request quota reset service: %w", err)
	}
	return resp, nil
}

func decodeQuotaResetResponse(body io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxQuotaResetResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxQuotaResetResponseBytes {
		return fmt.Errorf("response exceeds %d bytes", maxQuotaResetResponseBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("response must contain one JSON object")
	}
	return nil
}

func generateRedeemRequestID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw)
	return strings.Join([]string{encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:]}, "-"), nil
}
