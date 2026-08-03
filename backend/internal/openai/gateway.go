package openai

import (
	"bufio"
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
	maxBufferedResponseBytes = 128 << 20
)

const (
	codexProbeModel     = "gpt-5.4"
	codexProbeTimeout   = 15 * time.Second
	codexProbeVersion   = "0.125.0"
	codexProbeUserAgent = "codex_cli_rs/0.125.0 (Ubuntu 22.4.0; x86_64) xterm-256color"
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
	httpClient   *http.Client
	proxyMu      sync.Mutex
	proxyClients map[string]*http.Client
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
	return &Gateway{httpClient: httpClient, proxyClients: make(map[string]*http.Client)}
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
	if len(signals) > 0 {
		return signals, nil
	}
	return nil, fmt.Errorf("probe Codex quota returned status %d without complete quota signals", resp.StatusCode)
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
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
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
	if client := g.proxyClients[rawURL]; client != nil {
		return client, nil
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
	g.proxyClients[rawURL] = client
	return client, nil
}

type ProxyMetrics struct {
	TTFT         time.Duration
	Duration     time.Duration
	InputTokens  int64
	OutputTokens int64
	CachedTokens int64
}

func CopyResponse(dst http.ResponseWriter, src *http.Response, startedAt time.Time) (ProxyMetrics, error) {
	return CopyResponseForRequest(dst, src, startedAt, true)
}

// CopyResponseForRequest preserves streaming Responses payloads and converts
// the ChatGPT SSE upstream back to a regular JSON response when the caller did
// not request streaming.
func CopyResponseForRequest(dst http.ResponseWriter, src *http.Response, startedAt time.Time, clientWantsStream bool) (ProxyMetrics, error) {
	contentType := strings.ToLower(src.Header.Get("Content-Type"))
	isSSE := strings.Contains(contentType, "text/event-stream") || (contentType == "" && clientWantsStream)
	if isSSE && !clientWantsStream && src.StatusCode >= 200 && src.StatusCode < 300 {
		return copySSEAsJSON(dst, src, startedAt)
	}
	if isSSE {
		return copySSE(dst, src, startedAt)
	}
	return copyBufferedResponse(dst, src, startedAt)
}

func copyResponseHeaders(dst, src http.Header) {
	for _, key := range responseHeaders {
		for _, value := range src.Values(key) {
			dst.Add(key, value)
		}
	}
}

func copySSE(dst http.ResponseWriter, src *http.Response, startedAt time.Time) (ProxyMetrics, error) {
	copyResponseHeaders(dst.Header(), src.Header)
	if dst.Header().Get("Content-Type") == "" {
		dst.Header().Set("Content-Type", "text/event-stream")
	}
	dst.WriteHeader(src.StatusCode)
	flusher, _ := dst.(http.Flusher)
	reader := bufio.NewReader(src.Body)
	var firstByteAt, firstTokenAt time.Time
	var usage responseUsage
	terminalSeen := false
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			now := time.Now()
			if firstByteAt.IsZero() {
				firstByteAt = now
			}
			if firstTokenAt.IsZero() && isTokenEvent(line) {
				firstTokenAt = now
			}
			if parsed, ok := parseResponseUsage(line); ok {
				usage = parsed
			}
			if eventType, ok := responseEventType(line); ok && isTerminalResponseEvent(eventType) {
				terminalSeen = true
			}
			if _, writeErr := dst.Write(line); writeErr != nil {
				return proxyMetrics(startedAt, firstByteAt, firstTokenAt, usage), writeErr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if err == io.EOF {
				if successfulStatus(src.StatusCode) && !terminalSeen {
					_ = writeResponsesFailedSSE(dst, src.Header, ErrIncompleteStream.Error())
					return proxyMetrics(startedAt, firstByteAt, firstTokenAt, usage), ErrIncompleteStream
				}
				return proxyMetrics(startedAt, firstByteAt, firstTokenAt, usage), nil
			}
			if successfulStatus(src.StatusCode) && !terminalSeen {
				_ = writeResponsesFailedSSE(dst, src.Header, "upstream stream read failed")
				return proxyMetrics(startedAt, firstByteAt, firstTokenAt, usage), fmt.Errorf("%w: %v", ErrIncompleteStream, err)
			}
			return proxyMetrics(startedAt, firstByteAt, firstTokenAt, usage), err
		}
	}
}

func copySSEAsJSON(dst http.ResponseWriter, src *http.Response, startedAt time.Time) (ProxyMetrics, error) {
	reader := bufio.NewReader(src.Body)
	var firstByteAt, firstTokenAt time.Time
	var usage responseUsage
	var finalResponse json.RawMessage
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			now := time.Now()
			if firstByteAt.IsZero() {
				firstByteAt = now
			}
			if firstTokenAt.IsZero() && isTokenEvent(line) {
				firstTokenAt = now
			}
			payload, ok := ssePayload(line)
			if ok {
				var event struct {
					Type     string          `json:"type"`
					Response json.RawMessage `json:"response"`
				}
				if json.Unmarshal(payload, &event) == nil && isTerminalResponseEvent(event.Type) && len(event.Response) > 0 && string(event.Response) != "null" {
					finalResponse = append(finalResponse[:0], event.Response...)
					if parsed, ok := parseJSONResponseUsage(event.Response); ok {
						usage = parsed
					}
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				writeProxyJSONError(dst, http.StatusBadGateway, "upstream_error", "upstream stream read failed")
				return proxyMetrics(startedAt, firstByteAt, firstTokenAt, usage), err
			}
			break
		}
	}
	if len(finalResponse) == 0 {
		writeProxyJSONError(dst, http.StatusBadGateway, "upstream_error", ErrIncompleteStream.Error())
		return proxyMetrics(startedAt, firstByteAt, firstTokenAt, usage), ErrIncompleteStream
	}
	copyResponseHeaders(dst.Header(), src.Header)
	dst.Header().Set("Content-Type", "application/json")
	dst.WriteHeader(src.StatusCode)
	_, err := dst.Write(finalResponse)
	return proxyMetrics(startedAt, firstByteAt, firstTokenAt, usage), err
}

