package openai

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

const (
	codexResponsesURL        = "https://chatgpt.com/backend-api/codex/responses"
	codexCompactURL          = codexResponsesURL + "/compact"
	codexModelsURL           = "https://chatgpt.com/backend-api/codex/models"
	chatGPTUsageURL          = "https://chatgpt.com/backend-api/wham/usage"
	maxBufferedResponseBytes = 64 << 20
	maxProxyClients          = 16
	proxyClientTTL           = 30 * time.Minute
)

const codexProbeTimeout = 20 * time.Second

var requestHeaderAllowlist = map[string]struct{}{
	"accept": {}, "accept-language": {}, "content-type": {}, "conversation-id": {}, "conversation_id": {},
	"openai-beta": {}, "originator": {}, "session-id": {}, "session_id": {}, "thread-id": {}, "user-agent": {}, "version": {},
	"x-client-request-id": {}, "x-codex-beta-features": {}, "x-codex-installation-id": {}, "x-codex-turn-state": {}, "x-codex-turn-metadata": {}, "x-codex-window-id": {},
}

var responseHeaders = []string{
	"Content-Type", "Cache-Control", "Retry-After", "X-Request-Id", "Openai-Request-Id", "Openai-Processing-Ms", "Openai-Version", "X-Codex-Turn-State",
	"X-Codex-Primary-Used-Percent", "X-Codex-Primary-Reset-After-Seconds", "X-Codex-Primary-Window-Minutes",
	"X-Codex-Secondary-Used-Percent", "X-Codex-Secondary-Reset-After-Seconds", "X-Codex-Secondary-Window-Minutes",
	"X-Codex-Primary-Over-Secondary-Limit-Percent",
	"X-Ratelimit-Limit-Requests", "X-Ratelimit-Limit-Tokens", "X-Ratelimit-Remaining-Requests", "X-Ratelimit-Remaining-Tokens",
	"X-Ratelimit-Reset-Requests", "X-Ratelimit-Reset-Tokens",
}

var chatGPTUnsupportedFields = []string{
	"max_output_tokens", "max_completion_tokens", "temperature", "top_p", "frequency_penalty", "presence_penalty",
	"user", "metadata", "prompt_cache_retention", "safety_identifier", "stream_options",
}

var compactRequestFields = []string{
	"model", "input", "instructions", "tools", "parallel_tool_calls", "reasoning", "text", "previous_response_id",
}

var ErrIncompleteStream = errors.New("upstream stream ended before a terminal response event")

type Gateway struct {
	httpClient           *http.Client
	proxyMu              sync.Mutex
	proxyClients         map[string]*proxyClientEntry
	now                  func() time.Time
	quotaResetCreditsURL string
	quotaResetConsumeURL string
}

type proxyClientEntry struct {
	client   *http.Client
	lastUsed time.Time
}

type codexQuotaUsage struct {
	RateLimit codexQuotaRateLimit `json:"rate_limit"`
}

type codexQuotaRateLimit struct {
	PrimaryWindow   *codexQuotaWindow `json:"primary_window"`
	SecondaryWindow *codexQuotaWindow `json:"secondary_window"`
}

type codexQuotaWindow struct {
	UsedPercent       float64 `json:"used_percent"`
	WindowSeconds     int64   `json:"limit_window_seconds"`
	ResetAfterSeconds int64   `json:"reset_after_seconds"`
	ResetAt           int64   `json:"reset_at"`
}

type RequestBilling struct {
	Model          string
	ServiceTier    string
	PromptCacheKey string
	Stream         bool
}

func ParseRequestBilling(body []byte) (RequestBilling, error) {
	_, metadata, err := prepareRequest(body, false, false)
	return metadata, err

}

// PrepareRequest validates and normalizes a Responses request for the
// ChatGPT Codex internal API. Unknown request fields are preserved on the
// regular endpoint and the compact endpoint is reduced to its documented
// schema.
func PrepareRequest(body []byte, compact bool) ([]byte, RequestBilling, error) {
	return prepareRequest(body, compact, true)

}

