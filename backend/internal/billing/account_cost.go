package billing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/sharesub/sharesub/backend/internal/domain"
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
	CacheCreationPrice     float64 `json:"cache_creation_input_token_cost"`
	CacheCreationPriority  float64 `json:"cache_creation_input_token_cost_priority"`
	CacheReadPrice         float64 `json:"cache_read_input_token_cost"`
	CacheReadPricePriority float64 `json:"cache_read_input_token_cost_priority"`
	ImageInputPrice        float64 `json:"input_cost_per_image_token"`
	ImageOutputPrice       float64 `json:"output_cost_per_image_token"`
}

var (
	pricingOnce sync.Once
	prices      map[string]modelPricing
)

func AccountCostMicros(model, serviceTier string, usage domain.TokenUsage) int64 {
	return AccountCost(model, serviceTier, usage, 0).TotalMicros
}

func AccountCost(model, serviceTier string, usage domain.TokenUsage, webSearchCalls int64) domain.CostBreakdown {
	return AccountCostForImageSize(model, serviceTier, usage, webSearchCalls, "")
}

// AccountCostForImageSize applies sub2api's image-generation size tiers. An
// unspecified or unrecognized size is billed as 2K.
func AccountCostForImageSize(model, serviceTier string, usage domain.TokenUsage, webSearchCalls int64, imageSize string) domain.CostBreakdown {
	// Responses API web search is an add-on to the response's token usage.
	// Image generation keeps sub2api's default per-generated-image mode, which
	// replaces token billing for the generated response.
	out := domain.CostBreakdown{WebSearchMicros: webSearchCalls * 10_000}
	if usage.ImageCount > 0 {
		unitMicros := int64(201_000)
		switch imageBillingTier(imageSize) {
		case "1K":
			unitMicros = 134_000
		case "4K":
			unitMicros = 268_000
		}
		out.ImageOutputMicros = usage.ImageCount * unitMicros
		out.TotalMicros = out.ImageOutputMicros + out.WebSearchMicros
		return out
	}

	pricing, ok := pricingForModel(model)
	if !ok {
		out.TotalMicros = out.WebSearchMicros
		return out
	}

	inputPrice := pricing.InputPrice
	outputPrice := pricing.OutputPrice
	cacheCreationPrice := pricing.CacheCreationPrice
	cacheReadPrice := pricing.CacheReadPrice
	imageInputPrice := pricing.ImageInputPrice
	imageOutputPrice := pricing.ImageOutputPrice
	tierMultiplier := 1.0
	switch strings.ToLower(strings.TrimSpace(serviceTier)) {
	case "priority", "fast":
		if hasPriorityPricing(pricing) {
			if pricing.InputPricePriority > 0 {
				inputPrice = pricing.InputPricePriority
			}
			if pricing.OutputPricePriority > 0 {
				outputPrice = pricing.OutputPricePriority
			}
			if pricing.CacheReadPricePriority > 0 {
				cacheReadPrice = pricing.CacheReadPricePriority
			}
			if pricing.CacheCreationPriority > 0 {
				cacheCreationPrice = pricing.CacheCreationPriority
			}
		} else {
			tierMultiplier = 2.0
		}
	case "flex":
		tierMultiplier = 0.5
	}
	if imageInputPrice == 0 {
		imageInputPrice = inputPrice
	}
	if imageOutputPrice == 0 {
		imageOutputPrice = outputPrice
	}

	textInput := usage.InputTokens - usage.CachedTokens - usage.CacheCreationTokens - usage.ImageInputTokens
	if textInput < 0 {
		textInput = 0
	}
	textOutput := usage.OutputTokens - usage.ImageOutputTokens
	if textOutput < 0 {
		textOutput = 0
	}
	toMicros := func(tokens int64, price float64) int64 {
		return int64(math.Round(float64(tokens) * price * tierMultiplier * 1_000_000))
	}
	out.InputMicros = toMicros(textInput, inputPrice)
	out.OutputMicros = toMicros(textOutput, outputPrice)
	out.CacheCreationMicros = toMicros(usage.CacheCreationTokens, cacheCreationPrice)
	out.CacheReadMicros = toMicros(usage.CachedTokens, cacheReadPrice)
	out.ImageInputMicros = toMicros(usage.ImageInputTokens, imageInputPrice)
	out.ImageOutputMicros = toMicros(usage.ImageOutputTokens, imageOutputPrice)
	out.TotalMicros = out.InputMicros + out.OutputMicros + out.CacheCreationMicros + out.CacheReadMicros + out.ImageInputMicros + out.ImageOutputMicros + out.WebSearchMicros
	return out
}

func hasPriorityPricing(pricing modelPricing) bool {
	return pricing.InputPricePriority > 0 || pricing.OutputPricePriority > 0 ||
		pricing.CacheCreationPriority > 0 || pricing.CacheReadPricePriority > 0
}

func imageBillingTier(size string) string {
	normalized := strings.ToLower(strings.TrimSpace(size))
	switch normalized {
	case "1k":
		return "1K"
	case "2k", "2048x2048", "2048x1152":
		return "2K"
	case "4k", "3840x2160", "2160x3840":
		return "4K"
	}
	var width, height int
	if _, err := fmt.Sscanf(normalized, "%dx%d", &width, &height); err != nil || width <= 0 || height <= 0 {
		return "2K"
	}
	maxEdge := width
	if height > maxEdge {
		maxEdge = height
	}
	switch {
	case maxEdge <= 1024:
		return "1K"
	case maxEdge <= 2048:
		return "2K"
	default:
		return "4K"
	}
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
	case strings.Contains(model, "gpt-5.6-terra"):
		return "gpt-5.6-terra"
	case strings.Contains(model, "gpt-5.6-luna"):
		return "gpt-5.6-luna"
	case strings.Contains(model, "gpt-5.6"):
		return "gpt-5.6-sol"
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