func copyBufferedResponse(dst http.ResponseWriter, src *http.Response, startedAt time.Time) (ProxyMetrics, error) {
	body, err := io.ReadAll(io.LimitReader(src.Body, maxBufferedResponseBytes+1))
	firstByteAt := time.Now()
	if err != nil {
		writeProxyJSONError(dst, http.StatusBadGateway, "upstream_error", "upstream response read failed")
		return proxyMetrics(startedAt, firstByteAt, time.Time{}, responseUsage{}), err
	}
	if len(body) > maxBufferedResponseBytes {
		err = fmt.Errorf("upstream response exceeds %d bytes", maxBufferedResponseBytes)
		writeProxyJSONError(dst, http.StatusBadGateway, "upstream_error", err.Error())
		return proxyMetrics(startedAt, firstByteAt, time.Time{}, responseUsage{}), err
	}
	usage, _ := parseJSONResponseUsage(body)
	copyResponseHeaders(dst.Header(), src.Header)
	if dst.Header().Get("Content-Type") == "" {
		dst.Header().Set("Content-Type", "application/json")
	}
	dst.WriteHeader(src.StatusCode)
	_, writeErr := dst.Write(body)
	return proxyMetrics(startedAt, firstByteAt, time.Time{}, usage), writeErr
}

func writeProxyJSONError(dst http.ResponseWriter, status int, code, message string) {
	dst.Header().Set("Content-Type", "application/json")
	dst.WriteHeader(status)
	_ = json.NewEncoder(dst).Encode(map[string]any{"error": map[string]any{
		"type": code, "code": code, "message": message, "param": nil,
	}})
}

func writeResponsesFailedSSE(dst http.ResponseWriter, headers http.Header, message string) error {
	requestID := strings.TrimSpace(headers.Get("X-Request-Id"))
	if requestID == "" {
		requestID = strings.TrimSpace(headers.Get("Openai-Request-Id"))
	}
	requestID = strings.Map(func(value rune) rune {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' {
			return value
		}
		return -1
	}, requestID)
	if requestID == "" {
		seed, err := randomSessionSeed()
		if err != nil {
			return err
		}
		requestID = seed
	}
	payload, err := json.Marshal(map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"id": "resp_" + requestID, "object": "response", "status": "failed", "output": []any{},
			"error": map[string]string{"code": "upstream_error", "message": message},
		},
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(dst, "event: response.failed\ndata: %s\n\n", payload); err != nil {
		return err
	}
	if flusher, ok := dst.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func successfulStatus(status int) bool {
	return status >= 200 && status < 300
}

func proxyMetrics(startedAt, firstByteAt, firstTokenAt time.Time, usage responseUsage) ProxyMetrics {
	first := firstTokenAt
	if first.IsZero() {
		first = firstByteAt
	}
	ttft := time.Duration(0)
	if !first.IsZero() {
		ttft = first.Sub(startedAt)
	}
	return ProxyMetrics{
		TTFT:         ttft,
		Duration:     time.Since(startedAt),
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CachedTokens: usage.InputTokenDetails.CachedTokens,
	}
}

type responseUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	InputTokenDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

func parseJSONResponseUsage(payload []byte) (responseUsage, bool) {
	var response struct {
		Usage *responseUsage `json:"usage"`
	}
	if json.Unmarshal(payload, &response) != nil || response.Usage == nil {
		return responseUsage{}, false
	}
	return *response.Usage, true
}

func parseResponseUsage(line []byte) (responseUsage, bool) {
	payload, ok := ssePayload(line)
	if !ok {
		return responseUsage{}, false
	}
	var event struct {
		Type     string `json:"type"`
		Response struct {
			Usage responseUsage `json:"usage"`
		} `json:"response"`
	}
	if json.Unmarshal(payload, &event) != nil || !isTerminalResponseEvent(event.Type) {
		return responseUsage{}, false
	}
	return event.Response.Usage, true
}

func isTerminalResponseEvent(eventType string) bool {
	switch eventType {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}

func isTokenEvent(line []byte) bool {
	eventType, ok := responseEventType(line)
	return ok && strings.HasSuffix(eventType, ".delta")
}

func responseEventType(line []byte) (string, bool) {
	payload, ok := ssePayload(line)
	if !ok {
		return "", false
	}
	var event struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(payload, &event) != nil {
		return "", false
	}
	return event.Type, event.Type != ""
}

func ssePayload(line []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, []byte("data:")) {
		return nil, false
	}
	return bytes.TrimSpace(bytes.TrimPrefix(trimmed, []byte("data:"))), true
}

func isolateSession(apiKeyID, value string) string {
	sum := sha256.Sum256([]byte(apiKeyID + ":" + value))
	return hex.EncodeToString(sum[:])
}
