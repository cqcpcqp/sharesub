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
	if got, want := AccountCostMicros("gpt-5.3-codex", "priority", usage), int64(16_870_000); got != want {
		t.Fatalf("priority account cost = %d, want %d", got, want)
	}
}

func TestAccountCostMicrosAppliesGPT54LongContextPricing(t *testing.T) {
	usage := domain.TokenUsage{InputTokens: 300_000, OutputTokens: 100_000}
	if got, want := AccountCostMicros("gpt-5.4", "", usage), int64(3_750_000); got != want {
		t.Fatalf("long-context account cost = %d, want %d", got, want)
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
