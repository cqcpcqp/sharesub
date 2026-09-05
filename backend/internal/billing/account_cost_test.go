package billing

import (
	"testing"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestAccountCostMicrosUsesActualModelPricing(t *testing.T) {
	usage := domain.TokenUsage{InputTokens: 1_000_000, CachedTokens: 200_000, OutputTokens: 500_000}
	if got, want := AccountCostMicros("gpt-5.3-codex", "", usage), int64(8_435_000); got != want {
		t.Fatalf("account cost = %d, want %d", got, want)
	}
}

func TestAccountCostMicrosUsesPriorityPricing(t *testing.T) {
	usage := domain.TokenUsage{InputTokens: 1_000_000, CachedTokens: 200_000, OutputTokens: 500_000}
	for _, tier := range []string{"priority", "fast"} {
		if got, want := AccountCostMicros("gpt-5.3-codex", tier, usage), int64(16_870_000); got != want {
			t.Fatalf("%s account cost = %d, want %d", tier, got, want)
		}
	}
}

func TestAccountCostMicrosPriorityUsesDoubleMultiplierWithoutPriorityPricing(t *testing.T) {
	usage := domain.TokenUsage{InputTokens: 100_000, CachedTokens: 20_000, OutputTokens: 50_000}
	standard := AccountCostMicros("claude-4-sonnet-20250514", "", usage)
	if standard == 0 {
		t.Fatal("standard account cost must be non-zero")
	}
	if got, want := AccountCostMicros("claude-4-sonnet-20250514", "priority", usage), standard*2; got != want {
		t.Fatalf("priority account cost without priority pricing = %d, want %d", got, want)
	}
}

func TestAccountCostMicrosMatchesSub2APIDefaultLongContextPolicy(t *testing.T) {
	usage := domain.TokenUsage{InputTokens: 300_000, OutputTokens: 100_000}
	if got, want := AccountCostMicros("gpt-5.4", "", usage), int64(2_250_000); got != want {
		t.Fatalf("default account cost = %d, want %d", got, want)
	}
}

func TestAccountCostMicrosUsesGPT56ModelPricing(t *testing.T) {
	usage := domain.TokenUsage{
		InputTokens: 1_000_000, CachedTokens: 200_000, CacheCreationTokens: 100_000, OutputTokens: 500_000,
	}
	tests := []struct {
		model string
		want  int64
	}{
		{model: "gpt-5.6-sol", want: 30_950_000},
		{model: "gpt-5.6-terra", want: 12_380_000},
		{model: "gpt-5.6-luna", want: 1_238_000},
	}
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			if got := AccountCostMicros(test.model, "", usage); got != test.want {
				t.Fatalf("account cost = %d, want %d", got, test.want)
			}
		})
	}
}

func TestAccountCostMicrosUsesGPT56PriorityCacheCreationPricing(t *testing.T) {
	usage := domain.TokenUsage{
		InputTokens: 1_000_000, CachedTokens: 200_000, CacheCreationTokens: 100_000, OutputTokens: 500_000,
	}
	if got, want := AccountCostMicros("gpt-5.6-sol", "priority", usage), int64(61_900_000); got != want {
		t.Fatalf("priority account cost = %d, want %d", got, want)
	}
}

func TestAccountCostMicrosUsesGPT56LongContextPricing(t *testing.T) {
	usage := domain.TokenUsage{InputTokens: 273_000, CacheCreationTokens: 100_000, CachedTokens: 73_000, OutputTokens: 10}
	if got, want := AccountCostMicros("gpt-5.6-sol", "", usage), int64(2_323_450); got != want {
		t.Fatalf("long-context account cost = %d, want %d", got, want)
	}
}

func TestAccountCostMicrosGPT56LongContextBoundaryIsExclusive(t *testing.T) {
	usage := domain.TokenUsage{InputTokens: 272_000, CacheCreationTokens: 50_000, CachedTokens: 22_000, OutputTokens: 10}
	if got, want := AccountCostMicros("gpt-5.6-sol", "", usage), int64(1_323_800); got != want {
		t.Fatalf("boundary account cost = %d, want %d", got, want)
	}
}

func TestAccountCostMicrosUsesGPT6AstraPricing(t *testing.T) {
	usage := domain.TokenUsage{
		InputTokens: 1_000_000, CachedTokens: 200_000, CacheCreationTokens: 100_000, OutputTokens: 500_000,
	}
	if got, want := AccountCostMicros("gpt-6-astra", "", usage), int64(54_400_000); got != want {
		t.Fatalf("standard account cost = %d, want %d", got, want)
	}
	for _, tier := range []string{"priority", "fast"} {
		if got, want := AccountCostMicros("gpt-6-astra", tier, usage), int64(108_800_000); got != want {
			t.Fatalf("%s account cost = %d, want %d", tier, got, want)
		}
	}
	if got, want := AccountCostMicros("gpt-6-astra", "flex", usage), int64(27_200_000); got != want {
		t.Fatalf("flex account cost = %d, want %d", got, want)
	}
}

