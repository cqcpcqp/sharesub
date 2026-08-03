package openai

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestApplyFastPolicyFiltersMatchingMemberAndModel(t *testing.T) {
	rules := []domain.FastPolicyRule{{
		ServiceTier: "priority", Action: "filter", UserIDs: []string{"member-a"},
		ModelWhitelist: []string{"gpt-5.5*"}, FallbackAction: "pass",
	}}
	body, metadata, err := ApplyFastPolicy([]byte(`{"model":"gpt-5.5-codex","service_tier":"priority"}`), RequestBilling{Model: "gpt-5.5-codex", ServiceTier: "priority"}, rules, "member-a")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["service_tier"]; exists || metadata.ServiceTier != "" {
		t.Fatalf("filtered payload = %s, metadata tier = %q", body, metadata.ServiceTier)
	}
}

func TestApplyFastPolicyUsesMemberRuleBeforeGlobalRule(t *testing.T) {
	rules := []domain.FastPolicyRule{
		{ServiceTier: "priority", Action: "block", UserIDs: []string{}, ModelWhitelist: []string{}, FallbackAction: "pass", ErrorMessage: "global"},
		{ServiceTier: "priority", Action: "pass", UserIDs: []string{"member-a"}, ModelWhitelist: []string{}, FallbackAction: "pass"},
	}
	_, metadata, err := ApplyFastPolicy([]byte(`{"model":"gpt-5.4","service_tier":"fast"}`), RequestBilling{Model: "gpt-5.4", ServiceTier: "fast"}, rules, "member-a")
	if err != nil || metadata.ServiceTier != "priority" {
		t.Fatalf("member rule result: tier=%q err=%v", metadata.ServiceTier, err)
	}
	_, _, err = ApplyFastPolicy([]byte(`{"model":"gpt-5.4","service_tier":"priority"}`), RequestBilling{Model: "gpt-5.4", ServiceTier: "priority"}, rules, "member-b")
	var blocked *FastPolicyBlockedError
	if !errors.As(err, &blocked) || blocked.Message != "global" {
		t.Fatalf("global rule error = %#v", err)
	}
}

func TestApplyFastPolicyUsesModelFallbackAction(t *testing.T) {
	rules := []domain.FastPolicyRule{{
		ServiceTier: "all", Action: "pass", UserIDs: []string{}, ModelWhitelist: []string{"gpt-5.5"},
		FallbackAction: "force_priority",
	}}
	body, metadata, err := ApplyFastPolicy([]byte(`{"model":"gpt-5.4","service_tier":"flex"}`), RequestBilling{Model: "gpt-5.4", ServiceTier: "flex"}, rules, "member")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["service_tier"] != "priority" || metadata.ServiceTier != "priority" {
		t.Fatalf("forced payload = %s, metadata tier = %q", body, metadata.ServiceTier)
	}
}

func TestApplyFastPolicyDoesNotRestoreCompactServiceTier(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","instructions":"compact"}`)
	updated, metadata, err := ApplyFastPolicy(body, RequestBilling{Model: "gpt-5.4", ServiceTier: "priority"}, []domain.FastPolicyRule{{ServiceTier: "priority", Action: "force_priority", FallbackAction: "pass"}}, "member")
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != string(body) || metadata.ServiceTier != "priority" {
		t.Fatalf("compact policy result: body=%s metadata=%+v", updated, metadata)
	}
}
