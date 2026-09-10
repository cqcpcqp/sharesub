package billing

import (
	"encoding/json"
	"math"
	"sort"
	"strings"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func EmbeddedPricingConfig() domain.PricingConfig {
	var catalog map[string]modelPricing
	if err := json.Unmarshal(modelPricingJSON, &catalog); err != nil {
		panic(err)
	}
	config := domain.PricingConfig{Models: []domain.ModelPrice{}, FastMultiplierBPS: 20_000, FlexMultiplierBPS: 5_000, WebSearchMicros: 10_000, Image1KMicros: 134_000, Image2KMicros: 201_000, Image4KMicros: 268_000}
	for model, pricing := range catalog {
		if !strings.HasPrefix(model, "gpt-") && model != "codex-auto-review" {
			continue
		}
		convert := func(priority bool, factor float64) domain.TokenPrices {
			input, output, read, write := pricing.InputPrice, pricing.OutputPrice, pricing.CacheReadPrice, pricing.CacheCreationPrice
			if priority {
				if hasPriorityPricing(pricing) {
					if pricing.InputPricePriority > 0 {
						input = pricing.InputPricePriority
					}
					if pricing.OutputPricePriority > 0 {
						output = pricing.OutputPricePriority
					}
					if pricing.CacheReadPricePriority > 0 {
						read = pricing.CacheReadPricePriority
					}
					if pricing.CacheCreationPriority > 0 {
						write = pricing.CacheCreationPriority
					}
				} else {
					factor = 2
				}
			}
			imageInput, imageOutput := pricing.ImageInputPrice, pricing.ImageOutputPrice
			if imageInput == 0 {
				imageInput = input
			}
			if imageOutput == 0 {
				imageOutput = output
			}
			micros := func(value float64) int64 { return int64(math.Round(value * factor * 1e12)) }
			return domain.TokenPrices{Input: micros(input), Output: micros(output), CacheRead: micros(read), CacheWrite: micros(write), ImageInput: micros(imageInput), ImageOutput: micros(imageOutput)}
		}
		config.Models = append(config.Models, domain.ModelPrice{
			Model: model, Standard: convert(false, 1),
			LongContextTokens: pricing.LongContextThreshold,
			LongInputBPS:      int64(math.Round(positiveOrOne(pricing.LongContextInputFactor) * 10_000)),
			LongOutputBPS:     int64(math.Round(positiveOrOne(pricing.LongContextOutputFactor) * 10_000)),
		})
	}
	sort.Slice(config.Models, func(left, right int) bool { return config.Models[left].Model < config.Models[right].Model })
	return config
}
