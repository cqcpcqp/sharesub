package openai

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestApplyFastPolicyFiltersMatchingMemberAndModel(t *testing.T) {
	rules := []domain.FastPolicyRule{{
		ServiceTier: "priority", Action: "filter", UserIDs: []string{"member-a"},
		ModelWhitelist: []string{"gpt-5.5*"}, FallbackAction: "pass",
	}}
	body, metadata, err := ApplyFastPolicy([]byte(`{"model":"gpt-5.5-codex","service_tier":"priority"}`), RequestBilling{Model: "gpt-5.5-codex", ServiceTier: "priority"}, rules, nil, "member-a")
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
	_, metadata, err := ApplyFastPolicy([]byte(`{"model":"gpt-5.4","service_tier":"fast"}`), RequestBilling{Model: "gpt-5.4", ServiceTier: "fast"}, rules, nil, "member-a")
	if err != nil || metadata.ServiceTier != "fast" {
		t.Fatalf("member rule result: tier=%q err=%v", metadata.ServiceTier, err)
	}
	_, _, err = ApplyFastPolicy([]byte(`{"model":"gpt-5.4","service_tier":"priority"}`), RequestBilling{Model: "gpt-5.4", ServiceTier: "priority"}, rules, nil, "member-b")
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
	body, metadata, err := ApplyFastPolicy([]byte(`{"model":"gpt-5.4","service_tier":"flex"}`), RequestBilling{Model: "gpt-5.4", ServiceTier: "flex"}, rules, nil, "member")
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

func TestApplyFastPolicyForcesFastWhenServiceTierIsOmitted(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","instructions":"compact"}`)
	updated, metadata, err := ApplyFastPolicy(body, RequestBilling{Model: "gpt-5.4"}, []domain.FastPolicyRule{{ServiceTier: "priority", Action: "force_priority", FallbackAction: "pass"}}, nil, "member")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["service_tier"] != "priority" || metadata.ServiceTier != "priority" {
		t.Fatalf("forced payload = %s metadata=%+v", updated, metadata)
	}
}

func TestApplyFastPolicyDelegatesAccountPassToKeyPolicy(t *testing.T) {
	accountRules := []domain.FastPolicyRule{{ServiceTier: "priority", Action: "pass", FallbackAction: "pass"}}
	keyRules := []domain.FastPolicyRule{{ServiceTier: "priority", Action: "force_priority", FallbackAction: "pass"}}
	body, metadata, err := ApplyFastPolicy([]byte(`{"model":"gpt-5.4"}`), RequestBilling{Model: "gpt-5.4"}, accountRules, keyRules, "member")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"service_tier":"priority"`) || metadata.ServiceTier != "priority" {
		t.Fatalf("layered policy result: body=%s metadata=%+v", body, metadata)
	}
}

func TestApplyFastPolicyAccountDecisionOverridesKeyPolicy(t *testing.T) {
	accountRules := []domain.FastPolicyRule{{ServiceTier: "priority", Action: "filter", FallbackAction: "pass"}}
	keyRules := []domain.FastPolicyRule{{ServiceTier: "priority", Action: "force_priority", FallbackAction: "pass"}}
	body, metadata, err := ApplyFastPolicy([]byte(`{"model":"gpt-5.4","service_tier":"fast"}`), RequestBilling{Model: "gpt-5.4", ServiceTier: "fast"}, accountRules, keyRules, "member")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["service_tier"]; exists || metadata.ServiceTier != "" {
		t.Fatalf("account policy did not override key policy: body=%s metadata=%+v", body, metadata)
	}
}

func TestApplyFastPolicyAccountForceOverridesKeyBlock(t *testing.T) {
	accountRules := []domain.FastPolicyRule{{ServiceTier: "priority", Action: "force_priority", FallbackAction: "pass"}}
	keyRules := []domain.FastPolicyRule{{ServiceTier: "priority", Action: "block", FallbackAction: "pass", ErrorMessage: "key blocked"}}
	body, metadata, err := ApplyFastPolicy([]byte(`{"model":"gpt-5.4","service_tier":"fast"}`), RequestBilling{Model: "gpt-5.4", ServiceTier: "fast"}, accountRules, keyRules, "member")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"service_tier":"priority"`) || metadata.ServiceTier != "priority" {
		t.Fatalf("account force did not override key block: body=%s metadata=%+v", body, metadata)
	}
}

func TestApplyFastPolicyAccountFilterOverridesKeyForceForOmittedTier(t *testing.T) {
	accountRules := []domain.FastPolicyRule{{ServiceTier: "priority", Action: "filter", FallbackAction: "pass"}}
	keyRules := []domain.FastPolicyRule{{ServiceTier: "priority", Action: "force_priority", FallbackAction: "pass"}}
	body, metadata, err := ApplyFastPolicy([]byte(`{"model":"gpt-5.4"}`), RequestBilling{Model: "gpt-5.4"}, accountRules, keyRules, "member")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["service_tier"]; exists || metadata.ServiceTier != "" {
		t.Fatalf("account filter did not override omitted-tier key force: body=%s metadata=%+v", body, metadata)
	}
}

func TestApplyFastPolicyDefaultBehaviorWithoutRules(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		metadataTier    string
		expectedTier    string
		expectedPresent bool
	}{
		{name: "omitted", body: `{"model":"gpt-5.4"}`},
		{name: "priority", body: `{"model":"gpt-5.4","service_tier":"priority"}`, metadataTier: "priority", expectedTier: "priority", expectedPresent: true},
		{name: "fast", body: `{"model":"gpt-5.4","service_tier":"fast"}`, metadataTier: "fast", expectedTier: "fast", expectedPresent: true},
		{name: "flex", body: `{"model":"gpt-5.4","service_tier":"flex"}`, metadataTier: "flex", expectedTier: "flex", expectedPresent: true},
		{name: "auto", body: `{"model":"gpt-5.4","service_tier":"auto"}`, metadataTier: "auto", expectedTier: "auto", expectedPresent: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, metadata, err := ApplyFastPolicy([]byte(test.body), RequestBilling{Model: "gpt-5.4", ServiceTier: test.metadataTier}, nil, nil, "member")
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			actualTier, present := payload["service_tier"]
			if present != test.expectedPresent || (present && actualTier != test.expectedTier) || metadata.ServiceTier != test.expectedTier {
				t.Fatalf("payload tier=%#v present=%v metadata tier=%q", actualTier, present, metadata.ServiceTier)
			}
		})
	}
}
