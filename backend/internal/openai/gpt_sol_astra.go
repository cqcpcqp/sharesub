package openai

import (
	"fmt"
	"strings"
)

func validateGPTSolAstraReasoning(payload map[string]any, model string) error {
	canonical := strings.ToLower(strings.TrimSpace(model))
	if index := strings.LastIndex(canonical, "/"); index >= 0 {
		canonical = canonical[index+1:]
	}
	canonical = strings.ReplaceAll(canonical, "_", "-")
	switch canonical {
	case "gpt-6.1-sol", "gpt-6.2-sol", "gpt-6.2-astra":
	default:
		return nil
	}
	if reasoning, ok := payload["reasoning"].(map[string]any); ok {
		if effort, ok := reasoning["effort"].(string); ok {
			switch strings.ToLower(strings.TrimSpace(effort)) {
			case "none", "minimal":
				return fmt.Errorf("%s does not support reasoning effort %q; use low, medium, high, xhigh or max", canonical, effort)
			}
		}
	}
	return nil
}
