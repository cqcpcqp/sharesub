package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxSSELineBytes = 16 << 20

var errSSELineTooLarge = fmt.Errorf("upstream SSE event exceeds %d bytes", maxSSELineBytes)

type ProxyMetrics struct {
	TTFT                time.Duration
	Duration            time.Duration
	InputTokens         int64
	OutputTokens        int64
	CachedTokens        int64
	CacheCreationTokens int64
	ImageInputTokens    int64
	ImageOutputTokens   int64
	ImageCount          int64
	ImageSize           string
	WebSearchCalls      int64
	UpstreamModel       string
	ClientDisconnected  bool
	ErrorCode           string
	ErrorMessage        string
	ErrorStatusCode     int
}

type StreamFailoverError struct {
	StatusCode int
	StreamBody []byte
	Response   json.RawMessage
}

func (e *StreamFailoverError) Error() string {
	return fmt.Sprintf("retryable upstream terminal failure: status %d", e.StatusCode)
}

func WriteStreamFailoverError(dst http.ResponseWriter, src *http.Response, failure *StreamFailoverError, clientWantsStream bool) error {
	copyResponseHeaders(dst.Header(), src.Header)
	if clientWantsStream {
		if dst.Header().Get("Content-Type") == "" {
			dst.Header().Set("Content-Type", "text/event-stream")
		}
		dst.WriteHeader(src.StatusCode)
		_, err := dst.Write(sanitizeCapacityShedSSEForClient(failure.StreamBody))
		return err
	}
	dst.Header().Set("Content-Type", "application/json")
	dst.WriteHeader(failure.StatusCode)
	_, err := dst.Write(sanitizeCapacityShedResponseForClient(failure.Response))
	return err
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

func DrainResponse(src *http.Response, startedAt time.Time) (ProxyMetrics, []byte, error) {
	contentType := strings.ToLower(src.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/event-stream") {
		reader := bufio.NewReader(src.Body)
		var firstByteAt, firstTokenAt time.Time
		var terminal terminalResponse
		body := make([]byte, 0, 4096)
		for {
			line, err := readLimitedLine(reader)
			if len(line) > 0 {
				if len(body)+len(line) <= maxBufferedResponseBytes {
					body = append(body, line...)
				}
				now := time.Now()
				if firstByteAt.IsZero() {
					firstByteAt = now
				}
				if firstTokenAt.IsZero() && isClientOutputEvent(line) {
					firstTokenAt = now
				}
				if parsed, ok := parseResponseUsage(line); ok {
					terminal = parsed
				}
			}
			if err != nil {
				if err == io.EOF {
					return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), body, nil
				}
				return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), body, err
			}
		}
	}
	body, err := io.ReadAll(io.LimitReader(src.Body, maxBufferedResponseBytes+1))
	firstByteAt := time.Now()
	if err != nil {
		return proxyMetrics(startedAt, firstByteAt, time.Time{}, terminalResponse{}, false), body, err
	}
	if len(body) > maxBufferedResponseBytes {
		return proxyMetrics(startedAt, firstByteAt, time.Time{}, terminalResponse{}, false), nil, fmt.Errorf("upstream response exceeds %d bytes", maxBufferedResponseBytes)
	}
	terminal, _ := parseJSONResponseUsage(body)
	if terminal.Error == nil {
		terminal.Error = parseUpstreamErrorEnvelope(body)
	}
	return proxyMetrics(startedAt, firstByteAt, time.Time{}, terminal, false), body, nil
}

func WriteDrainedResponse(dst http.ResponseWriter, src *http.Response, body []byte) error {
	copyResponseHeaders(dst.Header(), src.Header)
	dst.WriteHeader(src.StatusCode)
	_, err := dst.Write(sanitizeCapacityShedResponseForClient(body))
	return err
}

func copyResponseHeaders(dst, src http.Header) {
	for _, key := range responseHeaders {
		for _, value := range src.Values(key) {
			dst.Add(key, value)
		}
	}
}

func copySSE(dst http.ResponseWriter, src *http.Response, startedAt time.Time) (ProxyMetrics, error) {
	return copySSEWithPendingLimit(dst, src, startedAt, maxBufferedResponseBytes)
}

