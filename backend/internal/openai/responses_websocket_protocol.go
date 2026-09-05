package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/coder/websocket"
)

func isResponsesWebSocketResponseCreate(messageType websocket.MessageType, frame []byte) bool {
	return responsesWebSocketEventTypeIs(messageType, frame, "response.create")
}

func isResponsesWebSocketResponseCancel(messageType websocket.MessageType, frame []byte) bool {
	return responsesWebSocketEventTypeIs(messageType, frame, "response.cancel")
}

func responsesWebSocketEventTypeIs(messageType websocket.MessageType, frame []byte, expected string) bool {
	if messageType != websocket.MessageText {
		return false
	}
	var event struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(frame, &event) == nil && strings.TrimSpace(event.Type) == expected
}

func updateResponsesWebSocketSessionModel(model *atomic.Pointer[string], messageType websocket.MessageType, frame []byte) {
	if model == nil || messageType != websocket.MessageText {
		return
	}
	var event struct {
		Type    string `json:"type"`
		Session struct {
			Model string `json:"model"`
		} `json:"session"`
	}
	if json.Unmarshal(frame, &event) != nil || strings.TrimSpace(event.Type) != "session.update" {
		return
	}
	storeResponsesWebSocketSessionModel(model, event.Session.Model)
}

// responsesWebSocketControlPreservesReplaySafety reports whether a client
// control frame can be represented completely when a later turn is rebuilt on
// a fresh upstream connection. Model-only session updates are safe because the
// proxy injects the tracked session model into every later response.create.
// Cancellation is turn-local. Any other control may carry upstream session
// state that the proxy cannot reconstruct, so cross-account replay must stop.
func responsesWebSocketControlPreservesReplaySafety(messageType websocket.MessageType, frame []byte) bool {
	if messageType != websocket.MessageText {
		return false
	}
	var event struct {
		Type    string                     `json:"type"`
		Session map[string]json.RawMessage `json:"session"`
	}
	if json.Unmarshal(frame, &event) != nil {
		return false
	}
	switch strings.TrimSpace(event.Type) {
	case "response.cancel":
		return true
	case "session.update":
		if len(event.Session) != 1 {
			return false
		}
		var model string
		if json.Unmarshal(event.Session["model"], &model) != nil {
			return false
		}
		return strings.TrimSpace(model) != ""
	default:
		return false
	}
}

func storeResponsesWebSocketSessionModel(model *atomic.Pointer[string], value string) {
	if model == nil {
		return
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	model.Store(&value)
}

func loadResponsesWebSocketSessionModel(model *atomic.Pointer[string]) string {
	if model == nil {
		return ""
	}
	current := model.Load()
	if current == nil {
		return ""
	}
	return strings.TrimSpace(*current)
}

func parseResponsesWebSocketFrame(frame []byte, turn int, inheritedModel string) ([]byte, RequestBilling, string, error) {
	trimmed := bytes.TrimSpace(frame)
	if len(trimmed) == 0 {
		return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "empty WebSocket request payload", nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "invalid WebSocket request payload", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "WebSocket request must contain one JSON object", err)
	}
	eventType, exists := payload["type"]
	if !exists {
		payload["type"] = "response.create"
	} else if value, ok := eventType.(string); !ok || strings.TrimSpace(value) != "response.create" {
		reason := "unsupported WebSocket request type"
		if value == "response.append" {
			reason = "response.append is not supported in WebSocket v2; use response.create with previous_response_id"
		}
		return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, reason, nil)
	}
	if turn == 1 {
		model, ok := payload["model"].(string)
		if !ok || strings.TrimSpace(model) == "" {
			return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "model is required in the first response.create payload", nil)
		}
	} else if _, exists := payload["model"]; !exists {
		payload["model"] = inheritedModel
	}
	if previous, ok := payload["previous_response_id"].(string); ok && responsesWebSocketMessageIDPattern.MatchString(strings.TrimSpace(previous)) {
		return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "previous_response_id must be a response.id (resp_*), not a message id", nil)
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, RequestBilling{}, "", err
	}
	metadata, previousResponseID, err := responsesWebSocketBilling(payload, turn)
	if err != nil {
		return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, err.Error(), err)
	}
	return normalized, metadata, previousResponseID, nil
}

func prepareResponsesWebSocketFrame(frame []byte, turn int, inheritedModel string) ([]byte, RequestBilling, string, error) {
	normalized, metadata, previousResponseID, err := parseResponsesWebSocketFrame(frame, turn, inheritedModel)
	if err != nil {
		return nil, RequestBilling{}, "", err
	}
	var payload map[string]any
	if err := json.Unmarshal(normalized, &payload); err != nil {
		return nil, RequestBilling{}, "", err
	}
	for _, field := range chatGPTUnsupportedFields {
		delete(payload, field)
	}
	normalizeGPT6AstraRequest(payload, metadata.Model)
	delete(payload, "background")
	payload["type"] = "response.create"
	payload["store"] = false
	if _, exists := payload["stream"]; !exists {
		payload["stream"] = true
	}
	normalizeCodexInput(payload)
	ensureCodexReasoningInclude(payload)
	normalized, err = json.Marshal(payload)
	return normalized, metadata, previousResponseID, err
}

