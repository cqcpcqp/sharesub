package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPrepareImagesGenerationRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","n":2,"size":"2048x1152","quality":"high","response_format":"url","output_format":"webp"}`)
	forward, request, metadata, err := PrepareImagesRequest(body, "application/json", "/v1/images/generations")
	if err != nil {
		t.Fatal(err)
	}
	if request.Model != "gpt-image-2" || request.N != 2 || request.ResponseFormat != "url" || metadata.Model != "gpt-image-2" {
		t.Fatalf("request = %+v, metadata = %+v", request, metadata)
	}
	var payload struct {
		Model      string `json:"model"`
		Stream     bool   `json:"stream"`
		ToolChoice struct {
			Type string `json:"type"`
		} `json:"tool_choice"`
		Tools []struct {
			Type         string `json:"type"`
			Action       string `json:"action"`
			Model        string `json:"model"`
			N            int    `json:"n"`
			Size         string `json:"size"`
			Quality      string `json:"quality"`
			OutputFormat string `json:"output_format"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(forward, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != imagesResponsesModel || !payload.Stream || payload.ToolChoice.Type != "image_generation" || len(payload.Tools) != 1 {
		t.Fatalf("forward payload = %+v", payload)
	}
	tool := payload.Tools[0]
	if tool.Type != "image_generation" || tool.Action != "generate" || tool.Model != "gpt-image-2" || tool.N != 2 || tool.Size != "2048x1152" || tool.Quality != "high" || tool.OutputFormat != "webp" {
		t.Fatalf("tool = %+v", tool)
	}
}

func TestPrepareImagesMultipartEditRequest(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", "gpt-image-1.5")
	_ = writer.WriteField("prompt", "replace the sky")
	image, _ := writer.CreateFormFile("image", "source.png")
	_, _ = image.Write([]byte("image bytes"))
	mask, _ := writer.CreateFormFile("mask", "mask.png")
	_, _ = mask.Write([]byte("mask bytes"))
	_ = writer.Close()
	forward, request, _, err := PrepareImagesRequest(body.Bytes(), writer.FormDataContentType(), "/images/edits")
	if err != nil {
		t.Fatal(err)
	}
	if !request.IsEdit() || len(request.InputImages) != 1 || !strings.HasPrefix(request.InputImages[0], "data:") || !strings.HasPrefix(request.MaskImage, "data:") {
		t.Fatalf("request = %+v", request)
	}
	var payload struct {
		Tools []struct {
			Action string `json:"action"`
			Mask   struct {
				ImageURL string `json:"image_url"`
			} `json:"input_image_mask"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(forward, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Tools[0].Action != "edit" || !strings.HasPrefix(payload.Tools[0].Mask.ImageURL, "data:") {
		t.Fatalf("tool = %+v", payload.Tools[0])
	}
}

func TestPrepareImagesRequestRejectsNonImageModel(t *testing.T) {
	_, _, _, err := PrepareImagesRequest([]byte(`{"model":"gpt-5.4","prompt":"draw"}`), "application/json", "/v1/images/generations")
	if err == nil {
		t.Fatal("non-image model was accepted")
	}
}

func TestCopyImagesResponseAsJSON(t *testing.T) {
	body := `data: {"type":"response.completed","response":{"created_at":1710000000,"model":"gpt-5.4-mini","usage":{"input_tokens":10,"output_tokens":20,"output_tokens_details":{"image_tokens":12}},"tool_usage":{"image_gen":{"input_tokens":46,"output_tokens":2459,"output_tokens_details":{"image_tokens":2459},"images":1}},"output":[{"type":"image_generation_call","result":"aW1hZ2U=","revised_prompt":"a cat","output_format":"png","size":"1024x1024","quality":"high"}]}}` + "\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	metrics, err := CopyImagesResponse(recorder, source, time.Now(), ImagesRequest{Endpoint: imagesGenerationsEndpoint, Model: "gpt-image-2"})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.ImageCount != 1 || metrics.InputTokens != 46 || metrics.OutputTokens != 2459 || metrics.ImageOutputTokens != 2459 {
		t.Fatalf("metrics = %+v", metrics)
	}
	var response struct {
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Size    string `json:"size"`
		Data    []struct {
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Created != 1710000000 || response.Model != "gpt-image-2" || response.Size != "1024x1024" || len(response.Data) != 1 || response.Data[0].B64JSON != "aW1hZ2U=" || response.Data[0].RevisedPrompt != "a cat" {
		t.Fatalf("response = %+v", response)
	}
}

func TestCopyImagesResponseStreamsImageEvents(t *testing.T) {
	body := "data: {\"type\":\"response.created\",\"response\":{\"created_at\":1710000000}}\n\n" +
		"data: {\"type\":\"response.image_generation_call.partial_image\",\"partial_image_index\":0,\"partial_image_b64\":\"cGFydA==\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000000,\"model\":\"gpt-5.4-mini\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2},\"tool_usage\":{\"image_gen\":{\"input_tokens\":3,\"output_tokens\":4,\"images\":1}},\"output\":[{\"type\":\"image_generation_call\",\"result\":\"ZmluYWw=\",\"output_format\":\"png\"}]}}\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	_, err := CopyImagesResponse(recorder, source, time.Now(), ImagesRequest{Endpoint: imagesGenerationsEndpoint, Model: "gpt-image-2", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	got := recorder.Body.String()
	if !strings.Contains(got, "event: image_generation.partial_image") || !strings.Contains(got, `"partial_image_index":0`) || !strings.Contains(got, "event: image_generation.completed") || !strings.Contains(got, `"b64_json":"ZmluYWw="`) || !strings.Contains(got, `"model":"gpt-image-2"`) {
		t.Fatalf("stream = %s", got)
	}
}

func TestCopyImagesResponseReturnsFailoverForRetryableTerminalFailure(t *testing.T) {
	body := `data: {"type":"response.failed","response":{"model":"gpt-5.4-mini","error":{"code":"rate_limit_exceeded","message":"limited"}}}` + "\n\n"
	source := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	recorder := httptest.NewRecorder()
	_, err := CopyImagesResponse(recorder, source, time.Now(), ImagesRequest{Endpoint: imagesGenerationsEndpoint, Model: "gpt-image-2"})
	failover, ok := err.(*StreamFailoverError)
	if !ok || failover.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("error = %#v", err)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}
