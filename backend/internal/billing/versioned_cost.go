package billing

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func ValidateConfig(config domain.PricingConfig) error {
	if len(config.Models) == 0 || len(config.Models) > 500 {
		return domain.ErrInvalidInput
	}
	seen := make(map[string]bool, len(config.Models))
	for _, model := range config.Models {
		if model.Model == "" || len(model.Model) > 100 || canonicalModel(model.Model) != model.Model || seen[model.Model] {
			return domain.ErrInvalidInput
		}
		seen[model.Model] = true
		if model.LongContextTokens < 0 || model.LongContextTokens > 10_000_000 || model.LongInputBPS < 10_000 || model.LongInputBPS > 1_000_000 || model.LongOutputBPS < 10_000 || model.LongOutputBPS > 1_000_000 {
			return domain.ErrInvalidInput
		}
		for _, tier := range []domain.TokenPrices{model.Standard} {
			for _, value := range []int64{tier.Input, tier.Output, tier.CacheRead, tier.CacheWrite, tier.ImageInput, tier.ImageOutput} {
				if value < 0 || value > 1_000_000_000_000 {
					return domain.ErrInvalidInput
				}
			}
		}
	}
	for _, value := range []int64{config.WebSearchMicros, config.Image1KMicros, config.Image2KMicros, config.Image4KMicros} {
		if value < 0 || value > 1_000_000_000 {
			return domain.ErrInvalidInput
		}
	}
	if config.FastMultiplierBPS < 10_000 || config.FastMultiplierBPS > 1_000_000 || config.FlexMultiplierBPS < 0 || config.FlexMultiplierBPS > 1_000_000 {
		return domain.ErrInvalidInput
	}
	return nil
}

func ConfigModel(config domain.PricingConfig, model string) (domain.ModelPrice, bool) {
	for _, candidate := range modelCandidates(model) {
		for _, price := range config.Models {
			if price.Model == candidate {
				if price.Standard.Input != 0 || price.Standard.Output != 0 || price.Standard.CacheRead != 0 || price.Standard.CacheWrite != 0 {
					return price, true
				}
			}
		}
	}
	return domain.ModelPrice{}, false
}

func VersionedCost(config domain.PricingConfig, model, serviceTier string, segments []domain.GatewayBillingSegment) (domain.CostBreakdown, error) {
	var total domain.CostBreakdown
	for _, segment := range segments {
		cost, err := versionedSegmentCost(config, model, serviceTier, segment)
		if err != nil {
			return domain.CostBreakdown{}, err
		}
		for _, pair := range []struct {
			target *int64
			value  int64
		}{
			{&total.InputMicros, cost.InputMicros}, {&total.OutputMicros, cost.OutputMicros},
			{&total.CacheCreationMicros, cost.CacheCreationMicros}, {&total.CacheReadMicros, cost.CacheReadMicros},
			{&total.ImageInputMicros, cost.ImageInputMicros}, {&total.ImageOutputMicros, cost.ImageOutputMicros},
			{&total.WebSearchMicros, cost.WebSearchMicros}, {&total.TotalMicros, cost.TotalMicros},
		} {
			sum := new(big.Int).Add(big.NewInt(*pair.target), big.NewInt(pair.value))
			if !sum.IsInt64() {
				return domain.CostBreakdown{}, fmt.Errorf("pricing cost overflow")
			}
			*pair.target = sum.Int64()
		}
	}
	return total, nil
}

