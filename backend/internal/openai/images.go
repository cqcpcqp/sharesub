package openai

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	imagesGenerationsEndpoint = "/v1/images/generations"
	imagesEditsEndpoint       = "/v1/images/edits"
	imagesResponsesModel      = "gpt-5.4-mini"
	maxImageUploadBytes       = 20 << 20
)

// ImagesRequest is the validated OpenAI Images request metadata needed while
// converting the ChatGPT Responses stream back to the Images API shape.
type ImagesRequest struct {
	Endpoint       string
	Model          string
	Prompt         string
	Stream         bool
	N              int
	Size           string
	ResponseFormat string
	Quality        string
	Background     string
	OutputFormat   string
	Moderation     string
	Style          string
	Compression    *int
	PartialImages  *int
	InputImages    []string
	MaskImage      string
}

func (r ImagesRequest) IsEdit() bool { return r.Endpoint == imagesEditsEndpoint }

func (r ImagesRequest) sessionSeed() string {
	return strings.Join([]string{"openai-images", r.Endpoint, r.Model, r.Size, r.Prompt}, "|")
}

// PrepareImagesRequest validates an Images request and converts it to the
// hosted image_generation tool request accepted by the ChatGPT Responses API.
func PrepareImagesRequest(body []byte, contentType, path string) ([]byte, ImagesRequest, RequestBilling, error) {
	request := ImagesRequest{Endpoint: normalizeImagesEndpoint(path), N: 1}
	if request.Endpoint == "" {
		return nil, ImagesRequest{}, RequestBilling{}, fmt.Errorf("unsupported images endpoint")
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err == nil && mediaType == "multipart/form-data" {
		if err := parseMultipartImagesRequest(body, params["boundary"], &request); err != nil {
			return nil, ImagesRequest{}, RequestBilling{}, err
		}
	} else {
		if err := parseJSONImagesRequest(body, &request); err != nil {
			return nil, ImagesRequest{}, RequestBilling{}, err
		}
	}
	if request.Model == "" {
		request.Model = "gpt-image-2"
	}
	if !strings.HasPrefix(strings.ToLower(request.Model), "gpt-image-") {
		return nil, ImagesRequest{}, RequestBilling{}, fmt.Errorf("images endpoint requires a gpt-image model, got %q", request.Model)
	}
	if request.Prompt == "" {
		return nil, ImagesRequest{}, RequestBilling{}, fmt.Errorf("prompt is required")
	}
	if request.IsEdit() && len(request.InputImages) == 0 {
		return nil, ImagesRequest{}, RequestBilling{}, fmt.Errorf("image input is required")
	}
	forward, err := buildImagesResponsesRequest(request)
	if err != nil {
		return nil, ImagesRequest{}, RequestBilling{}, err
	}
	return forward, request, RequestBilling{Model: request.Model, PromptCacheKey: request.sessionSeed(), Stream: request.Stream}, nil
}

func normalizeImagesEndpoint(path string) string {
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	switch {
	case strings.HasSuffix(path, "/images/generations"):
		return imagesGenerationsEndpoint
	case strings.HasSuffix(path, "/images/edits"):
		return imagesEditsEndpoint
	default:
		return ""
	}
}

type jsonImagesRequest struct {
	Model             string `json:"model"`
	Prompt            string `json:"prompt"`
	Stream            *bool  `json:"stream"`
	N                 *int   `json:"n"`
	Size              string `json:"size"`
	ResponseFormat    string `json:"response_format"`
	Quality           string `json:"quality"`
	Background        string `json:"background"`
	OutputFormat      string `json:"output_format"`
	Moderation        string `json:"moderation"`
	Style             string `json:"style"`
	OutputCompression *int   `json:"output_compression"`
	PartialImages     *int   `json:"partial_images"`
	Images            []struct {
		ImageURL string `json:"image_url"`
	} `json:"images"`
	Mask *struct {
		ImageURL string `json:"image_url"`
	} `json:"mask"`
}

func parseJSONImagesRequest(body []byte, request *ImagesRequest) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var input jsonImagesRequest
	if err := decoder.Decode(&input); err != nil {
		return fmt.Errorf("parse images request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("images request must contain one JSON object")
	}
	request.Model = strings.TrimSpace(input.Model)
	request.Prompt = strings.TrimSpace(input.Prompt)
	if input.Stream != nil {
		request.Stream = *input.Stream
	}
	if input.N != nil {
		if *input.N <= 0 {
			return fmt.Errorf("n must be greater than 0")
		}
		request.N = *input.N
	}
	request.Size = strings.TrimSpace(input.Size)
	request.ResponseFormat = strings.ToLower(strings.TrimSpace(input.ResponseFormat))
	request.Quality = strings.TrimSpace(input.Quality)
	request.Background = strings.TrimSpace(input.Background)
	request.OutputFormat = strings.TrimSpace(input.OutputFormat)
	request.Moderation = strings.TrimSpace(input.Moderation)
	request.Style = strings.TrimSpace(input.Style)
	request.Compression = input.OutputCompression
	request.PartialImages = input.PartialImages
	for _, image := range input.Images {
		if imageURL := strings.TrimSpace(image.ImageURL); imageURL != "" {
			request.InputImages = append(request.InputImages, imageURL)
		}
	}
	if input.Mask != nil {
		request.MaskImage = strings.TrimSpace(input.Mask.ImageURL)
	}
	return nil
}

func parseMultipartImagesRequest(body []byte, boundary string, request *ImagesRequest) error {
	if strings.TrimSpace(boundary) == "" {
		return fmt.Errorf("multipart boundary is required")
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read multipart images request: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(part, maxImageUploadBytes+1))
		_ = part.Close()
		if err != nil {
			return fmt.Errorf("read multipart field %q: %w", part.FormName(), err)
		}
		if len(data) > maxImageUploadBytes {
			return fmt.Errorf("multipart field %q exceeds 20 MiB", part.FormName())
		}
		name := part.FormName()
		if part.FileName() != "" {
			contentType := strings.TrimSpace(part.Header.Get("Content-Type"))
			if contentType == "" {
				contentType = http.DetectContentType(data)
			}
			dataURL := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
			switch {
			case name == "image" || strings.HasPrefix(name, "image["):
				request.InputImages = append(request.InputImages, dataURL)
			case name == "mask":
				request.MaskImage = dataURL
			}
			continue
		}
		value := strings.TrimSpace(string(data))
		switch name {
		case "model":
			request.Model = value
		case "prompt":
			request.Prompt = value
		case "stream":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("invalid stream field value")
			}
			request.Stream = parsed
		case "n":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("n must be a positive integer")
			}
			request.N = parsed
		case "size":
			request.Size = value
		case "response_format":
			request.ResponseFormat = strings.ToLower(value)
		case "quality":
			request.Quality = value
		case "background":
			request.Background = value
		case "output_format":
			request.OutputFormat = value
		case "moderation":
			request.Moderation = value
		case "style":
			request.Style = value
		case "output_compression":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid output_compression field value")
			}
			request.Compression = &parsed
		case "partial_images":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid partial_images field value")
			}
			request.PartialImages = &parsed
		}
	}
	return nil
}