func copySSEWithPendingLimit(dst http.ResponseWriter, src *http.Response, startedAt time.Time, pendingLimit int) (ProxyMetrics, error) {
	flusher, _ := dst.(http.Flusher)
	reader := bufio.NewReader(src.Body)
	var firstByteAt, firstTokenAt time.Time
	var terminal terminalResponse
	terminalSeen := false
	clientOutputStarted := false
	clientDisconnected := false
	pending := make([]byte, 0, 4096)
	writeClient := func(data []byte) {
		if clientDisconnected || len(data) == 0 {
			return
		}
		if _, err := dst.Write(data); err != nil {
			clientDisconnected = true
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	startClientOutput := func() {
		if clientOutputStarted {
			return
		}
		clientOutputStarted = true
		copyResponseHeaders(dst.Header(), src.Header)
		if dst.Header().Get("Content-Type") == "" {
			dst.Header().Set("Content-Type", "text/event-stream")
		}
		dst.WriteHeader(src.StatusCode)
		writeClient(sanitizeCapacityShedSSEForClient(pending))
		pending = pending[:0]
	}
	for {
		line, err := readLimitedLine(reader)
		if len(line) > 0 {
			now := time.Now()
			if firstByteAt.IsZero() {
				firstByteAt = now
			}
			startsOutput := isClientOutputEvent(line)
			if firstTokenAt.IsZero() && startsOutput {
				firstTokenAt = now
			}
			if parsed, ok := parseResponseUsage(line); ok {
				terminal = parsed
			}
			if !clientOutputStarted && len(line) >= pendingLimit-len(pending) {
				startClientOutput()
			}
			eventType, hasEvent := responseEventType(line)
			if hasEvent && isTerminalResponseEvent(eventType) {
				terminalSeen = true
				if eventType == "response.failed" && !clientOutputStarted {
					pending = append(pending, line...)
					if status, retryable := retryableTerminalFailure(terminal); retryable {
						return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, clientDisconnected), &StreamFailoverError{StatusCode: status, StreamBody: append([]byte(nil), pending...)}
					}
					startClientOutput()
					continue
				}
			}
			if !clientOutputStarted {
				pending = append(pending, line...)
				if startsOutput {
					startClientOutput()
				}
			} else {
				writeClient(sanitizeCapacityShedSSELineForClient(line))
			}
		}
		if err != nil {
			if err == io.EOF {
				if successfulStatus(src.StatusCode) && !terminalSeen {
					if !clientOutputStarted {
						startClientOutput()
					}
					if !clientDisconnected {
						_ = writeResponsesFailedSSE(dst, src.Header, ErrIncompleteStream.Error())
					}
					return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, clientDisconnected), ErrIncompleteStream
				}
				if !clientOutputStarted {
					startClientOutput()
				}
				return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, clientDisconnected), nil
			}
			if successfulStatus(src.StatusCode) && !terminalSeen {
				if !clientOutputStarted {
					startClientOutput()
				}
				if !clientDisconnected {
					_ = writeResponsesFailedSSE(dst, src.Header, "upstream stream read failed")
				}
				return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, clientDisconnected), fmt.Errorf("%w: %v", ErrIncompleteStream, err)
			}
			return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, clientDisconnected), err
		}
	}
}

func copySSEAsJSON(dst http.ResponseWriter, src *http.Response, startedAt time.Time) (ProxyMetrics, error) {
	reader := bufio.NewReader(src.Body)
	var firstByteAt, firstTokenAt time.Time
	var terminal terminalResponse
	var finalResponse json.RawMessage
	var terminalType string
	for {
		line, err := readLimitedLine(reader)
		if len(line) > 0 {
			now := time.Now()
			if firstByteAt.IsZero() {
				firstByteAt = now
			}
			if firstTokenAt.IsZero() && isClientOutputEvent(line) {
				firstTokenAt = now
			}
			payload, ok := ssePayload(line)
			if ok {
				var event struct {
					Type     string          `json:"type"`
					Response json.RawMessage `json:"response"`
				}
				if json.Unmarshal(payload, &event) == nil && isTerminalResponseEvent(event.Type) && len(event.Response) > 0 && string(event.Response) != "null" {
					terminalType = event.Type
					finalResponse = append(finalResponse[:0], event.Response...)
					if parsed, ok := parseJSONResponseUsage(event.Response); ok {
						terminal = parsed
					}
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				writeProxyJSONError(dst, http.StatusBadGateway, "upstream_error", "upstream stream read failed")
				return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), err
			}
			break
		}
	}
	if len(finalResponse) == 0 {
		writeProxyJSONError(dst, http.StatusBadGateway, "upstream_error", ErrIncompleteStream.Error())
		return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), ErrIncompleteStream
	}
	if terminalType == "response.failed" {
		if status, retryable := retryableTerminalFailure(terminal); retryable {
			return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), &StreamFailoverError{StatusCode: status, Response: append(json.RawMessage(nil), finalResponse...)}
		}
	}
	copyResponseHeaders(dst.Header(), src.Header)
	dst.Header().Set("Content-Type", "application/json")
	dst.WriteHeader(src.StatusCode)
	_, err := dst.Write(finalResponse)
	return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), err
}

func readLimitedLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 4096)
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > maxSSELineBytes {
			return nil, errSSELineTooLarge
		}
		line = append(line, fragment...)
		if err == bufio.ErrBufferFull {
			continue
		}
		return line, err
	}
}

func copyBufferedResponse(dst http.ResponseWriter, src *http.Response, startedAt time.Time) (ProxyMetrics, error) {
	body, err := io.ReadAll(io.LimitReader(src.Body, maxBufferedResponseBytes+1))
	firstByteAt := time.Now()
	if err != nil {
		writeProxyJSONError(dst, http.StatusBadGateway, "upstream_error", "upstream response read failed")
		return proxyMetrics(startedAt, firstByteAt, time.Time{}, terminalResponse{}, false), err
	}
	if len(body) > maxBufferedResponseBytes {
		err = fmt.Errorf("upstream response exceeds %d bytes", maxBufferedResponseBytes)
		writeProxyJSONError(dst, http.StatusBadGateway, "upstream_error", err.Error())
		return proxyMetrics(startedAt, firstByteAt, time.Time{}, terminalResponse{}, false), err
	}
	terminal, _ := parseJSONResponseUsage(body)
	if terminal.Error == nil {
		terminal.Error = parseUpstreamErrorEnvelope(body)
	}
	copyResponseHeaders(dst.Header(), src.Header)
	if dst.Header().Get("Content-Type") == "" {
		dst.Header().Set("Content-Type", "application/json")
	}
	dst.WriteHeader(src.StatusCode)
	_, writeErr := dst.Write(sanitizeCapacityShedResponseForClient(body))
	return proxyMetrics(startedAt, firstByteAt, time.Time{}, terminal, false), writeErr
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

func proxyMetrics(startedAt, firstByteAt, firstTokenAt time.Time, terminal terminalResponse, clientDisconnected bool) ProxyMetrics {
	ttft := time.Duration(0)
	if !firstTokenAt.IsZero() {
		ttft = firstTokenAt.Sub(startedAt)
	}
	metrics := ProxyMetrics{
		TTFT:                ttft,
		Duration:            time.Since(startedAt),
		InputTokens:         terminal.Usage.InputTokens,
		OutputTokens:        terminal.Usage.OutputTokens,
		CachedTokens:        terminal.Usage.InputTokenDetails.CachedTokens,
		CacheCreationTokens: terminal.Usage.InputTokenDetails.CacheWriteTokens,
		ImageInputTokens:    terminal.Usage.InputTokenDetails.ImageTokens,
		ImageOutputTokens:   terminal.Usage.OutputTokenDetails.ImageTokens,
		ImageCount:          terminal.imageCount(),
		WebSearchCalls:      terminal.webSearchCalls(),
		UpstreamModel:       terminal.Model,
		ClientDisconnected:  clientDisconnected,
	}
	if terminal.Error != nil {
		metrics.ErrorCode = terminal.Error.Code
		metrics.ErrorMessage = terminal.Error.Message
		metrics.ErrorStatusCode = terminalFailureStatus(terminal)
	}
	return metrics
}

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
	Model  string               `json:"model"`
	Usage  responseUsage        `json:"usage"`
	Output []responseOutputItem `json:"output"`
	Error  *responseError       `json:"error"`
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
		Type     string           `json:"type"`
		Response terminalResponse `json:"response"`
	}
	if json.Unmarshal(payload, &event) != nil || !isTerminalResponseEvent(event.Type) {
		return terminalResponse{}, false
	}
	return event.Response, true
}

func terminalFailureStatus(response terminalResponse) int {
	if response.Error == nil {
		return http.StatusBadGateway
	}
	combined := strings.ToLower(strings.TrimSpace(response.Error.Type + " " + response.Error.Code + " " + response.Error.Message))
	switch {
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
