package openai

import (
	"net/url"
	"testing"
)

func TestAuthorizationURL(t *testing.T) {
	client := NewOAuthClient("")
	raw := client.AuthorizationURL("state", "challenge", "http://localhost/callback")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.String() == "" || query.Get("client_id") != clientID || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("unexpected authorization URL: %s", raw)
	}
	if query.Get("codex_cli_simplified_flow") != "true" {
		t.Fatal("Codex simplified flow must be enabled")
	}
}
