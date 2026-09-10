package billing

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/migrations"
)

func TestInitialPricingMigrationMatchesEmbeddedCatalog(t *testing.T) {
	body, err := migrations.Files.ReadFile("033_global_pricing.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(body), "$pricing$")
	if len(parts) != 3 {
		t.Fatal("missing initial config")
	}
	var config domain.PricingConfig
	if err := json.Unmarshal([]byte(parts[1]), &config); err != nil {
		t.Fatal(err)
	}
	config.FastMultiplierBPS = 20_000
	config.FlexMultiplierBPS = 5_000
	if !reflect.DeepEqual(config, EmbeddedPricingConfig()) {
		t.Fatal("migration changed the embedded initial pricing")
	}
	if err := ValidateConfig(config); err != nil {
		t.Fatal(err)
	}
}

func TestVersionedCostMatchesExistingPricing(t *testing.T) {
	config := EmbeddedPricingConfig()
	for _, model := range config.Models {
		if model.Model != "gpt-6-astra" && model.Model != "gpt-5.6-sol" {
			continue
		}
		for _, tier := range []string{"", "standard", "priority", "fast", "flex"} {
			for _, usage := range []domain.TokenUsage{
				{InputTokens: 100_000, CachedTokens: 25_000, CacheCreationTokens: 10_000, OutputTokens: 20_000, ImageInputTokens: 500, ImageOutputTokens: 600},
				{InputTokens: 272_000, CachedTokens: 30_000, OutputTokens: 10_000},
				{InputTokens: 500_000, CachedTokens: 100_000, CacheCreationTokens: 100_000, OutputTokens: 20_000, ImageInputTokens: 500, ImageOutputTokens: 600},
				{ImageCount: 2},
			} {
				for _, size := range []string{"", "1K", "4K"} {
					want := AccountCostForImageSize(model.Model, tier, usage, 2, size)
					got, err := VersionedCost(config, model.Model, tier, []domain.GatewayBillingSegment{{TokenUsage: usage, WebSearchCalls: 2, ImageSize: size}})
					if err != nil || got != want {
						t.Fatalf("%s %s usage=%+v size=%s: got %+v err=%v want %+v", model.Model, tier, usage, size, got, err, want)
					}
				}
			}
		}
	}
}

func TestVersionedCostSegmentsAndRounding(t *testing.T) {
	config := EmbeddedPricingConfig()
	segments := []domain.GatewayBillingSegment{{TokenUsage: domain.TokenUsage{InputTokens: 150_000}}, {TokenUsage: domain.TokenUsage{InputTokens: 150_000}}}
	cost, err := VersionedCost(config, "gpt-6-astra", "", segments)
	if err != nil || cost.TotalMicros != 3_000_000 {
		t.Fatalf("segmented cost=%+v err=%v", cost, err)
	}
	config.Models = []domain.ModelPrice{{Model: "gpt-6-astra", Standard: domain.TokenPrices{Input: 500_000}, LongInputBPS: 10_000, LongOutputBPS: 10_000}}
	cost, err = VersionedCost(config, "gpt-6-astra", "", []domain.GatewayBillingSegment{{TokenUsage: domain.TokenUsage{InputTokens: 1}}})
	if err != nil || cost.TotalMicros != 1 {
		t.Fatalf("half micro must round up: %+v %v", cost, err)
	}
	_, err = VersionedCost(config, "unknown-model", "", segments)
	if err == nil {
		t.Fatal("missing pricing must not produce a zero bill")
	}
	_, err = VersionedCost(config, "gpt-6-astra", "", []domain.GatewayBillingSegment{{TokenUsage: domain.TokenUsage{InputTokens: -1}}})
	if err == nil {
		t.Fatal("negative token count must fail")
	}
}

func TestValidatePricingConfig(t *testing.T) {
	for _, mutate := range []func(*domain.PricingConfig){
		func(config *domain.PricingConfig) { config.Models = nil },
		func(config *domain.PricingConfig) { config.Models[0].Standard.Input = -1 },
		func(config *domain.PricingConfig) { config.Models[0].Standard.Output = 1_000_000_000_001 },
		func(config *domain.PricingConfig) { config.Models = append(config.Models, config.Models[0]) },
		func(config *domain.PricingConfig) { config.Models[0].LongInputBPS = 0 },
		func(config *domain.PricingConfig) { config.WebSearchMicros = -1 },
	} {
		config := EmbeddedPricingConfig()
		mutate(&config)
		if ValidateConfig(config) == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
