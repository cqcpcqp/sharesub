package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountIdentityHTTPAndWebSocketParityAndFailover(t *testing.T) {
	for _, mode := range []string{"", "off", "device", "session", "full"} {
		t.Run(mode, func(t *testing.T) {
			inbound := httptest.NewRequest(http.MethodPost, "http://gateway.test/v1/responses", nil)
			inbound.Header.Set("Session-Id", "client-session")
			inbound.Header.Set("Session_Id", "client-session")
			inbound.Header.Set("Thread-Id", "client-thread")
			inbound.Header.Set("X-Codex-Installation-Id", "client-device")
			inbound.Header.Set("X-Codex-Turn-Metadata", `{"session_id":"client-session","thread_id":"client-thread","sandbox":"seatbelt"}`)
			original := []byte(`{"type":"response.create","model":"gpt-5.5","prompt_cache_key":"client-session","input":[{"large":9007199254740993}],"client_metadata":{"session_id":"client-session","thread_id":"client-thread","x-codex-installation-id":"client-device","other":"untouched","x-codex-turn-metadata":"{\"session_id\":\"client-session\",\"thread_id\":\"client-thread\",\"sandbox\":\"seatbelt\"}"}}`)
			var observedHeader http.Header
			var observedBody []byte
			gateway := NewGateway(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				observedHeader = req.Header.Clone()
				observedBody, _ = io.ReadAll(req.Body)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})})
			forward := func(account, key string) (string, string) {
				t.Helper()
				resp, err := gateway.Forward(context.Background(), inbound, original, RequestBilling{Model: "gpt-5.5", PromptCacheKey: "client-session"}, "token", "upstream-account", key, "", CodexFingerprintContext{AccountID: account, Mode: mode})
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				assertIdentityParity(t, observedHeader, observedBody)
				config := ResponsesWebSocketDialConfig{InternalAccountID: account, APIKeyID: key, FingerprintMode: mode, InboundHeader: inbound.Header, ChatGPTAccountID: "upstream-account", AccessToken: "token"}
				frame, err := PrepareResponsesWebSocketFingerprint(&config, original, "client-session")
				if err != nil {
					t.Fatal(err)
				}
				headers, err := responsesWebSocketHeaders(config, "client-session")
				if err != nil {
					t.Fatal(err)
				}
				assertIdentityParity(t, headers, frame)
				for _, name := range []string{"Session-Id", "Session_Id", "Thread-Id", "X-Codex-Installation-Id"} {
					if headers.Get(name) != observedHeader.Get(name) {
						t.Fatalf("HTTP/WS %s differ: %q / %q", name, observedHeader.Get(name), headers.Get(name))
					}
				}
				return observedHeader.Get("Session_Id"), observedHeader.Get("Thread-Id")
			}
			session, thread := forward("account-a", "key-a")
			again, sameThread := forward("account-a", "key-a")
			if session != again || thread != sameThread {
				t.Fatal("same identity changed between requests")
			}
			other, otherThread := forward("account-b", "key-a")
			if session == other || thread == otherThread {
				t.Fatal("account switch reused identity")
			}
			anotherKey, anotherThread := forward("account-a", "key-b")
			if mode == "off" || mode == "" || mode == "device" {
				if session == anotherKey || thread == anotherThread {
					t.Fatal("API keys shared identity")
				}
			}
			if inbound.Header.Get("Session_Id") != "client-session" || !strings.Contains(string(original), `"prompt_cache_key":"client-session"`) {
				t.Fatal("inbound request mutated during retry")
			}
		})
	}
}