func responsesWebSocketBilling(payload map[string]any, turn int) (RequestBilling, string, error) {
	metadata := RequestBilling{Stream: true}
	if model, exists := payload["model"]; exists {
		value, ok := model.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return RequestBilling{}, "", errors.New("response.create model must be a non-empty string")
		}
		metadata.Model = strings.TrimSpace(value)
	} else if turn == 1 {
		return RequestBilling{}, "", errors.New("response.create model is required")
	}
	if tier, exists := payload["service_tier"]; exists {
		value, ok := tier.(string)
		if !ok {
			return RequestBilling{}, "", errors.New("response.create service_tier must be a string")
		}
		metadata.ServiceTier = strings.TrimSpace(value)
	}
	if key, exists := payload["prompt_cache_key"]; exists {
		value, ok := key.(string)
		if !ok {
			return RequestBilling{}, "", errors.New("response.create prompt_cache_key must be a string")
		}
		metadata.PromptCacheKey = strings.TrimSpace(value)
	}
	if stream, exists := payload["stream"]; exists {
		value, ok := stream.(bool)
		if !ok {
			return RequestBilling{}, "", errors.New("response.create stream must be a boolean")
		}
		metadata.Stream = value
	}
	previousResponseID := ""
	if previous, exists := payload["previous_response_id"]; exists {
		value, ok := previous.(string)
		if !ok {
			return RequestBilling{}, "", errors.New("response.create previous_response_id must be a string")
		}
		previousResponseID = strings.TrimSpace(value)
	}
	return metadata, previousResponseID, nil
}

func parseResponsesWebSocketEvent(frame []byte) (string, string, *terminalResponse, error) {
	var event struct {
		Type       string           `json:"type"`
		ID         string           `json:"id"`
		ResponseID string           `json:"response_id"`
		Response   terminalResponse `json:"response"`
		Error      *responseError   `json:"error"`
	}
	if err := json.Unmarshal(frame, &event); err != nil {
		return "", "", nil, fmt.Errorf("parse ChatGPT Responses WebSocket event: %w", err)
	}
	eventType := strings.TrimSpace(event.Type)
	if eventType == "" {
		return "", "", nil, errors.New("ChatGPT Responses WebSocket event type is required")
	}
	responseID := event.Response.ID
	if responseID == "" {
		responseID = event.ResponseID
	}
	if responseID == "" && isTerminalResponseEvent(eventType) {
		responseID = event.ID
	}
	if isTerminalResponseEvent(eventType) {
		return eventType, responseID, &event.Response, nil
	}
	return eventType, responseID, nil, nil
}

func observeResponsesWebSocketEvent(frame []byte) (string, string, *terminalResponse) {
	eventType, responseID, terminal, err := parseResponsesWebSocketEvent(frame)
	if err != nil {
		return "", "", nil
	}
	return eventType, responseID, terminal
}

func parseResponsesWebSocketErrorEvent(frame []byte) *ResponsesWebSocketUpstreamEventError {
	var event struct {
		Error *responseError `json:"error"`
	}
	if json.Unmarshal(frame, &event) != nil || event.Error == nil {
		return nil
	}
	return &ResponsesWebSocketUpstreamEventError{
		Code: event.Error.Code, Type: event.Error.Type, Message: event.Error.Message, Frame: append([]byte(nil), frame...),
	}
}

// isResponsesWebSocketRateLimitError intentionally mirrors sub2api's WS v2
// classifier. Only explicit upstream rate, usage, or quota exhaustion signals
// are safe to replay on another account before any downstream frame exists.
func isResponsesWebSocketRateLimitError(upstreamErr *ResponsesWebSocketUpstreamEventError) bool {
	if upstreamErr == nil {
		return false
	}
	code := strings.ToLower(strings.TrimSpace(upstreamErr.Code))
	errorType := strings.ToLower(strings.TrimSpace(upstreamErr.Type))
	message := strings.ToLower(strings.TrimSpace(upstreamErr.Message))
	if strings.Contains(errorType, "rate_limit") || strings.Contains(errorType, "usage_limit") {
		return true
	}
	if strings.Contains(code, "rate_limit") || strings.Contains(code, "usage_limit") || strings.Contains(code, "insufficient_quota") {
		return true
	}
	if strings.Contains(message, "usage limit") && strings.Contains(message, "reached") {
		return true
	}
	return strings.Contains(message, "rate limit") && (strings.Contains(message, "reached") || strings.Contains(message, "exceeded"))
}

func normalizeResponsesWebSocketTerminalEvent(eventType string) string {
	if eventType == "response.canceled" {
		return "response.cancelled"
	}
	return eventType
}

func isResponsesWebSocketTokenEvent(frame []byte, eventType string) bool {
	return responseEventStartsVisibleOutput(frame, eventType)
}

