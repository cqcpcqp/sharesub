package openai

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const DefaultCodexFingerprintMode = "session"

type CodexFingerprintRequestError struct {
	message string
	cause   error
}

func (e *CodexFingerprintRequestError) Error() string { return e.message + ": " + e.cause.Error() }
func (e *CodexFingerprintRequestError) Unwrap() error { return e.cause }

type CodexFingerprintConfig struct {
	AccountID       string
	APIKeyID        string
	Mode            string
	ClientSessionID string
}

type CodexFingerprint struct {
	mode           string
	installationID string
	sessionID      string
	threadID       string
	turnID         string
	windowID       string
	turnStartedAt  int64
}

func ResolveCodexFingerprint(config CodexFingerprintConfig) (*CodexFingerprint, error) {
	mode := strings.TrimSpace(config.Mode)
	if mode == "" {
		mode = DefaultCodexFingerprintMode
	}
	if mode == "off" {
		return nil, nil
	}
	if mode != "device" && mode != "session" && mode != "full" {
		return nil, fmt.Errorf("unsupported Codex fingerprint mode %q", mode)
	}
	accountID := strings.TrimSpace(config.AccountID)
	if accountID == "" {
		return nil, fmt.Errorf("Codex fingerprint account ID is required")
	}
	fingerprint := &CodexFingerprint{
		mode:           mode,
		installationID: stableCodexUUID("sharesub:codex-installation:v1:" + accountID),
	}
	if mode == "device" {
		return fingerprint, nil
	}
	fingerprint.sessionID = stableCodexUUID("sharesub:codex-session:v1:" + accountID)
	if mode == "full" {
		fingerprint.threadID = fingerprint.sessionID
	} else {
		clientSessionID := strings.TrimSpace(config.ClientSessionID)
		if clientSessionID == "" {
			clientSessionID = "default"
		}
		fingerprint.threadID = stableCodexUUID("sharesub:codex-thread:v1:" + accountID + ":" + config.APIKeyID + ":" + clientSessionID)
	}
	turnID, err := randomCodexUUID()
	if err != nil {
		return nil, fmt.Errorf("create Codex turn ID: %w", err)
	}
	fingerprint.turnID = turnID
	fingerprint.windowID = fingerprint.threadID + ":0"
	fingerprint.turnStartedAt = time.Now().UnixMilli()
	return fingerprint, nil
}

func ClientCodexSessionID(headers http.Header, promptCacheKey string) string {
	for _, name := range []string{"session-id", "session_id", "conversation-id", "conversation_id"} {
		if value := strings.TrimSpace(headers.Get(name)); value != "" {
			return value
		}
	}
	return strings.TrimSpace(promptCacheKey)
}

func ApplyCodexFingerprintHeaders(headers http.Header, fingerprint *CodexFingerprint) error {
	if headers == nil || fingerprint == nil {
		return nil
	}
	headers.Set("X-Codex-Installation-Id", fingerprint.installationID)
	fields := map[string]any{"installation_id": fingerprint.installationID}
	if fingerprint.mode != "device" {
		headers.Set("Session-Id", fingerprint.sessionID)
		headers.Set("Session_Id", fingerprint.sessionID)
		headers.Set("Conversation-Id", fingerprint.threadID)
		headers.Set("Conversation_Id", fingerprint.threadID)
		headers.Set("Thread-Id", fingerprint.threadID)
		headers.Set("X-Client-Request-Id", fingerprint.threadID)
		headers.Set("X-Codex-Window-Id", fingerprint.windowID)
		fields["session_id"] = fingerprint.sessionID
		fields["thread_id"] = fingerprint.threadID
		fields["turn_id"] = fingerprint.turnID
		fields["window_id"] = fingerprint.windowID
		fields["turn_started_at_unix_ms"] = fingerprint.turnStartedAt
	}
	return rewriteCodexTurnMetadataHeader(headers, fields)
}

func ApplyCodexFingerprintBody(body []byte, fingerprint *CodexFingerprint) ([]byte, error) {
	if fingerprint == nil {
		return body, nil
	}
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("parse Codex request for fingerprinting: %w", err)
	}
	clientMetadata, _ := payload["client_metadata"].(map[string]any)
	if clientMetadata == nil {
		clientMetadata = make(map[string]any)
	}
	clientMetadata["x-codex-installation-id"] = fingerprint.installationID
	fields := map[string]any{"installation_id": fingerprint.installationID}
	if fingerprint.mode != "device" {
		clientMetadata["session_id"] = fingerprint.sessionID
		clientMetadata["thread_id"] = fingerprint.threadID
		clientMetadata["turn_id"] = fingerprint.turnID
		clientMetadata["x-codex-window-id"] = fingerprint.windowID
		fields["session_id"] = fingerprint.sessionID
		fields["thread_id"] = fingerprint.threadID
		fields["turn_id"] = fingerprint.turnID
		fields["window_id"] = fingerprint.windowID
		fields["turn_started_at_unix_ms"] = fingerprint.turnStartedAt
	}
	if raw, ok := clientMetadata["x-codex-turn-metadata"].(string); ok && strings.TrimSpace(raw) != "" {
		rewritten, err := rewriteCodexTurnMetadata(raw, fields)
		if err != nil {
			return nil, err
		}
		clientMetadata["x-codex-turn-metadata"] = rewritten
	}
	payload["client_metadata"] = clientMetadata
	return json.Marshal(payload)
}

func rewriteCodexTurnMetadataHeader(headers http.Header, fields map[string]any) error {
	raw := strings.TrimSpace(headers.Get("X-Codex-Turn-Metadata"))
	if raw == "" {
		return nil
	}
	rewritten, err := rewriteCodexTurnMetadata(raw, fields)
	if err != nil {
		return err
	}
	headers.Set("X-Codex-Turn-Metadata", rewritten)
	return nil
}

func rewriteCodexTurnMetadata(raw string, fields map[string]any) (string, error) {
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return "", &CodexFingerprintRequestError{message: "invalid X-Codex-Turn-Metadata", cause: err}
	}
	if metadata == nil {
		return "", &CodexFingerprintRequestError{message: "invalid X-Codex-Turn-Metadata", cause: fmt.Errorf("expected JSON object")}
	}
	for key, value := range fields {
		metadata[key] = value
	}
	rewritten, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("encode X-Codex-Turn-Metadata: %w", err)
	}
	return string(rewritten), nil
}

func stableCodexUUID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return formatCodexUUID(sum[:16])
}

func randomCodexUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return formatCodexUUID(value), nil
}

func formatCodexUUID(value []byte) string {
	value = append([]byte(nil), value...)
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}
