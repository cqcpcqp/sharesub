package openai

import (
	"bufio"
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
	StatusCode             int
	StreamBody             []byte
	Response               json.RawMessage
	RequestScopedTransient bool
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
func CopyResponseForRequest(dst http.ResponseWriter, src *http.Response, startedAt time.Time, clientWantsStream bool, firstOutputTimeout ...time.Duration) (ProxyMetrics, error) {
	timeout := time.Duration(0)
	if len(firstOutputTimeout) > 0 {
		timeout = firstOutputTimeout[0]
	}
	contentType := strings.ToLower(src.Header.Get("Content-Type"))
	isSSE := strings.Contains(contentType, "text/event-stream") || (contentType == "" && clientWantsStream)
	if isSSE && !clientWantsStream && src.StatusCode >= 200 && src.StatusCode < 300 {
		return copySSEAsJSON(dst, src, startedAt, timeout)
	}
	if isSSE {
		return copySSEWithPendingLimitAndTimeout(dst, src, startedAt, maxBufferedResponseBytes, timeout)
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
				if firstTokenAt.IsZero() && isClientVisibleOutputEvent(line) {
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
	return copySSEWithPendingLimitAndTimeout(dst, src, startedAt, pendingLimit, 0)
}

func copySSEWithPendingLimitAndTimeout(dst http.ResponseWriter, src *http.Response, startedAt time.Time, pendingLimit int, firstOutputTimeout time.Duration) (ProxyMetrics, error) {
	flusher, _ := dst.(http.Flusher)
	reader := bufio.NewReader(src.Body)
	var firstByteAt, firstTokenAt time.Time
	var terminal terminalResponse
	var topLevelError *responseError
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
	deadline := time.Time{}
	if firstOutputTimeout > 0 {
		deadline = time.Now().Add(firstOutputTimeout)
	}
	for {
		line, err, timedOut := readLimitedLineBefore(reader, deadline, clientOutputStarted)
		if timedOut {
			return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, clientDisconnected), &StreamFailoverError{StatusCode: http.StatusGatewayTimeout, StreamBody: append([]byte(nil), pending...)}
		}
		if len(line) > 0 {
			now := time.Now()
			if firstByteAt.IsZero() {
				firstByteAt = now
			}
			startsOutput := isClientOutputEvent(line)
			if firstTokenAt.IsZero() && isClientVisibleOutputEvent(line) {
				firstTokenAt = now
			}
			if parsed, ok := parseResponseUsage(line); ok {
				if parsed.Error == nil && topLevelError != nil {
					parsed.Error = topLevelError
				}
				terminal = parsed
			}
			if !clientOutputStarted && len(line) >= pendingLimit-len(pending) {
				startClientOutput()
			}
			eventType, hasEvent := responseEventType(line)
			if hasEvent && eventType == "error" {
				if parsed := parseSSEError(line); parsed != nil {
					topLevelError = parsed
					terminal.Error = parsed
				}
			}
			if hasEvent && isTerminalResponseEvent(eventType) {
				terminalSeen = true
				if (eventType == "response.completed" || eventType == "response.done") && !clientOutputStarted && terminal.emptyCompleted() {
					pending = append(pending, line...)
					return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, clientDisconnected), &StreamFailoverError{StatusCode: http.StatusBadGateway, StreamBody: append([]byte(nil), pending...)}
				}
				if eventType == "response.failed" && !clientOutputStarted {
					pending = append(pending, line...)
					if status, retryable := retryableTerminalFailure(terminal); retryable {
						return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, clientDisconnected), &StreamFailoverError{StatusCode: status, StreamBody: append([]byte(nil), pending...), RequestScopedTransient: requestScopedCapacityFailure(terminal)}
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
					// A top-level error event is a valid terminal form even when the
					// upstream does not follow it with response.failed. Keep it pending
					// until EOF so the common error+response.failed sequence can still
					// be handled as one terminal response.
					if topLevelError != nil {
						if !clientOutputStarted {
							if status, retryable := retryableTerminalFailure(terminal); retryable {
								return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, clientDisconnected), &StreamFailoverError{StatusCode: status, StreamBody: append([]byte(nil), pending...), RequestScopedTransient: requestScopedCapacityFailure(terminal)}
							}
							startClientOutput()
						}
						return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, clientDisconnected), nil
					}
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
				if topLevelError != nil {
					if !clientOutputStarted {
						if status, retryable := retryableTerminalFailure(terminal); retryable {
							return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, clientDisconnected), &StreamFailoverError{StatusCode: status, StreamBody: append([]byte(nil), pending...), RequestScopedTransient: requestScopedCapacityFailure(terminal)}
						}
						startClientOutput()
					}
					return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, clientDisconnected), nil
				}
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

func copySSEAsJSON(dst http.ResponseWriter, src *http.Response, startedAt time.Time, firstOutputTimeout time.Duration) (ProxyMetrics, error) {
	reader := bufio.NewReader(src.Body)
	var firstByteAt, firstTokenAt time.Time
	var terminal terminalResponse
	var finalResponse json.RawMessage
	var terminalType string
	var topLevelError *responseError
	deadline := time.Time{}
	if firstOutputTimeout > 0 {
		deadline = time.Now().Add(firstOutputTimeout)
	}
	outputSeen := false
	for {
		line, err, timedOut := readLimitedLineBefore(reader, deadline, outputSeen)
		if timedOut {
			return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), &StreamFailoverError{StatusCode: http.StatusGatewayTimeout}
		}
		if len(line) > 0 {
			now := time.Now()
			if firstByteAt.IsZero() {
				firstByteAt = now
			}
			if firstTokenAt.IsZero() && isClientVisibleOutputEvent(line) {
				firstTokenAt = now
			}
			if isClientOutputEvent(line) {
				outputSeen = true
			}
			payload, ok := ssePayload(line)
			if ok {
				var event struct {
					Type     string          `json:"type"`
					Response json.RawMessage `json:"response"`
					Error    *responseError  `json:"error"`
				}
				if json.Unmarshal(payload, &event) == nil {
					if event.Type == "error" && event.Error != nil {
						topLevelError = event.Error
						terminal.Error = event.Error
					}
					if isTerminalResponseEvent(event.Type) && len(event.Response) > 0 && string(event.Response) != "null" {
						terminalType = event.Type
						finalResponse = append(finalResponse[:0], event.Response...)
						if parsed, ok := parseJSONResponseUsage(event.Response); ok {
							if parsed.Error == nil && topLevelError != nil {
								parsed.Error = topLevelError
								responseWithError, marshalErr := responseJSONWithError(event.Response, topLevelError)
								if marshalErr != nil {
									writeProxyJSONError(dst, http.StatusBadGateway, "upstream_error", "upstream error response could not be encoded")
									return proxyMetrics(startedAt, firstByteAt, firstTokenAt, parsed, false), marshalErr
								}
								finalResponse = responseWithError
							}
							terminal = parsed
						}
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
		if topLevelError != nil {
			errorEnvelope, marshalErr := json.Marshal(map[string]any{"error": topLevelError})
			if marshalErr != nil {
				writeProxyJSONError(dst, http.StatusBadGateway, "upstream_error", "upstream error response could not be encoded")
				return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), marshalErr
			}
			status, retryable := retryableTerminalFailure(terminal)
			if retryable {
				return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), &StreamFailoverError{StatusCode: status, Response: errorEnvelope, RequestScopedTransient: requestScopedCapacityFailure(terminal)}
			}
			copyResponseHeaders(dst.Header(), src.Header)
			dst.Header().Set("Content-Type", "application/json")
			dst.WriteHeader(status)
			_, err := dst.Write(errorEnvelope)
			return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), err
		}
		writeProxyJSONError(dst, http.StatusBadGateway, "upstream_error", ErrIncompleteStream.Error())
		return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), ErrIncompleteStream
	}
	if terminalType == "response.failed" {
		status, retryable := retryableTerminalFailure(terminal)
		if retryable {
			return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), &StreamFailoverError{StatusCode: status, Response: append(json.RawMessage(nil), finalResponse...), RequestScopedTransient: requestScopedCapacityFailure(terminal)}
		}
		copyResponseHeaders(dst.Header(), src.Header)
		dst.Header().Set("Content-Type", "application/json")
		dst.WriteHeader(status)
		_, err := dst.Write(finalResponse)
		return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), err
	}
	if (terminalType == "response.completed" || terminalType == "response.done") && terminal.emptyCompleted() {
		return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), &StreamFailoverError{StatusCode: http.StatusBadGateway, Response: append(json.RawMessage(nil), finalResponse...)}
	}
	copyResponseHeaders(dst.Header(), src.Header)
	dst.Header().Set("Content-Type", "application/json")
	dst.WriteHeader(src.StatusCode)
	_, err := dst.Write(finalResponse)
	return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), err
}

type limitedLineResult struct {
	line []byte
	err  error
}

func readLimitedLineBefore(reader *bufio.Reader, deadline time.Time, outputStarted bool) ([]byte, error, bool) {
	if deadline.IsZero() || outputStarted {
		line, err := readLimitedLine(reader)
		return line, err, false
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return nil, nil, true
	}
	result := make(chan limitedLineResult, 1)
	go func() {
		line, err := readLimitedLine(reader)
		result <- limitedLineResult{line: line, err: err}
	}()
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case value := <-result:
		return value.line, value.err, false
	case <-timer.C:
		return nil, nil, true
	}
}

func responseJSONWithError(payload json.RawMessage, responseError *responseError) (json.RawMessage, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, err
	}
	encodedError, err := json.Marshal(responseError)
	if err != nil {
		return nil, err
	}
	response["error"] = encodedError
	return json.Marshal(response)
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
