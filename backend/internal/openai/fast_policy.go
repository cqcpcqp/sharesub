package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type FastPolicyBlockedError struct {
	Message string
}

func (e *FastPolicyBlockedError) Error() string { return e.Message }

func ApplyFastPolicy(body []byte, metadata RequestBilling, rules []domain.FastPolicyRule, userID string) ([]byte, RequestBilling, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, RequestBilling{}, fmt.Errorf("apply OpenAI Fast/Flex policy: %w", err)
	}
	value, exists := payload["service_tier"]
	if !exists {
		return body, metadata, nil
	}
	serviceTier, ok := value.(string)
	if !ok {
		return nil, RequestBilling{}, fmt.Errorf("apply OpenAI Fast/Flex policy: service_tier must be a string")
	}
	rawTier := strings.TrimSpace(serviceTier)
	if rawTier == "" {
		return body, metadata, nil
	}
	tier := strings.ToLower(rawTier)
	if tier == "fast" {
		tier = "priority"
	}
	if tier != "priority" && tier != "flex" {
		return body, metadata, nil
	}

	action, message := resolveFastPolicy(rules, userID, metadata.Model, tier)
	if action == "block" {
		if message == "" {
			message = fmt.Sprintf("OpenAI service_tier=%s is not allowed for model %s", tier, metadata.Model)
		}
		return body, metadata, &FastPolicyBlockedError{Message: message}
	}

	switch action {
	case "filter":
		delete(payload, "service_tier")
		metadata.ServiceTier = ""
	case "force_priority":
		payload["service_tier"] = "priority"
		metadata.ServiceTier = "priority"
	default:
		payload["service_tier"] = tier
		metadata.ServiceTier = tier
	}
	updated, err := json.Marshal(payload)
	if err != nil {
		return nil, RequestBilling{}, fmt.Errorf("encode OpenAI Fast/Flex policy request: %w", err)
	}
	return updated, metadata, nil
}

func resolveFastPolicy(rules []domain.FastPolicyRule, userID, model, tier string) (string, string) {
	for _, userScoped := range []bool{true, false} {
		for _, rule := range rules {
			if (len(rule.UserIDs) > 0) != userScoped || !fastPolicyUserMatches(rule.UserIDs, userID) {
				continue
			}
			if rule.ServiceTier != "all" && rule.ServiceTier != tier {
				continue
			}
			if fastPolicyModelMatches(rule.ModelWhitelist, model) {
				return rule.Action, rule.ErrorMessage
			}
			return rule.FallbackAction, rule.FallbackErrorMessage
		}
	}
	return "pass", ""
}

func fastPolicyUserMatches(userIDs []string, userID string) bool {
	if len(userIDs) == 0 {
		return true
	}
	for _, candidate := range userIDs {
		if candidate == userID {
			return true
		}
	}
	return false
}

func fastPolicyModelMatches(patterns []string, model string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		if pattern == model || (strings.HasSuffix(pattern, "*") && strings.HasPrefix(model, strings.TrimSuffix(pattern, "*"))) {
			return true
		}
	}
	return false
}