func buildImagesResponsesRequest(request ImagesRequest) ([]byte, error) {
	content := []map[string]any{{"type": "input_text", "text": request.Prompt}}
	for _, imageURL := range request.InputImages {
		content = append(content, map[string]any{"type": "input_image", "image_url": imageURL})
	}
	action := "generate"
	if request.IsEdit() {
		action = "edit"
	}
	tool := map[string]any{"type": "image_generation", "action": action, "model": request.Model}
	if request.N > 1 {
		tool["n"] = request.N
	}
	for key, value := range map[string]string{
		"size": request.Size, "quality": request.Quality, "background": request.Background,
		"output_format": request.OutputFormat, "moderation": request.Moderation, "style": request.Style,
	} {
		if value != "" {
			tool[key] = value
		}
	}
	if request.Compression != nil {
		tool["output_compression"] = *request.Compression
	}
	if request.PartialImages != nil {
		tool["partial_images"] = *request.PartialImages
	}
	if request.MaskImage != "" {
		tool["input_image_mask"] = map[string]string{"image_url": request.MaskImage}
	}
	payload := map[string]any{
		"model": imagesResponsesModel, "store": false, "stream": true,
		"instructions": "", "parallel_tool_calls": true,
		"reasoning": map[string]string{"effort": "medium", "summary": "auto"},
		"include":   []string{"reasoning.encrypted_content"},
		"input":     []any{map[string]any{"type": "message", "role": "user", "content": content}},
		"tools":     []any{tool}, "tool_choice": map[string]string{"type": "image_generation"},
	}
	return json.Marshal(payload)
}

