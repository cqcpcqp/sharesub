package openai

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

type responseUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	Images            int64 `json:"images,omitempty"`
	InputTokenDetails struct {
		CachedTokens     int64 `json:"cached_tokens"`
		CacheWriteTokens int64 `json:"cache_write_tokens"`
		ImageTokens      int64 `json:"image_tokens"`
	} `json:"input_tokens_details"`
	OutputTokenDetails struct {
		ImageTokens int64 `json:"image_tokens"`
	} `json:"output_tokens_details"`
}

type responseOutputItem struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type responseError struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type terminalResponse struct {
	ID        string               `json:"id"`
	Model     string               `json:"model"`
	Usage     responseUsage        `json:"usage"`
	Output    []responseOutputItem `json:"output"`
	Error     *responseError       `json:"error"`
	hasUsage  bool
	hasOutput bool
}

func (r terminalResponse) emptyCompleted() bool {
	return !r.hasUsage && !r.hasOutput && r.Error == nil
}

func (r terminalResponse) imageCount() int64 {
	var count int64
	for _, item := range r.Output {
		if item.Type == "image_generation_call" && item.Status == "completed" {
			count++
		}
	}
	return count
}

func (r terminalResponse) webSearchCalls() int64 {
	var count int64
	for _, item := range r.Output {
		if item.Type == "web_search_call" && item.Status == "completed" {
			count++
		}
	}
	return count
}

func parseJSONResponseUsage(payload []byte) (terminalResponse, bool) {
	var response terminalResponse
	if json.Unmarshal(payload, &response) != nil {
		return terminalResponse{}, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) == nil {
		_, response.hasUsage = fields["usage"]
		_, outputPresent := fields["output"]
		response.hasOutput = outputPresent && len(response.Output) > 0
	}
	return response, true
}