func assertIdentityParity(t *testing.T, headers http.Header, body []byte) {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	var metadata map[string]string
	if err := json.Unmarshal(payload["client_metadata"], &metadata); err != nil {
		t.Fatal(err)
	}
	if headers.Get("Session_Id") != metadata["session_id"] || headers.Get("Thread-Id") != metadata["thread_id"] || headers.Get("X-Codex-Installation-Id") != metadata["x-codex-installation-id"] {
		t.Fatalf("header/body identity mismatch: %v / %s", headers, body)
	}
	var cache string
	_ = json.Unmarshal(payload["prompt_cache_key"], &cache)
	if cache != metadata["session_id"] {
		t.Fatalf("cache/session mismatch: %s", body)
	}
	var headerEmbedded, bodyEmbedded map[string]any
	if err := json.Unmarshal([]byte(headers.Get("X-Codex-Turn-Metadata")), &headerEmbedded); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(metadata["x-codex-turn-metadata"]), &bodyEmbedded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"session_id", "thread_id", "sandbox"} {
		if headerEmbedded[field] != bodyEmbedded[field] {
			t.Fatalf("embedded %s mismatch", field)
		}
	}
	if headerEmbedded["session_id"] != metadata["session_id"] || metadata["other"] != "untouched" || !strings.Contains(string(payload["input"]), "9007199254740993") {
		t.Fatalf("request payload changed unexpectedly: %s", body)
	}
}

func TestAccountIdentityOffPreservesDistinctClientSessions(t *testing.T) {
	identity := codexAccountIdentity{accountID: "account", apiKeyID: "key"}
	seen := map[string]bool{}
	for _, session := range []string{"one", "two"} {
		headers := make(http.Header)
		headers.Set("Session_Id", session)
		if err := identity.headers(headers); err != nil {
			t.Fatal(err)
		}
		scoped := headers.Get("Session_Id")
		if scoped == session || seen[scoped] {
			t.Fatal("client sessions were not independently isolated")
		}
		seen[scoped] = true
	}
	fingerprint, err := ResolveCodexFingerprint(CodexFingerprintConfig{AccountID: "account"})
	if err != nil || fingerprint != nil {
		t.Fatalf("default must not converge: %v, %v", fingerprint, err)
	}
	body, err := identity.body([]byte(`{"input":[],"prompt_cache_key":"independent-cache"}`), "session", nil)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	if _, ok := payload["client_metadata"]; ok {
		t.Fatal("off mode injected metadata")
	}
	if payload["prompt_cache_key"] == "independent-cache" || payload["prompt_cache_key"] == identity.scope("session", "session") {
		t.Fatal("independent cache key was not isolated independently")
	}
}

func TestAccountIdentityCompactDoesNotInjectBodyFields(t *testing.T) {
	input := `{"model":"gpt-5.5","input":[]}`
	gateway := NewGateway(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(req.Body)
		if string(data) != input {
			t.Fatalf("compact body changed: %s", data)
		}
		if req.Header.Get("Session_Id") == "" || req.Header.Get("Session_Id") == "client" {
			t.Fatal("compact session not isolated")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})})
	inbound := httptest.NewRequest(http.MethodPost, "http://gateway.test/v1/responses/compact", nil)
	inbound.Header.Set("Session_Id", "client")
	resp, err := gateway.Forward(context.Background(), inbound, []byte(input), RequestBilling{}, "token", "chatgpt", "key", "", CodexFingerprintContext{AccountID: "account", Mode: "off"})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestAccountIdentityWebSocketUsesOriginalCacheSession(t *testing.T) {
	for _, mode := range []string{"off", "device", "session", "full"} {
		t.Run(mode, func(t *testing.T) {
			config := ResponsesWebSocketDialConfig{InternalAccountID: "account", APIKeyID: "key", FingerprintMode: mode, InboundHeader: make(http.Header)}
			frame, err := PrepareResponsesWebSocketFingerprint(&config, []byte(`{"type":"response.create","model":"gpt-5.5","prompt_cache_key":"original-session"}`), "original-session")
			if err != nil {
				t.Fatal(err)
			}
			// The relay extracts the billing/cache key from the already rewritten frame.
			var payload map[string]any
			if err := json.Unmarshal(frame, &payload); err != nil {
				t.Fatal(err)
			}
			cache := payload["prompt_cache_key"].(string)
			headers, err := responsesWebSocketHeaders(config, cache)
			if err != nil {
				t.Fatal(err)
			}
			if headers.Get("Session_Id") != cache {
				t.Fatalf("session was isolated twice: %v / %s", headers, frame)
			}
		})
	}
}