type imagesResult struct {
	Result        string `json:"result"`
	RevisedPrompt string `json:"revised_prompt"`
	OutputFormat  string `json:"output_format"`
	Size          string `json:"size"`
	Background    string `json:"background"`
	Quality       string `json:"quality"`
	Model         string `json:"model"`
}

type imagesTerminalEvent struct {
	Type  string         `json:"type"`
	Error *responseError `json:"error"`
	Item  struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		imagesResult
	} `json:"item"`
	Response struct {
		ID                string `json:"id"`
		CreatedAt         int64  `json:"created_at"`
		Model             string `json:"model"`
		IncompleteDetails struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		ToolUsage struct {
			ImageGen responseUsage `json:"image_gen"`
		} `json:"tool_usage"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			imagesResult
		} `json:"output"`
		Error *responseError `json:"error"`
	} `json:"response"`
	Delta             string `json:"delta"`
	PartialImageB64   string `json:"partial_image_b64"`
	PartialImageIndex int64  `json:"partial_image_index"`
}

// CopyImagesResponse converts the fixed ChatGPT Responses event schema to the
// OpenAI Images response schema while collecting the same gateway metrics.
func CopyImagesResponse(dst http.ResponseWriter, src *http.Response, startedAt time.Time, request ImagesRequest) (ProxyMetrics, error) {
	if src.StatusCode < 200 || src.StatusCode >= 300 {
		return CopyResponseForRequest(dst, src, startedAt, request.Stream)
	}
	reader := bufio.NewReader(src.Body)
	var terminal terminalResponse
	var completed *imagesTerminalEvent
	var outputItemResults []imagesResult
	firstByteAt := time.Time{}
	firstTokenAt := time.Time{}
	streamStarted := false
	createdAt := int64(0)
	var refusal strings.Builder
	flusher, _ := dst.(http.Flusher)
	for {
		line, err := readLimitedLine(reader)
		if len(line) > 0 {
			if firstByteAt.IsZero() {
				firstByteAt = time.Now()
			}
			payload, ok := ssePayload(line)
			if ok {
				var event imagesTerminalEvent
				if json.Unmarshal(payload, &event) != nil {
					return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), fmt.Errorf("parse images upstream event")
				}
				switch event.Type {
				case "response.created", "response.in_progress":
					createdAt = event.Response.CreatedAt
				case "response.output_text.delta":
					appendImagesRefusalText(&refusal, event.Delta)
				case "response.image_generation_call.partial_image":
					if request.Stream {
						if firstTokenAt.IsZero() {
							firstTokenAt = time.Now()
						}
						if !streamStarted {
							copyResponseHeaders(dst.Header(), src.Header)
							dst.Header().Set("Content-Type", "text/event-stream")
							dst.WriteHeader(src.StatusCode)
							streamStarted = true
						}
						if err := writeImagesStreamEvent(dst, request, "partial_image", map[string]any{
							"created_at": createdAt, "partial_image_index": event.PartialImageIndex, "b64_json": event.PartialImageB64,
						}); err != nil {
							return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, true), err
						}
						if flusher != nil {
							flusher.Flush()
						}
					}
				case "response.output_item.done":
					if event.Item.Type == "image_generation_call" && event.Item.Result != "" {
						outputItemResults = append(outputItemResults, event.Item.imagesResult)
					} else if event.Item.Type == "message" {
						appendImagesContentText(&refusal, event.Item.Content)
					}
				case "response.completed":
					completed = &event
					createdAt = event.Response.CreatedAt
					terminal = terminalFromImagesEvent(event)
					for _, item := range event.Response.Output {
						if item.Type == "message" {
							appendImagesContentText(&refusal, item.Content)
						}
					}
				case "error":
					if event.Error != nil {
						terminal.Error = event.Error
						return finishImagesFailure(dst, src, request, startedAt, firstByteAt, firstTokenAt, terminal, streamStarted, event.Response)
					}
				case "response.failed":
					terminal = terminalFromImagesEvent(event)
					if terminal.Error == nil {
						terminal.Error = &responseError{Type: "server_error", Code: "response_failed", Message: "Upstream image generation failed"}
					}
					return finishImagesFailure(dst, src, request, startedAt, firstByteAt, firstTokenAt, terminal, streamStarted, event.Response)
				case "response.incomplete":
					terminal = terminalFromImagesEvent(event)
					terminal.Error = imagesIncompleteError(event.Response.IncompleteDetails.Reason)
					return finishImagesFailure(dst, src, request, startedAt, firstByteAt, firstTokenAt, terminal, streamStarted, event.Response)
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				if !streamStarted {
					terminal.Error = &responseError{Type: "server_error", Code: "upstream_stream_read_error", Message: "Upstream image response stream could not be read"}
					return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), &StreamFailoverError{StatusCode: http.StatusBadGateway}
				}
				return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), err
			}
			break
		}
	}
	if completed == nil {
		if !streamStarted {
			terminal.Error = &responseError{Type: "server_error", Code: "upstream_stream_incomplete", Message: ErrIncompleteStream.Error()}
			return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), &StreamFailoverError{StatusCode: http.StatusBadGateway}
		}
		_ = writeImagesStreamError(dst, &responseError{Code: "upstream_error", Message: ErrIncompleteStream.Error()})
		return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), ErrIncompleteStream
	}
	results := make([]imagesResult, 0, len(completed.Response.Output))
	for _, output := range completed.Response.Output {
		if output.Type == "image_generation_call" && output.Result != "" {
			results = append(results, output.imagesResult)
		}
	}
	if len(results) == 0 {
		results = outputItemResults
	}
	if len(results) == 0 {
		if message := strings.TrimSpace(refusal.String()); message != "" {
			terminal.Error = &responseError{Type: "image_generation_user_error", Code: "content_policy_violation", Message: message}
			return finishImagesFailure(dst, src, request, startedAt, firstByteAt, firstTokenAt, terminal, streamStarted, completed.Response)
		}
		if !streamStarted {
			terminal.Error = &responseError{Type: "server_error", Code: "no_image_output", Message: "Upstream completed without image output"}
			return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), &StreamFailoverError{StatusCode: http.StatusBadGateway}
		}
		resultErr := fmt.Errorf("upstream completed without image output")
		_ = writeImagesStreamError(dst, &responseError{Code: "upstream_error", Message: resultErr.Error()})
		return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), resultErr
	}
	imageSize := results[0].Size
	if imageSize == "" {
		imageSize = request.Size
	}
	if firstTokenAt.IsZero() {
		firstTokenAt = time.Now()
	}
	if request.Stream {
		if !streamStarted {
			copyResponseHeaders(dst.Header(), src.Header)
			dst.Header().Set("Content-Type", "text/event-stream")
			dst.WriteHeader(src.StatusCode)
		}
		for _, result := range results {
			payload := imageResultPayload(result, request.ResponseFormat)
			payload["created_at"] = completed.Response.CreatedAt
			for key, value := range map[string]string{
				"background": result.Background, "output_format": result.OutputFormat,
				"quality": result.Quality, "size": result.Size, "model": result.Model,
			} {
				if value != "" {
					payload[key] = value
				}
			}
			if _, ok := payload["model"]; !ok {
				payload["model"] = request.Model
			}
			payload["usage"] = completed.Response.ToolUsage.ImageGen
			if err := writeImagesStreamEvent(dst, request, "completed", payload); err != nil {
				return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, true), err
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
		metrics := proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false)
		metrics.ImageCount = int64(len(results))
		metrics.ImageSize = imageSize
		return metrics, nil
	}
	data := make([]map[string]any, 0, len(results))
	for _, result := range results {
		data = append(data, imageResultPayload(result, request.ResponseFormat))
	}
	response := map[string]any{"created": completed.Response.CreatedAt, "data": data, "model": request.Model}
	first := results[0]
	for key, value := range map[string]string{
		"background": first.Background, "output_format": first.OutputFormat,
		"quality": first.Quality, "size": first.Size,
	} {
		if value != "" {
			response[key] = value
		}
	}
	response["usage"] = completed.Response.ToolUsage.ImageGen
	copyResponseHeaders(dst.Header(), src.Header)
	dst.Header().Set("Content-Type", "application/json")
	dst.WriteHeader(src.StatusCode)
	writeErr := json.NewEncoder(dst).Encode(response)
	metrics := proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false)
	metrics.ImageCount = int64(len(results))
	metrics.ImageSize = imageSize
	return metrics, writeErr
}

func appendImagesRefusalText(dst *strings.Builder, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if dst.Len() > 0 {
		dst.WriteByte(' ')
	}
	dst.WriteString(value)
}

func appendImagesContentText(dst *strings.Builder, content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) {
	for _, part := range content {
		if part.Type == "output_text" {
			appendImagesRefusalText(dst, part.Text)
		}
	}
}

func imagesIncompleteError(reason string) *responseError {
	reason = strings.TrimSpace(reason)
	err := &responseError{Type: "incomplete_error", Code: "response_incomplete", Message: "Upstream did not complete image generation"}
	if reason != "" {
		err.Message = "Upstream image generation incomplete: " + reason
	}
	lower := strings.ToLower(reason)
	if strings.Contains(lower, "content_filter") || strings.Contains(lower, "moderation") {
		err.Type = "image_generation_user_error"
		err.Code = "content_policy_violation"
	}
	return err
}

func finishImagesFailure(dst http.ResponseWriter, src *http.Response, request ImagesRequest, startedAt, firstByteAt, firstTokenAt time.Time, terminal terminalResponse, streamStarted bool, response any) (ProxyMetrics, error) {
	status, retryable := retryableTerminalFailure(terminal)
	if !streamStarted && retryable {
		body, _ := json.Marshal(response)
		return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), &StreamFailoverError{StatusCode: status, Response: body}
	}
	if request.Stream {
		if !streamStarted {
			copyResponseHeaders(dst.Header(), src.Header)
			dst.Header().Set("Content-Type", "text/event-stream")
			dst.WriteHeader(src.StatusCode)
		}
		_ = writeImagesStreamError(dst, terminal.Error)
	} else {
		writeImagesError(dst, status, terminal.Error)
	}
	return proxyMetrics(startedAt, firstByteAt, firstTokenAt, terminal, false), nil
}

func terminalFromImagesEvent(event imagesTerminalEvent) terminalResponse {
	output := make([]responseOutputItem, 0, len(event.Response.Output))
	for _, item := range event.Response.Output {
		if item.Type == "image_generation_call" && item.Result != "" {
			output = append(output, responseOutputItem{Type: item.Type, Status: "completed"})
		}
	}
	return terminalResponse{Model: event.Response.Model, Usage: event.Response.ToolUsage.ImageGen, Output: output, Error: event.Response.Error}
}

func imageResultPayload(result imagesResult, responseFormat string) map[string]any {
	payload := make(map[string]any)
	if strings.EqualFold(responseFormat, "url") {
		payload["url"] = "data:" + imageMIMEType(result.OutputFormat) + ";base64," + result.Result
	} else {
		payload["b64_json"] = result.Result
	}
	if result.RevisedPrompt != "" {
		payload["revised_prompt"] = result.RevisedPrompt
	}
	return payload
}

func imageMIMEType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

func writeImagesStreamEvent(dst io.Writer, request ImagesRequest, suffix string, payload map[string]any) error {
	prefix := "image_generation"
	if request.IsEdit() {
		prefix = "image_edit"
	}
	eventType := prefix + "." + suffix
	payload["type"] = eventType
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(dst, "event: %s\ndata: %s\n\n", eventType, encoded)
	return err
}

func writeImagesError(dst http.ResponseWriter, status int, responseErr *responseError) {
	code, message := "upstream_error", "upstream image generation failed"
	if responseErr != nil {
		if responseErr.Code != "" {
			code = responseErr.Code
		}
		if responseErr.Message != "" {
			message = responseErr.Message
		}
	}
	writeProxyJSONError(dst, status, code, message)
}

func writeImagesStreamError(dst io.Writer, responseErr *responseError) error {
	code, message := "upstream_error", "upstream image generation failed"
	if responseErr != nil {
		if responseErr.Code != "" {
			code = responseErr.Code
		}
		if responseErr.Message != "" {
			message = responseErr.Message
		}
	}
	payload, err := json.Marshal(map[string]any{"type": "error", "error": map[string]any{"type": code, "code": code, "message": message}})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(dst, "event: error\ndata: %s\n\n", payload)
	return err
}