func PrepareResponsesWebSocketFingerprint(config *ResponsesWebSocketDialConfig, frame []byte, promptCacheKey string) ([]byte, error) {
	if config == nil || strings.TrimSpace(config.InternalAccountID) == "" {
		return frame, nil
	}
	sessionID := ClientCodexSessionID(config.InboundHeader, promptCacheKey)
	fingerprint, err := ResolveCodexFingerprint(CodexFingerprintConfig{
		AccountID: config.InternalAccountID, APIKeyID: config.APIKeyID, Mode: config.FingerprintMode, ClientSessionID: sessionID,
	})
	if err != nil {
		return nil, err
	}
	config.Fingerprint = fingerprint
	return ApplyCodexFingerprintBody(frame, fingerprint)
}

func responsesWebSocketHeaders(config ResponsesWebSocketDialConfig, promptCacheKey string) (http.Header, error) {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+config.AccessToken)
	if config.ChatGPTAccountID != "" {
		headers.Set("ChatGPT-Account-ID", config.ChatGPTAccountID)
	}
	headers.Set("OpenAI-Beta", responsesWebSocketBetaV2)
	applyCodexOAuthIdentity(headers, "")
	applyCodexRoutingHint(headers, config.Model, config.ServiceTier)
	for _, name := range []string{"Accept-Language", "X-Codex-Beta-Features", "X-Codex-Window-Id", "X-Codex-Installation-Id", "X-Codex-Turn-State", "X-Codex-Turn-Metadata"} {
		for _, value := range config.InboundHeader.Values(name) {
			if strings.TrimSpace(value) != "" {
				headers.Add(name, value)
			}
		}
	}
	sessionID := strings.TrimSpace(config.InboundHeader.Get("session_id"))
	conversationID := strings.TrimSpace(config.InboundHeader.Get("conversation_id"))
	if sessionID == "" && conversationID != "" {
		sessionID = conversationID
	}
	if sessionID == "" {
		sessionID = strings.TrimSpace(promptCacheKey)
	}
	fingerprint := config.Fingerprint
	var err error
	if fingerprint == nil && strings.TrimSpace(config.InternalAccountID) != "" {
		fingerprint, err = ResolveCodexFingerprint(CodexFingerprintConfig{
			AccountID: config.InternalAccountID, APIKeyID: config.APIKeyID, Mode: config.FingerprintMode, ClientSessionID: sessionID,
		})
		if err != nil {
			return nil, err
		}
	}
	useLegacyIsolation := strings.TrimSpace(config.InternalAccountID) == ""
	if err == nil && useLegacyIsolation {
		if sessionID != "" {
			headers.Set("session_id", isolateSession(config.APIKeyID, sessionID))
		}
		if conversationID != "" {
			headers.Set("conversation_id", isolateSession(config.APIKeyID, conversationID))
		}
	} else if fingerprint == nil || fingerprint.mode == "device" {
		if sessionID != "" {
			headers.Set("session_id", sessionID)
		}
		if conversationID != "" {
			headers.Set("conversation_id", conversationID)
		}
	}
	if err := ApplyCodexFingerprintHeaders(headers, fingerprint); err != nil {
		return nil, err
	}
	return headers, nil
}

func validateResponsesWebSocketDialConfig(config ResponsesWebSocketDialConfig) error {
	if strings.TrimSpace(config.AccessToken) == "" {
		return errors.New("Responses WebSocket access token is required")
	}
	if strings.TrimSpace(config.APIKeyID) == "" {
		return errors.New("Responses WebSocket API key ID is required")
	}
	if strings.TrimSpace(config.ChatGPTAccountID) == "" {
		return errors.New("Responses WebSocket ChatGPT account ID is required")
	}
	if strings.TrimSpace(config.InternalAccountID) != "" {
		if _, err := ResolveCodexFingerprint(CodexFingerprintConfig{AccountID: config.InternalAccountID, APIKeyID: config.APIKeyID, Mode: config.FingerprintMode, ClientSessionID: "validation"}); err != nil {
			return err
		}
	}
	return nil
}

func cloneResponsesWebSocketDialConfig(config ResponsesWebSocketDialConfig) ResponsesWebSocketDialConfig {
	config.InboundHeader = cloneWebSocketHeader(config.InboundHeader)
	return config
}

func verifyPinnedResponsesWebSocketDial(pinned, current ResponsesWebSocketDialConfig) error {
	if strings.TrimSpace(current.APIKeyID) != strings.TrimSpace(pinned.APIKeyID) {
		return errors.New("Responses WebSocket API key binding changed")
	}
	if strings.TrimSpace(current.ChatGPTAccountID) != strings.TrimSpace(pinned.ChatGPTAccountID) {
		return errors.New("Responses WebSocket ChatGPT account binding changed")
	}
	if strings.TrimSpace(current.ProxyURL) != strings.TrimSpace(pinned.ProxyURL) {
		return errors.New("Responses WebSocket proxy binding changed")
	}
	return nil
}

func cloneWebSocketHeader(source http.Header) http.Header {
	if source == nil {
		return nil
	}
	return source.Clone()
}