func TestAccountCostMicrosGPT6AstraLongContextBoundaryIsExclusive(t *testing.T) {
	boundary := domain.TokenUsage{InputTokens: 272_000, CacheCreationTokens: 50_000, CachedTokens: 22_000, OutputTokens: 10}
	if got, want := AccountCostMicros("gpt-6-astra", "", boundary), int64(2_647_500); got != want {
		t.Fatalf("boundary account cost = %d, want %d", got, want)
	}
	above := domain.TokenUsage{InputTokens: 273_000, CacheCreationTokens: 100_000, CachedTokens: 73_000, OutputTokens: 10}
	if got, want := AccountCostMicros("gpt-6-astra", "", above), int64(4_646_750); got != want {
		t.Fatalf("long-context account cost = %d, want %d", got, want)
	}
}

func TestAccountCostForSegmentsAppliesLongContextPricingPerResponse(t *testing.T) {
	segments := []domain.GatewayBillingSegment{
		{TokenUsage: domain.TokenUsage{InputTokens: 150_000}},
		{TokenUsage: domain.TokenUsage{InputTokens: 150_000}},
	}
	if got, want := AccountCostForSegments("gpt-6-astra", "", segments).TotalMicros, int64(3_000_000); got != want {
		t.Fatalf("segmented account cost = %d, want %d", got, want)
	}
	if got := AccountCostMicros("gpt-6-astra", "", domain.TokenUsage{InputTokens: 300_000}); got == 3_000_000 {
		t.Fatalf("aggregate usage unexpectedly matched segmented long-context cost: %d", got)
	}
}

func TestAccountCostMicrosNormalizesGPT56AliasesToSol(t *testing.T) {
	usage := domain.TokenUsage{InputTokens: 100_000}
	for _, model := range []string{"gpt-5.6", "openai/gpt-5.6", "gpt-5.6-codex"} {
		if got, want := AccountCostMicros(model, "", usage), int64(500_000); got != want {
			t.Fatalf("account cost for %q = %d, want %d", model, got, want)
		}
	}
}

func TestAccountCostMicrosNormalizesCodexAliases(t *testing.T) {
	usage := domain.TokenUsage{InputTokens: 100_000}
	if got, want := AccountCostMicros("openai/gpt5.4", "", usage), int64(250_000); got != want {
		t.Fatalf("aliased account cost = %d, want %d", got, want)
	}
}

func TestAccountCostMicrosUsesFlexTierMultiplier(t *testing.T) {
	usage := domain.TokenUsage{InputTokens: 100_000}
	if got, want := AccountCostMicros("gpt-5.3-codex", "flex", usage), int64(87_500); got != want {
		t.Fatalf("flex account cost = %d, want %d", got, want)
	}
}

func TestAccountCostAddsResponsesWebSearchToTokenBilling(t *testing.T) {
	usage := domain.TokenUsage{InputTokens: 100_000, OutputTokens: 50_000}
	got := AccountCost("gpt-5.3-codex", "", usage, 2)
	if got.InputMicros != 175_000 || got.OutputMicros != 700_000 || got.WebSearchMicros != 20_000 || got.TotalMicros != 895_000 {
		t.Fatalf("web search cost = %+v", got)
	}
}

func TestAccountCostUsesSub2APIDefaultImagePerRequestBilling(t *testing.T) {
	usage := domain.TokenUsage{InputTokens: 1_000, OutputTokens: 2_000, ImageOutputTokens: 1_500, ImageCount: 2}
	got := AccountCost("gpt-5.6-luna", "", usage, 0)
	if got.TotalMicros != 402_000 || got.ImageOutputMicros != 402_000 || got.InputMicros != 0 || got.OutputMicros != 0 {
		t.Fatalf("image cost = %+v", got)
	}
}

func TestAccountCostUsesSub2APIImageSizeTiers(t *testing.T) {
	usage := domain.TokenUsage{ImageCount: 2}
	tests := []struct {
		size string
		want int64
	}{
		{size: "1024x1024", want: 268_000},
		{size: "2048x1152", want: 402_000},
		{size: "4K", want: 536_000},
		{size: "", want: 402_000},
	}
	for _, test := range tests {
		got := AccountCostForImageSize("gpt-image-2", "", usage, 0, test.size)
		if got.TotalMicros != test.want || got.ImageOutputMicros != test.want {
			t.Errorf("size %q cost = %+v, want %d", test.size, got, test.want)
		}
	}
}
