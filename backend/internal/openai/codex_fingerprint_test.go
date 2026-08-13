package openai

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCodexFingerprintSessionConvergesAccountAndIsolatesAPIKeys(t *testing.T) {
	first, err := ResolveCodexFingerprint(CodexFingerprintConfig{AccountID: "account", APIKeyID: "key-a", Mode: "session", ClientSessionID: "client-session"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveCodexFingerprint(CodexFingerprintConfig{AccountID: "account", APIKeyID: "key-b", Mode: "session", ClientSessionID: "client-session"})
	if err != nil {
		t.Fatal(err)
	}
	if first.installationID != second.installationID || first.sessionID != second.sessionID {
		t.Fatal("same account did not converge installation and session IDs")
	}
	if first.threadID == second.threadID {
		t.Fatal("different API keys received the same thread ID")
	}
}

func TestCodexFingerprintSessionKeepsThreadStableAndTurnUnique(t *testing.T) {
	config := CodexFingerprintConfig{AccountID: "account", APIKeyID: "key", Mode: "session", ClientSessionID: "client-session"}
	first, err := ResolveCodexFingerprint(config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveCodexFingerprint(config)
	if err != nil {
		t.Fatal(err)
	}
	if first.threadID != second.threadID || first.sessionID != second.sessionID {
		t.Fatal("stable session inputs did not produce stable session and thread IDs")
	}
	if first.turnID == second.turnID {
		t.Fatal("separate turns received the same turn ID")
	}
}

func TestCodexFingerprintHeaderAndBodyStayConsistent(t *testing.T) {
	fingerprint, err := ResolveCodexFingerprint(CodexFingerprintConfig{AccountID: "account", APIKeyID: "key", Mode: "session", ClientSessionID: "client-session"})
	if err != nil {
		t.Fatal(err)
	}
	headers := make(http.Header)
	headers.Set("X-Codex-Turn-Metadata", `{"sandbox":"seatbelt","thread_id":"client"}`)
	if err := ApplyCodexFingerprintHeaders(headers, fingerprint); err != nil {
		t.Fatal(err)
	}
	body, err := ApplyCodexFingerprintBody([]byte(`{"model":"gpt-5.5","client_metadata":{"x-codex-turn-metadata":"{\"sandbox\":\"seatbelt\"}"}}`), fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	clientMetadata := payload["client_metadata"].(map[string]any)
	if headers.Get("Session_Id") != clientMetadata["session_id"] || headers.Get("Thread-Id") != clientMetadata["thread_id"] {
		t.Fatalf("header/body fingerprint mismatch: headers=%v body=%v", headers, clientMetadata)
	}
	var headerMetadata, bodyMetadata map[string]any
	if err := json.Unmarshal([]byte(headers.Get("X-Codex-Turn-Metadata")), &headerMetadata); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(clientMetadata["x-codex-turn-metadata"].(string)), &bodyMetadata); err != nil {
		t.Fatal(err)
	}
	if headerMetadata["turn_id"] != bodyMetadata["turn_id"] || headerMetadata["thread_id"] != bodyMetadata["thread_id"] {
		t.Fatalf("embedded metadata mismatch: header=%v body=%v", headerMetadata, bodyMetadata)
	}
}

func TestCodexFingerprintRejectsInvalidTurnMetadata(t *testing.T) {
	fingerprint, err := ResolveCodexFingerprint(CodexFingerprintConfig{AccountID: "account", APIKeyID: "key", Mode: "session", ClientSessionID: "client-session"})
	if err != nil {
		t.Fatal(err)
	}
	headers := make(http.Header)
	headers.Set("X-Codex-Turn-Metadata", "not-json")
	if err := ApplyCodexFingerprintHeaders(headers, fingerprint); err == nil {
		t.Fatal("invalid turn metadata was accepted")
	}
}

func TestCodexFingerprintModes(t *testing.T) {
	if fingerprint, err := ResolveCodexFingerprint(CodexFingerprintConfig{Mode: "off"}); err != nil || fingerprint != nil {
		t.Fatalf("off mode = %#v, %v", fingerprint, err)
	}
	device, err := ResolveCodexFingerprint(CodexFingerprintConfig{AccountID: "account", APIKeyID: "key", Mode: "device"})
	if err != nil || device.installationID == "" || device.sessionID != "" || device.threadID != "" {
		t.Fatalf("device mode = %#v, %v", device, err)
	}
	full, err := ResolveCodexFingerprint(CodexFingerprintConfig{AccountID: "account", APIKeyID: "key", Mode: "full"})
	if err != nil || full.threadID != full.sessionID {
		t.Fatalf("full mode = %#v, %v", full, err)
	}
}

func TestPrepareResponsesWebSocketFingerprintSharesHeaderAndFrameIDs(t *testing.T) {
	inbound := make(http.Header)
	inbound.Set("session_id", "client-session")
	inbound.Set("X-Codex-Turn-Metadata", `{"sandbox":"seatbelt"}`)
	config := ResponsesWebSocketDialConfig{
		AccessToken: "access", ChatGPTAccountID: "chatgpt-account", APIKeyID: "key",
		InternalAccountID: "account", FingerprintMode: "session", InboundHeader: inbound,
	}
	frame, err := PrepareResponsesWebSocketFingerprint(&config, []byte(`{"type":"response.create","model":"gpt-5.5"}`), "cache")
	if err != nil {
		t.Fatal(err)
	}
	headers, err := responsesWebSocketHeaders(config, "cache")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(frame, &payload); err != nil {
		t.Fatal(err)
	}
	clientMetadata := payload["client_metadata"].(map[string]any)
	if headers.Get("Session_Id") != clientMetadata["session_id"] || headers.Get("Thread-Id") != clientMetadata["thread_id"] {
		t.Fatalf("WebSocket header/frame mismatch: headers=%v frame=%v", headers, clientMetadata)
	}
}

func TestCodexFingerprintOffLeavesRequestUntouched(t *testing.T) {
	fingerprint, err := ResolveCodexFingerprint(CodexFingerprintConfig{Mode: "off"})
	if err != nil {
		t.Fatal(err)
	}
	headers := make(http.Header)
	headers.Set("Session_Id", "client-session")
	body := []byte(`{"model":"gpt-5.5","client_metadata":{"session_id":"client-session"}}`)
	if err := ApplyCodexFingerprintHeaders(headers, fingerprint); err != nil {
		t.Fatal(err)
	}
	updated, err := ApplyCodexFingerprintBody(body, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get("Session_Id") != "client-session" || string(updated) != string(body) {
		t.Fatalf("off mode changed request: headers=%v body=%s", headers, updated)
	}
}

func TestResponsesWebSocketOffPreservesClientSessionHeaders(t *testing.T) {
	inbound := make(http.Header)
	inbound.Set("session_id", "client-session")
	inbound.Set("conversation_id", "client-conversation")
	headers, err := responsesWebSocketHeaders(ResponsesWebSocketDialConfig{
		AccessToken: "access", ChatGPTAccountID: "chatgpt-account", APIKeyID: "key",
		InternalAccountID: "account", FingerprintMode: "off", InboundHeader: inbound,
	}, "cache")
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get("session_id") != "client-session" || headers.Get("conversation_id") != "client-conversation" {
		t.Fatalf("off mode did not preserve client session headers: %v", headers)
	}
}
