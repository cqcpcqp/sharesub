package openai

import (
	"net/http"
	"testing"
)

func TestCodexProtectionErrorClassification(t *testing.T) {
	if !IsCodexPlanGatedModelError(http.StatusBadRequest, []byte(`{"detail":"The 'gpt-5.6' model is not supported when using Codex with a ChatGPT account."}`)) {
		t.Fatal("plan-gated model error was not detected")
	}
	if IsCodexPlanGatedModelError(http.StatusForbidden, []byte(`{"detail":"model is not supported when using Codex"}`)) {
		t.Fatal("wrong status matched plan-gated error")
	}
	if !IsRevokedCodexTokenError(http.StatusUnauthorized, []byte(`{"error":{"code":"token_revoked"}}`)) {
		t.Fatal("revoked token was not detected")
	}
	if IsRevokedCodexTokenError(http.StatusUnauthorized, []byte(`<html>Unauthorized</html>`)) {
		t.Fatal("unstructured 401 was treated as revoked token")
	}
}
