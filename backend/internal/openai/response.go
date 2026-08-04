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
		line, err := readLimitedLine(reader)
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
		line, err := readLimitedLine(reader)
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
