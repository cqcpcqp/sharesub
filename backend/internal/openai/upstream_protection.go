package openai

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

const codexPlanGatedPhrase = "model is not supported when using codex"

func IsCodexPlanGatedModelError(status int, body []byte) bool {
	if status != http.StatusBadRequest {
		return false
	}
	return strings.Contains(normalizeUpstreamMessage(body), codexPlanGatedPhrase)
}

func IsRevokedCodexTokenError(status int, body []byte) bool {
	if status != http.StatusUnauthorized {
		return false
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(body), &envelope) != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(envelope.Error.Code)) {
	case "token_revoked", "token_invalidated", "refresh_token_invalidated":
		return true
	default:
		return false
	}
}

func normalizeUpstreamMessage(body []byte) string {
	normalized := strings.ToLower(string(body))
	normalized = strings.NewReplacer("_", " ", "-", " ", "\n", " ", "\r", " ", "\t", " ").Replace(normalized)
	return strings.Join(strings.Fields(normalized), " ")
}