func prepareRequest(body []byte, compact, normalize bool) ([]byte, RequestBilling, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, RequestBilling{}, fmt.Errorf("parse Codex request: %w", err)
	}
	if payload == nil {
		return nil, RequestBilling{}, fmt.Errorf("Codex request must be a JSON object")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, RequestBilling{}, fmt.Errorf("Codex request must contain one JSON object")
	}

	model, ok := payload["model"].(string)
	model = strings.TrimSpace(model)
	if !ok || model == "" {
		return nil, RequestBilling{}, fmt.Errorf("Codex request model is required")
	}
	metadata := RequestBilling{Model: model}
	if value, exists := payload["service_tier"]; exists {
		serviceTier, ok := value.(string)
		if !ok {
			return nil, RequestBilling{}, fmt.Errorf("Codex request service_tier must be a string")
		}
		metadata.ServiceTier = strings.TrimSpace(serviceTier)
	}
	if value, exists := payload["prompt_cache_key"]; exists {
		promptCacheKey, ok := value.(string)
		if !ok {
			return nil, RequestBilling{}, fmt.Errorf("Codex request prompt_cache_key must be a string")
		}
		metadata.PromptCacheKey = strings.TrimSpace(promptCacheKey)
	}
	if value, exists := payload["stream"]; exists {
		stream, ok := value.(bool)
		if !ok {
			return nil, RequestBilling{}, fmt.Errorf("Codex request stream must be a boolean")
		}
		metadata.Stream = stream
	}
	if !normalize {
		return body, metadata, nil
	}
	if instructions, exists := payload["instructions"]; exists && strings.Contains(strings.ToLower(model), "codex") {
		value, ok := instructions.(string)
		if !ok {
			return nil, RequestBilling{}, fmt.Errorf("Codex request instructions must be a string")
		}
		if strings.TrimSpace(value) == "" {
			return nil, RequestBilling{}, fmt.Errorf("Codex request instructions must not be empty")
		}
	}

	for _, field := range chatGPTUnsupportedFields {
		delete(payload, field)
	}
	normalizeCodexInput(payload)
	normalizeCodexToolParameterTypes(payload)
	if compact {
		compactPayload := make(map[string]any, len(compactRequestFields))
		for _, field := range compactRequestFields {
			if value, exists := payload[field]; exists {
				compactPayload[field] = value
			}
		}
		payload = compactPayload
	} else {
		payload["store"] = false
		// ChatGPT's Codex endpoint is an SSE upstream. Non-streaming callers
		// are converted back to JSON by CopyResponseForRequest.
		payload["stream"] = true
		ensureCodexReasoningInclude(payload)
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, RequestBilling{}, fmt.Errorf("normalize Codex request: %w", err)
	}
	return normalized, metadata, nil
}

func normalizeCodexInput(payload map[string]any) {
	input, exists := payload["input"]
	if !exists {
		return
	}
	switch value := input.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			payload["input"] = []any{}
			return
		}
		payload["input"] = []any{map[string]any{
			"type": "message", "role": "user", "content": value,
		}}
	case map[string]any:
		payload["input"] = []any{value}
	}
}

const maxCodexNestedToolDepth = 4

// normalizeCodexToolParameterTypes repairs the explicit null schema emitted by
// affected Codex clients. A missing type remains untouched because it is valid
// JSON Schema and adding one would narrow the caller's contract.
func normalizeCodexToolParameterTypes(payload map[string]any) {
	var normalizeTools func(any, int)
	normalizeTools = func(value any, depth int) {
		if depth > maxCodexNestedToolDepth {
			return
		}
		tools, ok := value.([]any)
		if !ok {
			return
		}
		for _, value := range tools {
			tool, ok := value.(map[string]any)
			if !ok {
				continue
			}
			for _, container := range []map[string]any{tool, objectField(tool, "function")} {
				parameters := objectField(container, "parameters")
				if parameterType, exists := parameters["type"]; exists && parameterType == nil {
					parameters["type"] = "object"
				}
			}
			normalizeTools(tool["tools"], depth+1)
		}
	}
	normalizeTools(payload["tools"], 0)
	if input, ok := payload["input"].([]any); ok {
		for _, value := range input {
			if item, ok := value.(map[string]any); ok {
				normalizeTools(item["tools"], 0)
			}
		}
	}
}

func objectField(object map[string]any, key string) map[string]any {
	if object == nil {
		return nil
	}
	value, _ := object[key].(map[string]any)
	return value
}

func ensureCodexReasoningInclude(payload map[string]any) {
	reasoning, ok := payload["reasoning"].(map[string]any)
	if !ok || len(reasoning) == 0 {
		return
	}
	const encryptedContent = "reasoning.encrypted_content"
	include, exists := payload["include"]
	if !exists || include == nil {
		payload["include"] = []any{encryptedContent}
		return
	}
	values, ok := include.([]any)
	if !ok {
		return
	}
	for _, value := range values {
		if value == encryptedContent {
			return
		}
	}
	payload["include"] = append(values, encryptedContent)
}

func NewGateway(httpClient *http.Client) *Gateway {
	return &Gateway{
		httpClient:           httpClient,
		proxyClients:         make(map[string]*proxyClientEntry),
		now:                  time.Now,
		quotaResetCreditsURL: chatGPTRateLimitResetCreditsURL,
		quotaResetConsumeURL: chatGPTRateLimitResetConsumeURL,
	}
}

func (g *Gateway) Close() {
	g.proxyMu.Lock()
	defer g.proxyMu.Unlock()
	for key, entry := range g.proxyClients {
		closeClientIdleConnections(entry.client)
		delete(g.proxyClients, key)
	}
	closeClientIdleConnections(g.httpClient)
}