func parseUpstreamErrorEnvelope(payload []byte) *responseError {
	var envelope struct {
		Error *responseError `json:"error"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return nil
	}
	return envelope.Error
}

func parseResponseUsage(line []byte) (terminalResponse, bool) {
	payload, ok := ssePayload(line)
	if !ok {
		return terminalResponse{}, false
	}
	var event struct {
		Type     string          `json:"type"`
		Response json.RawMessage `json:"response"`
	}
	if json.Unmarshal(payload, &event) != nil || !isTerminalResponseEvent(event.Type) {
		return terminalResponse{}, false
	}
	return parseJSONResponseUsage(event.Response)
}

func parseSSEError(line []byte) *responseError {
	payload, ok := ssePayload(line)
	if !ok {
		return nil
	}
	var event struct {
		Type  string         `json:"type"`
		Error *responseError `json:"error"`
	}
	if json.Unmarshal(payload, &event) != nil || event.Type != "error" {
		return nil
	}
	return event.Error
}

func terminalFailureStatus(response terminalResponse) int {
	if response.Error == nil {
		return http.StatusBadGateway
	}
	combined := strings.ToLower(strings.TrimSpace(response.Error.Type + " " + response.Error.Code + " " + response.Error.Message))
	switch {
	case response.Error.Type == "image_generation_user_error" || strings.Contains(combined, "content_policy") || strings.Contains(combined, "policy_violation") || strings.Contains(combined, "safety_violation") || strings.Contains(combined, "content_filter") || strings.Contains(combined, "moderation_blocked"):
		return http.StatusBadRequest
	case strings.Contains(combined, "context_length") || strings.Contains(combined, "context window") || strings.Contains(combined, "invalid_request"):
		if response.Error.Code == "server_is_overloaded" || response.Error.Code == "slow_down" || transientProcessingFailure(combined) {
			return http.StatusServiceUnavailable
		}
		return http.StatusBadRequest
	case strings.Contains(combined, "rate_limit"):
		return http.StatusTooManyRequests
	case strings.Contains(combined, "authentication") || strings.Contains(combined, "unauthorized") || strings.Contains(combined, "invalid_api_key"):
		return http.StatusUnauthorized
	case strings.Contains(combined, "permission") || strings.Contains(combined, "forbidden") || strings.Contains(combined, "access denied"):
		return http.StatusForbidden
	case response.Error.Code == "server_is_overloaded" || response.Error.Code == "slow_down":
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

func retryableTerminalFailure(response terminalResponse) (int, bool) {
	status := terminalFailureStatus(response)
	if response.Error == nil {
		return status, true
	}
	combined := strings.ToLower(strings.TrimSpace(response.Error.Type + " " + response.Error.Code + " " + response.Error.Message))
	for _, marker := range []string{"content_policy", "policy", "safety", "high-risk cyber", "not allowed", "violat"} {
		if strings.Contains(combined, marker) {
			return status, false
		}
	}
	return status, status != http.StatusBadRequest
}

const capacityShedRetryableClientCode = "server_error"

func isCapacityShedErrorCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "server_is_overloaded", "slow_down":
		return true
	default:
		return false
	}
}

func sanitizeCapacityShedSSEForClient(body []byte) []byte {
	lines := bytes.SplitAfter(body, []byte("\n"))
	for index, line := range lines {
		lines[index] = sanitizeCapacityShedSSELineForClient(line)
	}
	return bytes.Join(lines, nil)
}

// Capacity-shed codes are fatal to Codex. Rewrite only the client copy so
// metrics and account routing continue to use the original upstream error.
func sanitizeCapacityShedSSELineForClient(line []byte) []byte {
	payload, ok := ssePayload(line)
	if !ok {
		return line
	}
	var event map[string]json.RawMessage
	if json.Unmarshal(payload, &event) != nil {
		return line
	}
	var eventType string
	if json.Unmarshal(event["type"], &eventType) != nil {
		return line
	}
	changed := false
	switch eventType {
	case "error":
		event["error"], changed = sanitizeCapacityShedErrorForClient(event["error"])
	case "response.failed":
		var response map[string]json.RawMessage
		if json.Unmarshal(event["response"], &response) != nil {
			return line
		}
		response["error"], changed = sanitizeCapacityShedErrorForClient(response["error"])
		if changed {
			updatedResponse, err := json.Marshal(response)
			if err != nil {
				return line
			}
			event["response"] = updatedResponse
		}
	}
	if !changed {
		return line
	}
	updatedPayload, err := json.Marshal(event)
	if err != nil {
		return line
	}
	return replaceSSEPayload(line, payload, updatedPayload)
}

func sanitizeCapacityShedResponseForClient(payload []byte) []byte {
	var response map[string]json.RawMessage
	if json.Unmarshal(payload, &response) != nil {
		return payload
	}
	updatedError, changed := sanitizeCapacityShedErrorForClient(response["error"])
	if !changed {
		return payload
	}
	response["error"] = updatedError
	updated, err := json.Marshal(response)
	if err != nil {
		return payload
	}
	return updated
}

func sanitizeCapacityShedErrorForClient(payload json.RawMessage) (json.RawMessage, bool) {
	var upstreamError map[string]json.RawMessage
	if json.Unmarshal(payload, &upstreamError) != nil {
		return payload, false
	}
	var code string
	if json.Unmarshal(upstreamError["code"], &code) != nil || !isCapacityShedErrorCode(code) {
		return payload, false
	}
	updatedCode, err := json.Marshal(capacityShedRetryableClientCode)
	if err != nil {
		return payload, false
	}
	upstreamError["code"] = updatedCode
	updated, err := json.Marshal(upstreamError)
	if err != nil {
		return payload, false
	}
	return updated, true
}

func replaceSSEPayload(line, payload, replacement []byte) []byte {
	start := bytes.Index(line, payload)
	if start < 0 {
		return line
	}
	updated := make([]byte, 0, len(line)-len(payload)+len(replacement))
	updated = append(updated, line[:start]...)
	updated = append(updated, replacement...)
	updated = append(updated, line[start+len(payload):]...)
	return updated
}

func transientProcessingFailure(message string) bool {
	return strings.Contains(message, "an error occurred while processing your request") ||
		strings.Contains(message, "selected model is at capacity") ||
		(strings.Contains(message, "you can retry your request") && strings.Contains(message, "help.openai.com") && strings.Contains(message, "request id"))
}

func isTerminalResponseEvent(eventType string) bool {
	switch eventType {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}

func isClientOutputEvent(line []byte) bool {
	eventType, ok := responseEventType(line)
	if !ok {
		return false
	}
	switch eventType {
	case "response.created", "response.in_progress", "response.failed":
		return false
	case "error":
		// Retryable upstream errors precede response.failed and must remain in
		// the pending buffer so account failover is still safe.
		payload, _ := ssePayload(line)
		var event struct {
			Error *responseError `json:"error"`
		}
		if json.Unmarshal(payload, &event) != nil || event.Error == nil {
			return false
		}
		_, retryable := retryableTerminalFailure(terminalResponse{Error: event.Error})
		return !retryable
	default:
		return !strings.HasPrefix(eventType, "rate_limits.")
	}
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
