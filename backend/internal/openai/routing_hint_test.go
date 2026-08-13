package openai

import (
	"net/http"
	"testing"
)

func TestApplyCodexRoutingHint(t *testing.T) {
	headers := http.Header{"x-codex-routing-hint": []string{"model=forged;tier=priority"}}
	applyCodexRoutingHint(headers, "gpt-5.5", "fast")
	if got := headers.Get(codexRoutingHintHeader); got != "model=gpt-5.5;tier=priority" {
		t.Fatalf("routing hint = %q", got)
	}
	applyCodexRoutingHint(headers, "gpt-5.5", "default")
	if got := headers.Get(codexRoutingHintHeader); got != "model=gpt-5.5" {
		t.Fatalf("model-only routing hint = %q", got)
	}
}