// ProbeQuota actively obtains the current Codex quota windows for an OAuth account.
func (g *Gateway) ProbeQuota(ctx context.Context, accessToken, chatgptAccountID, proxyURL string) ([]domain.QuotaSignal, error) {
	probeCtx, cancel := context.WithTimeout(ctx, codexProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, chatGPTUsageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create Codex quota probe: %w", err)
	}
	req.Host = "chatgpt.com"
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("OpenAI-Beta", "codex-1")
	req.Header.Set("OAI-Language", "zh-CN")
	applyCodexOAuthIdentity(req.Header, "")
	req.Header.Set("Priority", "u=4, i")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "no-cors")
	req.Header.Set("Sec-Fetch-Site", "none")
	if chatgptAccountID != "" {
		req.Header.Set("Chatgpt-Account-Id", chatgptAccountID)
	}

	client, err := g.clientForProxy(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("configure account proxy: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("probe Codex quota: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("probe Codex quota returned status %d", resp.StatusCode)
	}
	var usage codexQuotaUsage
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&usage); err != nil {
		return nil, fmt.Errorf("decode Codex quota response: %w", err)
	}
	signals := make([]domain.QuotaSignal, 0, 2)
	for _, window := range []*codexQuotaWindow{
		usage.RateLimit.PrimaryWindow,
		usage.RateLimit.SecondaryWindow,
	} {
		if window == nil {
			continue
		}
		signal := quotaSignalFromUsageWindow(*window)
		if signal.WindowType == "" {
			return nil, errors.New("probe Codex quota returned unknown quota window")
		}
		signals = append(signals, signal)
	}
	if len(signals) == 0 {
		return nil, errors.New("probe Codex quota returned no quota windows")
	}
	if len(signals) == 2 && signals[0].WindowType == signals[1].WindowType {
		return nil, errors.New("probe Codex quota returned duplicate quota windows")
	}
	return signals, nil
}

func quotaSignalFromUsageWindow(window codexQuotaWindow) domain.QuotaSignal {
	var kind string
	switch window.WindowSeconds {
	case int64(5 * time.Hour / time.Second):
		kind = domain.Window5H
	case int64(7 * 24 * time.Hour / time.Second):
		kind = domain.Window7D
	}
	resetAt := time.Unix(window.ResetAt, 0).UTC()
	return domain.QuotaSignal{
		WindowType:        kind,
		WindowStart:       resetAt.Add(-time.Duration(window.WindowSeconds) * time.Second),
		ResetAt:           resetAt,
		AccountUsedMicros: int64(window.UsedPercent * domain.PercentMicros),
	}
}

type CodexFingerprintContext struct {
	AccountID string
	Mode      string
}

func (g *Gateway) Forward(ctx context.Context, inbound *http.Request, body []byte, metadata RequestBilling, accessToken, chatGPTAccountID, apiKeyID, proxyURL string, fingerprintContext ...CodexFingerprintContext) (*http.Response, error) {
	compact := isCompactRequestPath(inbound.URL.Path)
	images := normalizeImagesEndpoint(inbound.URL.Path) != ""
	var err error
	clientSessionID := ClientCodexSessionID(inbound.Header, metadata.PromptCacheKey)
	if compact && clientSessionID == "" {
		clientSessionID, err = randomSessionSeed()
		if err != nil {
			return nil, fmt.Errorf("create compact session: %w", err)
		}
	}
	var fingerprint *CodexFingerprint
	fingerprintConfigured := len(fingerprintContext) > 0
	if fingerprintConfigured {
		fingerprint, err = ResolveCodexFingerprint(CodexFingerprintConfig{
			AccountID: fingerprintContext[0].AccountID, APIKeyID: apiKeyID, Mode: fingerprintContext[0].Mode, ClientSessionID: clientSessionID,
		})
	}
	if err != nil {
		return nil, err
	}
	if !compact {
		body, err = ApplyCodexFingerprintBody(body, fingerprint)
		if err != nil {
			return nil, err
		}
	}
	targetURL := codexResponsesURL
	if compact {
		targetURL = codexCompactURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Host = "chatgpt.com"
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Chatgpt-Account-Id", chatGPTAccountID)
	for key, values := range inbound.Header {
		if _, ok := requestHeaderAllowlist[strings.ToLower(key)]; !ok {
			continue
		}
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	// The legacy responses=experimental beta is no longer accepted by the
	// ChatGPT Codex OAuth HTTP endpoints. Preserve independent beta tokens sent
	// by the client while removing only that legacy token.
	stripLegacyResponsesBeta(req.Header)
	// Every supported gateway request is normalized to a JSON body before it
	// reaches Forward, including multipart Images edits.
	req.Header.Set("Content-Type", "application/json")
	if compact {
		req.Header.Set("Accept", "application/json")
	} else if images {
		req.Header.Set("Accept", "text/event-stream")
	} else if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "text/event-stream")
	}
	applyCodexOAuthIdentity(req.Header, "")
	applyCodexRoutingHint(req.Header, metadata.Model, metadata.ServiceTier)

	clientConversationID := strings.TrimSpace(req.Header.Get("Conversation_Id"))
	seed := clientSessionID
	if clientConversationID == "" && !compact {
		clientConversationID = seed
	}
	if !fingerprintConfigured {
		if clientSessionID != "" {
			req.Header.Set("Session_Id", isolateSession(apiKeyID, clientSessionID))
		}
		if clientConversationID != "" {
			req.Header.Set("Conversation_Id", isolateSession(apiKeyID, clientConversationID))
		}
	}
	if err := ApplyCodexFingerprintHeaders(req.Header, fingerprint); err != nil {
		return nil, err
	}
	client, err := g.clientForProxy(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("configure account proxy: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forward Codex request: %w", err)
	}
	return resp, nil
}

