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

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

const (
	codexResponsesURL        = "https://chatgpt.com/backend-api/codex/responses"
	codexCompactURL          = codexResponsesURL + "/compact"
	codexModelsURL           = "https://chatgpt.com/backend-api/codex/models"
	maxBufferedResponseBytes = 64 << 20
	maxProxyClients          = 16
	proxyClientTTL           = 30 * time.Minute
)

const (
	codexProbeModel     = "gpt-5.4"
	codexProbeTimeout   = 15 * time.Second
	codexProbeVersion   = "0.144.1"
	codexProbeUserAgent = "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color"
)

var requestHeaderAllowlist = map[string]struct{}{
	"accept": {}, "accept-language": {}, "content-type": {}, "conversation_id": {},
	"openai-beta": {}, "originator": {}, "session_id": {}, "user-agent": {}, "version": {},
	"x-codex-turn-state": {}, "x-codex-turn-metadata": {},
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

type codexProbePayload struct {
	Model  string              `json:"model"`
	Input  []codexProbeMessage `json:"input"`
	Stream bool                `json:"stream"`
	Store  bool                `json:"store"`
}

type codexProbeMessage struct {
	Role    string              `json:"role"`
	Content []codexProbeContent `json:"content"`
}

type codexProbeContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
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

	for _, field := range chatGPTUnsupportedFields {
		delete(payload, field)
	}
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
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, RequestBilling{}, fmt.Errorf("normalize Codex request: %w", err)
	}
	return normalized, metadata, nil
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
	payload, err := json.Marshal(codexProbePayload{
		Model: codexProbeModel,
		Input: []codexProbeMessage{{
			Role: "user",
			Content: []codexProbeContent{{
				Type: "input_text",
				Text: "hi",
			}},
		}},
		Stream: true,
		Store:  false,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal Codex quota probe: %w", err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, codexProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, codexResponsesURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create Codex quota probe: %w", err)
	}
	req.Host = "chatgpt.com"
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Version", codexProbeVersion)
	req.Header.Set("User-Agent", codexProbeUserAgent)
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

	signals := application.ParseCodexQuotaHeaders(resp.Header, time.Now())
	if hasCompleteQuotaWindows(signals) {
		return signals, nil
	}
	return nil, fmt.Errorf("probe Codex quota returned status %d without complete quota signals", resp.StatusCode)
}

func hasCompleteQuotaWindows(signals []domain.QuotaSignal) bool {
	if len(signals) != 2 {
		return false
	}
	first, second := signals[0].WindowType, signals[1].WindowType
	return (first == domain.Window5H && second == domain.Window7D) ||
		(first == domain.Window7D && second == domain.Window5H)
}

func (g *Gateway) Forward(ctx context.Context, inbound *http.Request, body []byte, metadata RequestBilling, accessToken, accountID, apiKeyID, proxyURL string) (*http.Response, error) {
	compact := isCompactRequestPath(inbound.URL.Path)
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
	req.Header.Set("Chatgpt-Account-Id", accountID)
	for key, values := range inbound.Header {
		if _, ok := requestHeaderAllowlist[strings.ToLower(key)]; !ok {
			continue
		}
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	if req.Header.Get("Originator") == "" {
		req.Header.Set("Originator", "codex_cli_rs")
	}
	// Every supported gateway request is normalized to a JSON body before it
	// reaches Forward, including multipart Images edits.
	req.Header.Set("Content-Type", "application/json")
	if compact {
		req.Header.Set("Accept", "application/json")
		if req.Header.Get("Version") == "" {
			req.Header.Set("Version", codexProbeVersion)
		}
	} else if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "text/event-stream")
	}
	if userAgent := strings.TrimSpace(req.Header.Get("User-Agent")); userAgent == "" || isBrowserUserAgent(userAgent) {
		req.Header.Set("User-Agent", codexProbeUserAgent)
	}

	clientSessionID := strings.TrimSpace(req.Header.Get("Session_Id"))
	clientConversationID := strings.TrimSpace(req.Header.Get("Conversation_Id"))
	seed := strings.TrimSpace(metadata.PromptCacheKey)
	if compact && clientSessionID == "" && seed == "" {
		seed, err = randomSessionSeed()
		if err != nil {
			return nil, fmt.Errorf("create compact session: %w", err)
		}
	}
	if clientSessionID == "" {
		clientSessionID = seed
	}
	if clientConversationID == "" && !compact {
		clientConversationID = seed
	}
	if clientSessionID != "" {
		req.Header.Set("Session_Id", isolateSession(apiKeyID, clientSessionID))
	}
	if clientConversationID != "" {
		req.Header.Set("Conversation_Id", isolateSession(apiKeyID, clientConversationID))
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

// FetchModels forwards Codex model discovery to the selected ChatGPT OAuth
// account. The upstream manifest is returned unchanged to the caller.
func (g *Gateway) FetchModels(ctx context.Context, inbound *http.Request, accessToken, accountID, proxyURL string) (*http.Response, error) {
	clientVersion := strings.TrimSpace(inbound.URL.Query().Get("client_version"))
	if clientVersion == "" {
		clientVersion = codexProbeVersion
	}
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
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Version", clientVersion)
	req.Header.Set("User-Agent", codexProbeUserAgent)
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

func isBrowserUserAgent(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "mozilla/")
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
