package openai

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestGPT62ReasoningAcrossProtocols(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"} {
			for _, protocol := range []string{"responses", "compact", "websocket"} {
				t.Run(model+"/"+effort+"/"+protocol, func(t *testing.T) {
					body := []byte(fmt.Sprintf(`{"type":"response.create","model":%q,"input":[],"reasoning":{"effort":%q}}`, model, effort))
					var normalized []byte
					var metadata RequestBilling
					var err error
					if protocol == "websocket" {
						normalized, metadata, _, err = prepareResponsesWebSocketFrame(body, 1, "")
					} else {
						normalized, metadata, err = PrepareRequest(body, protocol == "compact")
					}
					if effort == "none" || effort == "minimal" {
						if err == nil || !strings.Contains(err.Error(), model+" does not support reasoning effort") {
							t.Fatalf("expected unsupported reasoning error, got %v", err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					var payload struct {
						Model     string
						Reasoning struct{ Effort string }
					}
					if err := json.Unmarshal(normalized, &payload); err != nil {
						t.Fatal(err)
					}
					if payload.Model != model || metadata.Model != model || payload.Reasoning.Effort != effort {
						t.Fatalf("model/effort changed: %s metadata=%+v", normalized, metadata)
					}
				})
			}
		}
	}
}
