package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const codexAlphaSearchURL = "https://chatgpt.com/backend-api/codex/alpha/search"

var alphaSearchUnsupportedFields = [...]string{
	"prompt_cache_key",
	"prompt_cache_retention",
}

// PrepareAlphaSearchRequest validates the standalone Codex SearchClient
// request while preserving its evolving wire schema. Only fields explicitly
// rejected by the alpha/search endpoint are removed.
func PrepareAlphaSearchRequest(body []byte) ([]byte, RequestBilling, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, RequestBilling{}, fmt.Errorf("parse Codex alpha search request: %w", err)
	}
	if payload == nil {
		return nil, RequestBilling{}, fmt.Errorf("Codex alpha search request must be a JSON object")
	}
	var model string
	if raw, exists := payload["model"]; !exists || json.Unmarshal(raw, &model) != nil || strings.TrimSpace(model) == "" {
		return nil, RequestBilling{}, fmt.Errorf("Codex alpha search request model is required")
	}
	model = strings.TrimSpace(model)

	changed := false
	for _, field := range alphaSearchUnsupportedFields {
		if _, exists := payload[field]; exists {
			delete(payload, field)
			changed = true
		}
	}
	if !changed {
		return body, RequestBilling{Model: model}, nil
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, RequestBilling{}, fmt.Errorf("normalize Codex alpha search request: %w", err)
	}
	return normalized, RequestBilling{Model: model}, nil
}

// ForwardAlphaSearch proxies the standalone SearchClient protocol. It is not
// a Responses subrequest and therefore deliberately omits Responses-only
// headers such as OpenAI-Beta and session/conversation state.
func (g *Gateway) ForwardAlphaSearch(ctx context.Context, inbound *http.Request, body []byte, accessToken, accountID, proxyURL string) (*http.Response, error) {
	target, err := url.Parse(codexAlphaSearchURL)
	if err != nil {
		return nil, err
	}
	target.RawQuery = inbound.URL.RawQuery
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Host = "chatgpt.com"
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if accountID != "" {
		req.Header.Set("Chatgpt-Account-Id", accountID)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if metadata := strings.TrimSpace(inbound.Header.Get("X-Codex-Turn-Metadata")); metadata != "" {
		req.Header.Set("X-Codex-Turn-Metadata", metadata)
	}
	version := strings.TrimSpace(inbound.Header.Get("Version"))
	if !supportedCodexVersion(version) {
		version = codexProbeVersion
	}
	req.Header.Set("Version", version)
	originator, userAgent := alphaSearchIdentity(inbound.Header.Get("User-Agent"))
	req.Header.Set("Originator", originator)
	req.Header.Set("User-Agent", userAgent)

	client, err := g.clientForProxy(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("configure account proxy: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forward Codex alpha search request: %w", err)
	}
	return resp, nil
}

func supportedCodexVersion(version string) bool {
	var major, minor, patch int
	if _, err := fmt.Sscanf(strings.TrimSpace(version), "%d.%d.%d", &major, &minor, &patch); err != nil {
		return false
	}
	return major > 0 || minor > 144 || minor == 144 && patch >= 0
}

func alphaSearchIdentity(rawUserAgent string) (string, string) {
	userAgent := strings.TrimSpace(rawUserAgent)
	slash := strings.IndexByte(userAgent, '/')
	if slash <= 0 {
		return "codex_cli_rs", codexProbeUserAgent
	}
	originator := strings.TrimSpace(userAgent[:slash])
	switch strings.ToLower(originator) {
	case "codex_cli_rs", "codex_vscode", "codex_vscode_copilot", "codex_app", "codex_chatgpt_desktop", "codex_atlas", "codex_exec", "codex_sdk_ts":
		originator = strings.ToLower(originator)
	case "codex-tui":
		return "codex_cli_rs", "codex_cli_rs" + userAgent[slash:]
	default:
		if !strings.HasPrefix(originator, "Codex ") {
			return "codex_cli_rs", codexProbeUserAgent
		}
	}
	return originator, originator + userAgent[slash:]
}

// CopyAlphaSearchResponse forwards the fixed JSON response and accounts one
// successful standalone search. Non-2xx responses never count as searches.
func CopyAlphaSearchResponse(dst http.ResponseWriter, src *http.Response, startedAt time.Time) (ProxyMetrics, error) {
	metrics, err := copyBufferedResponse(dst, src, startedAt)
	if err == nil && src.StatusCode >= http.StatusOK && src.StatusCode < http.StatusMultipleChoices {
		metrics.WebSearchCalls = 1
	}
	return metrics, err
}
