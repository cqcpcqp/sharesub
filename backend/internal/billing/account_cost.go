package billing

import (
	_ "embed"
	"encoding/json"
	"math"
	"strings"
	"sync"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

const (
	gpt54LongContextThreshold = int64(272_000)
	gpt54InputMultiplier      = 2.0
	gpt54OutputMultiplier     = 1.5
)

// The pricing snapshot is shared with sub2api and follows its LiteLLM-based
// account-cost calculation instead of assuming every request uses one model.
//
//go:embed model_prices_and_context_window.json
var modelPricingJSON []byte

type modelPricing struct {
	InputPrice             float64 `json:"input_cost_per_token"`
	InputPricePriority     float64 `json:"input_cost_per_token_priority"`
	OutputPrice            float64 `json:"output_cost_per_token"`
	OutputPricePriority    float64 `json:"output_cost_per_token_priority"`
	CacheReadPrice         float64 `json:"cache_read_input_token_cost"`
	CacheReadPricePriority float64 `json:"cache_read_input_token_cost_priority"`
}

var (
	pricingOnce sync.Once
	prices      map[string]modelPricing
)

func AccountCostMicros(model, serviceTier string, usage domain.TokenUsage) int64 {
	pricing, ok := pricingForModel(model)
	if !ok {
		return 0
	}

	inputPrice := pricing.InputPrice
	outputPrice := pricing.OutputPrice
	cacheReadPrice := pricing.CacheReadPrice
	tierMultiplier := 1.0
	switch strings.ToLower(strings.TrimSpace(serviceTier)) {
	case "priority":
		if pricing.InputPricePriority > 0 {
			inputPrice = pricing.InputPricePriority
		}
		if pricing.OutputPricePriority > 0 {
			outputPrice = pricing.OutputPricePriority
		}
		if pricing.CacheReadPricePriority > 0 {
			cacheReadPrice = pricing.CacheReadPricePriority
		}
	case "flex":
		tierMultiplier = 0.5
	}

	if isGPT54Family(model) && usage.InputTokens > gpt54LongContextThreshold {
		inputPrice *= gpt54InputMultiplier
		cacheReadPrice *= gpt54InputMultiplier
		outputPrice *= gpt54OutputMultiplier
	}

	nonCachedInput := usage.InputTokens - usage.CachedTokens
	if nonCachedInput < 0 {
		nonCachedInput = 0
	}
	costUSD := (float64(nonCachedInput)*inputPrice +
		float64(usage.CachedTokens)*cacheReadPrice +
		float64(usage.OutputTokens)*outputPrice) * tierMultiplier
	return int64(math.Round(costUSD * 1_000_000))
}

func pricingForModel(model string) (modelPricing, bool) {
	pricingOnce.Do(func() {
		if err := json.Unmarshal(modelPricingJSON, &prices); err != nil {
			panic("parse embedded model pricing: " + err.Error())
		}
	})
	for _, candidate := range modelCandidates(model) {
		if pricing, ok := prices[candidate]; ok {
			return pricing, true
		}
	}
	return modelPricing{}, false
}

func modelCandidates(model string) []string {
	normalized := canonicalModel(model)
	if normalized == "" {
		return nil
	}
	candidates := []string{normalized}
	if family := knownCodexFamily(normalized); family != "" && family != normalized {
		candidates = append(candidates, family)
	}
	return candidates
}

func canonicalModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if index := strings.LastIndex(model, "/"); index >= 0 {
		model = model[index+1:]
	}
	model = strings.ReplaceAll(model, "_", "-")
	model = strings.Join(strings.Fields(model), "-")
	for strings.Contains(model, "--") {
		model = strings.ReplaceAll(model, "--", "-")
	}
	if strings.HasPrefix(model, "gpt5") {
		model = "gpt-5" + strings.TrimPrefix(model, "gpt5")
	}
	replacements := []struct{ from, to string }{
		{"gpt-5.4mini", "gpt-5.4-mini"},
		{"gpt-5.4nano", "gpt-5.4-nano"},
		{"gpt-5.3-codexspark", "gpt-5.3-codex-spark"},
		{"gpt-5.3codexspark", "gpt-5.3-codex-spark"},
		{"gpt-5.3codex", "gpt-5.3-codex"},
	}
	for _, replacement := range replacements {
		model = strings.ReplaceAll(model, replacement.from, replacement.to)
	}
	return model
}

func knownCodexFamily(model string) string {
	switch {
	case strings.Contains(model, "gpt-5.5"):
		return "gpt-5.5"
	case strings.Contains(model, "gpt-5.4-mini"):
		return "gpt-5.4-mini"
	case strings.Contains(model, "gpt-5.4-nano"):
		return "gpt-5.4-nano"
	case strings.Contains(model, "gpt-5.4"):
		return "gpt-5.4"
	case strings.Contains(model, "gpt-5.3-codex-spark"):
		return "gpt-5.3-codex-spark"
	case strings.Contains(model, "gpt-5.3-codex"):
		return "gpt-5.3-codex"
	case strings.Contains(model, "gpt-5.3"):
		return "gpt-5.3-codex"
	case strings.Contains(model, "gpt-5.2-codex"):
		return "gpt-5.2-codex"
	case strings.Contains(model, "gpt-5.2"):
		return "gpt-5.2"
	case strings.Contains(model, "gpt-5.1-codex-mini"):
		return "gpt-5.1-codex-mini"
	case strings.Contains(model, "gpt-5.1-codex-max"):
		return "gpt-5.1-codex-max"
	case strings.Contains(model, "gpt-5.1-codex"):
		return "gpt-5.1-codex"
	case strings.Contains(model, "gpt-5-codex"):
		return "gpt-5-codex"
	case strings.Contains(model, "codex"):
		return "gpt-5.3-codex"
	case strings.Contains(model, "gpt-5"):
		return "gpt-5.4"
	default:
		return ""
	}
}

func isGPT54Family(model string) bool {
	family := knownCodexFamily(canonicalModel(model))
	return family == "gpt-5.4" || family == "gpt-5.5"
}