func stripLegacyResponsesBeta(headers http.Header) {
	values := headers.Values("OpenAI-Beta")
	headers.Del("OpenAI-Beta")
	for _, value := range values {
		kept := make([]string, 0, 2)
		for _, token := range strings.Split(value, ",") {
			token = strings.TrimSpace(token)
			if token == "" || strings.EqualFold(token, "responses=experimental") {
				continue
			}
			kept = append(kept, token)
		}
		if len(kept) > 0 {
			headers.Add("OpenAI-Beta", strings.Join(kept, ", "))
		}
	}
}

// FetchModels forwards Codex model discovery to the selected ChatGPT OAuth
// account. The upstream manifest is returned unchanged to the caller.
func (g *Gateway) FetchModels(ctx context.Context, inbound *http.Request, accessToken, accountID, proxyURL string) (*http.Response, error) {
	clientVersion := EffectiveCodexVersion()
	target, err := url.Parse(codexModelsURL)
	if err != nil {
		return nil, err
	}
	query := target.Query()
	query.Set("client_version", clientVersion)
	target.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Host = "chatgpt.com"
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if accountID != "" {
		req.Header.Set("Chatgpt-Account-Id", accountID)
	}
	req.Header.Set("Accept", "application/json")
	applyCodexOAuthIdentity(req.Header, clientVersion)
	if etag := strings.TrimSpace(inbound.Header.Get("If-None-Match")); etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	client, err := g.clientForProxy(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("configure account proxy: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch Codex models: %w", err)
	}
	return resp, nil
}

func isCompactRequestPath(path string) bool {
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	return strings.HasSuffix(path, "/responses/compact")
}

func randomSessionSeed() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func (g *Gateway) clientForProxy(rawURL string) (*http.Client, error) {
	if rawURL == "" {
		return g.httpClient, nil
	}
	g.proxyMu.Lock()
	defer g.proxyMu.Unlock()
	now := g.now()
	g.pruneProxyClients(now)
	if entry := g.proxyClients[rawURL]; entry != nil {
		entry.lastUsed = now
		return entry.client, nil
	}
	proxyURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	baseTransport, ok := g.httpClient.Transport.(*http.Transport)
	if !ok || baseTransport == nil {
		baseTransport = http.DefaultTransport.(*http.Transport)
	}
	transport := baseTransport.Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{Transport: transport, Timeout: g.httpClient.Timeout}
	if len(g.proxyClients) >= maxProxyClients {
		g.evictOldestProxyClient()
	}
	g.proxyClients[rawURL] = &proxyClientEntry{client: client, lastUsed: now}
	return client, nil
}

func (g *Gateway) pruneProxyClients(now time.Time) {
	for key, entry := range g.proxyClients {
		if now.Sub(entry.lastUsed) < proxyClientTTL {
			continue
		}
		closeClientIdleConnections(entry.client)
		delete(g.proxyClients, key)
	}
}

func (g *Gateway) evictOldestProxyClient() {
	var oldestKey string
	var oldestTime time.Time
	for key, entry := range g.proxyClients {
		if oldestKey == "" || entry.lastUsed.Before(oldestTime) {
			oldestKey, oldestTime = key, entry.lastUsed
		}
	}
	if oldestKey == "" {
		return
	}
	closeClientIdleConnections(g.proxyClients[oldestKey].client)
	delete(g.proxyClients, oldestKey)
}

func closeClientIdleConnections(client *http.Client) {
	if client == nil {
		return
	}
	if transport, ok := client.Transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}

func isolateSession(apiKeyID, value string) string {
	sum := sha256.Sum256([]byte(apiKeyID + ":" + value))
	return hex.EncodeToString(sum[:])
}
