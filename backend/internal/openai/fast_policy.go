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

func ApplyFastPolicy(body []byte, metadata RequestBilling, accountRules, keyRules []domain.FastPolicyRule, userID string) ([]byte, RequestBilling, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, RequestBilling{}, fmt.Errorf("apply OpenAI Fast/Flex policy: %w", err)
	}
	value, exists := payload["service_tier"]
	rawTier := ""
	if exists {
		serviceTier, ok := value.(string)
		if !ok {
			return nil, RequestBilling{}, fmt.Errorf("apply OpenAI Fast/Flex policy: service_tier must be a string")
		}
		rawTier = strings.ToLower(strings.TrimSpace(serviceTier))
	}
	tier := rawTier
	if tier == "fast" {
		tier = "priority"
	}
	if tier != "" && tier != "priority" && tier != "flex" {
		return body, metadata, nil
	}

	changed := false
	accountAction, accountMessage := resolveFastPolicy(accountRules, userID, metadata.Model, tier)
	if accountAction != "pass" {
		if err := fastPolicyBlockError(accountAction, accountMessage, tier, metadata.Model); err != nil {
			return body, metadata, err
		}
		_, changed = applyFastPolicyAction(payload, &metadata, rawTier, accountAction, changed)
	} else {
		keyAction, keyMessage := resolveFastPolicy(keyRules, userID, metadata.Model, tier)
		if err := fastPolicyBlockError(keyAction, keyMessage, tier, metadata.Model); err != nil {
			return body, metadata, err
		}
		rawTier, changed = applyFastPolicyAction(payload, &metadata, rawTier, keyAction, changed)

		// Re-evaluate the account after a Key transformation so an account filter,
		// block, or force rule remains the final authority over the resulting tier.
		tier = rawTier
		if tier == "fast" {
			tier = "priority"
		}
		accountAction, accountMessage = resolveFastPolicy(accountRules, userID, metadata.Model, tier)
		if err := fastPolicyBlockError(accountAction, accountMessage, tier, metadata.Model); err != nil {
			return body, metadata, err
		}
		_, changed = applyFastPolicyAction(payload, &metadata, rawTier, accountAction, changed)
	}
	if !changed {
		return body, metadata, nil
	}
	updated, err := json.Marshal(payload)
	if err != nil {
		return nil, RequestBilling{}, fmt.Errorf("encode OpenAI Fast/Flex policy request: %w", err)
	}
	return updated, metadata, nil
}

func fastPolicyBlockError(action, message, tier, model string) error {
	if action != "block" {
		return nil
	}
	if message == "" {
		message = fmt.Sprintf("OpenAI service_tier=%s is not allowed for model %s", tier, model)
	}
	return &FastPolicyBlockedError{Message: message}
}

func applyFastPolicyAction(payload map[string]any, metadata *RequestBilling, rawTier, action string, changed bool) (string, bool) {
	switch action {
	case "filter":
		if rawTier != "" {
			delete(payload, "service_tier")
			changed = true
		}
		metadata.ServiceTier = ""
		return "", changed
	case "force_priority":
		payload["service_tier"] = "fast"
		metadata.ServiceTier = "fast"
		return "fast", changed || rawTier != "fast"
	default:
		return rawTier, changed
	}
}

func resolveFastPolicy(rules []domain.FastPolicyRule, userID, model, tier string) (string, string) {
	for _, userScoped := range []bool{true, false} {
		for _, rule := range rules {
			if (len(rule.UserIDs) > 0) != userScoped || !fastPolicyUserMatches(rule.UserIDs, userID) {
				continue
			}
			if tier == "" {
				if rule.ServiceTier != "all" && rule.ServiceTier != "priority" {
					continue
				}
				action, message := fastPolicyModelAction(rule, model)
				if action == "force_priority" {
					return action, message
				}
				continue
			}
			if rule.ServiceTier != "all" && rule.ServiceTier != tier {
				continue
			}
			return fastPolicyModelAction(rule, model)
		}
	}
	return "pass", ""
}

func fastPolicyModelAction(rule domain.FastPolicyRule, model string) (string, string) {
	if fastPolicyModelMatches(rule.ModelWhitelist, model) {
		return rule.Action, rule.ErrorMessage
	}
	return rule.FallbackAction, rule.FallbackErrorMessage
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
