package openai

import (
	"net/http"
	"strings"
)

const codexRoutingHintHeader = "X-Codex-Routing-Hint"

func applyCodexRoutingHint(headers http.Header, model, serviceTier string) {
	for key := range headers {
		if strings.EqualFold(strings.TrimSpace(key), codexRoutingHintHeader) {
			delete(headers, key)
		}
	}
	model = strings.TrimSpace(model)
	if model == "" || strings.ContainsAny(model, ";=\r\n") {
		return
	}
	tier := strings.ToLower(strings.TrimSpace(serviceTier))
	if tier == "fast" {
		tier = "priority"
	}
	if tier != "priority" && tier != "flex" {
		tier = ""
	}
	hint := "model=" + model
	if tier != "" {
		hint += ";tier=" + tier
	}
	headers.Set(codexRoutingHintHeader, hint)
}
