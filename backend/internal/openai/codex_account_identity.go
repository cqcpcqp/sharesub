package openai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Account isolation is independent of optional fingerprint convergence. Local
// account IDs survive reauthorization; API keys keep sharing clients separate.
type codexAccountIdentity struct {
	accountID string
	apiKeyID  string
}

func (identity codexAccountIdentity) scope(kind, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	// Length-prefixed components avoid ambiguous namespaces.
	return stableCodexUUID(fmt.Sprintf("sharesub:codex-account-identity:v1:%d:%s:%d:%s:%s:%s", len(identity.accountID), identity.accountID, len(identity.apiKeyID), identity.apiKeyID, kind, raw))
}

var codexIdentityFields = []struct{ name, kind string }{
	{"installation_id", "installation"}, {"x-codex-installation-id", "installation"},
	{"session_id", "session"}, {"session-id", "session"},
	{"conversation_id", "thread"}, {"conversation-id", "thread"},
	{"thread_id", "thread"}, {"thread-id", "thread"},
	{"turn_id", "turn"}, {"turn-id", "turn"},
	{"window_id", "window"}, {"x-codex-window-id", "window"},
	{"x-client-request-id", "request"},
}

func (identity codexAccountIdentity) fields(values map[string]json.RawMessage) {
	for _, field := range codexIdentityFields {
		var raw string
		if json.Unmarshal(values[field.name], &raw) == nil && strings.TrimSpace(raw) != "" {
			values[field.name], _ = json.Marshal(identity.scope(field.kind, raw))
		}
	}
}

func (identity codexAccountIdentity) turnMetadata(raw string) (string, error) {
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return "", &CodexFingerprintRequestError{message: "invalid X-Codex-Turn-Metadata", cause: err}
	}
	if metadata == nil {
		return "", &CodexFingerprintRequestError{message: "invalid X-Codex-Turn-Metadata", cause: fmt.Errorf("expected JSON object")}
	}
	identity.fields(metadata)
	data, err := json.Marshal(metadata)
	return string(data), err
}

func (identity codexAccountIdentity) headers(headers http.Header) error {
	if identity.accountID == "" {
		return nil
	}
	for _, field := range codexIdentityFields {
		if raw := headers.Get(field.name); strings.TrimSpace(raw) != "" {
			headers.Set(field.name, identity.scope(field.kind, raw))
		}
	}
	if raw := headers.Get("X-Codex-Turn-Metadata"); strings.TrimSpace(raw) != "" {
		scoped, err := identity.turnMetadata(raw)
		if err != nil {
			return err
		}
		headers.Set("X-Codex-Turn-Metadata", scoped)
	}
	return nil
}

// Only identity fields are rewritten. RawMessage preserves arbitrary input/tool
// payloads, including large integers, without a float64 round trip.
func (identity codexAccountIdentity) body(body []byte, clientSessionID string, fingerprint *CodexFingerprint) ([]byte, error) {
	if identity.accountID == "" {
		return body, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse Codex request for account isolation: %w", err)
	}
	if payload == nil {
		return nil, fmt.Errorf("Codex request must be a JSON object")
	}
	var metadata map[string]json.RawMessage
	originalSession := ""
	if raw, ok := payload["client_metadata"]; ok {
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return nil, &CodexFingerprintRequestError{message: "invalid Codex client_metadata", cause: err}
		}
		if metadata != nil {
			_ = json.Unmarshal(metadata["session_id"], &originalSession)
			identity.fields(metadata)
			var embedded string
			if json.Unmarshal(metadata["x-codex-turn-metadata"], &embedded) == nil && strings.TrimSpace(embedded) != "" {
				scoped, err := identity.turnMetadata(embedded)
				if err != nil {
					return nil, err
				}
				metadata["x-codex-turn-metadata"], _ = json.Marshal(scoped)
			}
			payload["client_metadata"], _ = json.Marshal(metadata)
		}
	}
	var cacheKey string
	if json.Unmarshal(payload["prompt_cache_key"], &cacheKey) == nil && strings.TrimSpace(cacheKey) != "" {
		kind := "prompt-cache"
		if cacheKey == originalSession || cacheKey == clientSessionID {
			kind = "session"
		}
		scoped := identity.scope(kind, cacheKey)
		if kind == "session" && fingerprint != nil && fingerprint.sessionID != "" {
			scoped = fingerprint.sessionID
		}
		payload["prompt_cache_key"], _ = json.Marshal(scoped)
	}
	return json.Marshal(payload)
}
