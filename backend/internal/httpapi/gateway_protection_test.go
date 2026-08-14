package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestGatewayProtectionRateLimitAndBackoff(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	state := newGatewayProtectionState(2)
	state.now = func() time.Time { return now }
	if ok, _ := state.admitAPIKey("key"); !ok {
		t.Fatal("first request rejected")
	}
	if ok, _ := state.admitAPIKey("key"); !ok {
		t.Fatal("second request rejected")
	}
	if ok, retry := state.admitAPIKey("key"); ok || retry != time.Minute {
		t.Fatalf("third request ok=%v retry=%v", ok, retry)
	}
	now = now.Add(time.Minute)
	if ok, _ := state.admitAPIKey("key"); !ok {
		t.Fatal("new minute remained blocked")
	}
	state.backoffAPIKey("key", http.Header{"Retry-After": []string{"30"}})
	if ok, retry := state.admitAPIKey("key"); ok || retry != maximumTransientBackoff {
		t.Fatalf("backoff ok=%v retry=%v", ok, retry)
	}
}

func TestGatewayProtectionModelCooldown(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	state := newGatewayProtectionState(10)
	state.now = func() time.Time { return now }
	state.blockModel("account", "GPT-5.6")
	if !state.modelBlocked("account", "gpt-5.6") || state.modelBlocked("other", "gpt-5.6") {
		t.Fatal("model cooldown scope is incorrect")
	}
	now = now.Add(defaultModelCooldown)
	if state.modelBlocked("account", "gpt-5.6") {
		t.Fatal("expired model cooldown remained active")
	}
}

func TestShouldBlockPlanGatedModelRespectsImagesEndpoint(t *testing.T) {
	if shouldBlockPlanGatedModel(false, "gpt-image-2") {
		t.Fatal("Responses endpoint mismatch cooled down an image model")
	}
	if !shouldBlockPlanGatedModel(true, "gpt-image-2") {
		t.Fatal("Images endpoint capability rejection did not cool down the model")
	}
	if !shouldBlockPlanGatedModel(false, "gpt-5.6-sol") {
		t.Fatal("text model capability rejection did not cool down the model")
	}
}

func TestGatewayTokenRefreshContextUsesDedicatedTimeout(t *testing.T) {
	ctx, cancel := gatewayTokenRefreshContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("gateway token refresh context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining < gatewayTokenRefreshTimeout-time.Second || remaining > gatewayTokenRefreshTimeout {
		t.Fatalf("gateway token refresh timeout = %v", remaining)
	}
	if remaining <= gatewayMetricWriteTimeout {
		t.Fatalf("gateway token refresh timeout %v still uses metric timeout %v", remaining, gatewayMetricWriteTimeout)
	}
}