func versionedSegmentCost(config domain.PricingConfig, model, serviceTier string, segment domain.GatewayBillingSegment) (domain.CostBreakdown, error) {
	usage := segment.TokenUsage
	for _, count := range []int64{usage.InputTokens, usage.OutputTokens, usage.CachedTokens, usage.CacheCreationTokens, usage.ImageInputTokens, usage.ImageOutputTokens, usage.ImageCount, segment.WebSearchCalls} {
		if count < 0 || count > 1_000_000_000_000 {
			return domain.CostBreakdown{}, fmt.Errorf("pricing usage count outside supported range")
		}
	}
	var cost domain.CostBreakdown
	var calculationErr error
	calculate := func(tokens, price, factor, divisor int64) int64 {
		if tokens < 0 {
			calculationErr = fmt.Errorf("negative pricing token count")
			return 0
		}
		base := float64(tokens) * float64(price) / 1_000_000_000_000
		amount := math.Round(base * float64(factor) / float64(divisor) * 1_000_000)
		if amount < 0 || amount > float64(math.MaxInt64) {
			calculationErr = fmt.Errorf("pricing cost overflow")
			return 0
		}
		return int64(amount)
	}
	calculateMicros := func(count, unit int64) int64 {
		amount := float64(count) * float64(unit)
		if amount < 0 || amount > float64(math.MaxInt64) {
			calculationErr = fmt.Errorf("pricing cost overflow")
			return 0
		}
		return int64(math.Round(amount))
	}
	cost.WebSearchMicros = calculateMicros(segment.WebSearchCalls, config.WebSearchMicros)
	if usage.ImageCount > 0 {
		unit := config.Image2KMicros
		switch imageBillingTier(segment.ImageSize) {
		case "1K":
			unit = config.Image1KMicros
		case "4K":
			unit = config.Image4KMicros
		}
		cost.ImageOutputMicros = calculateMicros(usage.ImageCount, unit)
	} else if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.CachedTokens != 0 || usage.CacheCreationTokens != 0 || usage.ImageInputTokens != 0 || usage.ImageOutputTokens != 0 {
		pricing, ok := ConfigModel(config, model)
		if !ok {
			return cost, fmt.Errorf("no configured pricing for model %q", model)
		}
		prices := pricing.Standard
		tierMultiplier := int64(10_000)
		switch strings.ToLower(strings.TrimSpace(serviceTier)) {
		case "priority", "fast":
			tierMultiplier = config.FastMultiplierBPS
		case "flex":
			tierMultiplier = config.FlexMultiplierBPS
		}
		if prices.ImageInput == 0 {
			prices.ImageInput = prices.Input
		}
		if prices.ImageOutput == 0 {
			prices.ImageOutput = prices.Output
		}
		inputFactor, outputFactor := int64(10_000), int64(10_000)
		if pricing.LongContextTokens > 0 && usage.InputTokens > pricing.LongContextTokens {
			inputFactor, outputFactor = pricing.LongInputBPS, pricing.LongOutputBPS
		}
		cost.InputMicros = calculate(max(0, usage.InputTokens-usage.CachedTokens-usage.CacheCreationTokens-usage.ImageInputTokens), prices.Input, inputFactor*tierMultiplier/10_000, 10_000)
		cost.OutputMicros = calculate(max(0, usage.OutputTokens-usage.ImageOutputTokens), prices.Output, outputFactor*tierMultiplier/10_000, 10_000)
		cost.CacheReadMicros = calculate(usage.CachedTokens, prices.CacheRead, inputFactor*tierMultiplier/10_000, 10_000)
		cost.CacheCreationMicros = calculate(usage.CacheCreationTokens, prices.CacheWrite, inputFactor*tierMultiplier/10_000, 10_000)
		cost.ImageInputMicros = calculate(usage.ImageInputTokens, prices.ImageInput, tierMultiplier, 10_000)
		cost.ImageOutputMicros = calculate(usage.ImageOutputTokens, prices.ImageOutput, tierMultiplier, 10_000)
	}
	amount := new(big.Int)
	for _, value := range []int64{cost.InputMicros, cost.OutputMicros, cost.CacheReadMicros, cost.CacheCreationMicros, cost.ImageInputMicros, cost.ImageOutputMicros, cost.WebSearchMicros} {
		amount.Add(amount, big.NewInt(value))
	}
	if !amount.IsInt64() {
		return cost, fmt.Errorf("pricing cost overflow")
	}
	cost.TotalMicros = amount.Int64()
	return cost, calculationErr
}
