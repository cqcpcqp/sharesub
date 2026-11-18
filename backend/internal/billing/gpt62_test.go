package billing

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/migrations"
)

func TestGPT62StandardPricesAndLongContext(t *testing.T) {
	config := EmbeddedPricingConfig()
	for _, model := range []struct {
		name                  string
		short, boundary, long int64
	}{
		{"gpt-6.2-sol", 267_000, 611_000, 1_172_004},
		{"gpt-6.2-astra", 1_068_000, 2_444_000, 4_688_016},
	} {
		for _, test := range []struct {
			name        string
			input, want int64
		}{
			{"short", 100_000, model.short}, {"boundary", 272_000, model.boundary}, {"long", 272_001, model.long},
		} {
			t.Run(model.name+"/"+test.name, func(t *testing.T) {
				usage := domain.TokenUsage{InputTokens: test.input, CachedTokens: 20_000, CacheCreationTokens: 10_000, OutputTokens: 10_000}
				cost, err := VersionedCost(config, model.name, "", []domain.GatewayBillingSegment{{TokenUsage: usage}})
				if err != nil || cost.TotalMicros != test.want {
					t.Fatalf("cost=%+v err=%v want=%d", cost, err, test.want)
				}
				if got := AccountCostMicros(model.name, "", usage); got != test.want {
					t.Fatalf("embedded cost=%d want=%d", got, test.want)
				}
			})
		}
		for _, alias := range []string{"openai/" + model.name, model.name + "-max", model.name + "-openai-compact"} {
			price, ok := ConfigModel(config, alias)
			if !ok || price.Model != model.name {
				t.Fatalf("alias %s resolved to %+v", alias, price)
			}
		}
	}
	for _, name := range []string{"gpt-6.2", "gpt-6.2-solitude", "gpt-6.2-sol-preview", "gpt-6.2-astra-preview", "gpt-6.2-astral"} {
		if _, ok := ConfigModel(config, name); ok {
			t.Fatalf("unrelated model %s acquired a price", name)
		}
	}
}

func TestGPT62PricingMigrationMatchesCatalog(t *testing.T) {
	body, err := migrations.Files.ReadFile("041_gpt62_sol_astra_pricing.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(body), "$models$")
	if len(parts) != 3 {
		t.Fatal("missing model prices")
	}
	var models []domain.ModelPrice
	if err := json.Unmarshal([]byte(parts[1]), &models); err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatal("expected two new models")
	}
	for _, model := range models {
		want, ok := ConfigModel(EmbeddedPricingConfig(), model.Model)
		if !ok || !reflect.DeepEqual(model, want) {
			t.Fatalf("migration price mismatch for %s: got %+v want %+v", model.Model, model, want)
		}
	}
}

func TestGPT62ImageInputLongContextPricing(t *testing.T) {
	config := EmbeddedPricingConfig()
	for _, model := range []struct {
		name  string
		scale int64
	}{
		{"gpt-6.2-sol", 1},
		{"gpt-6.2-astra", 4},
	} {
		for _, test := range []struct {
			name                      string
			input, image, totalMicros int64
			imageMicros               int64
		}{
			{"short_image_only", 100_000, 100_000, 200_000, 200_000},
			{"boundary", 272_000, 100_000, 544_000, 200_000},
			{"above_boundary", 272_001, 100_000, 1_088_004, 400_000},
			{"mixed_long", 300_000, 100_000, 1_200_000, 400_000},
		} {
			for _, tier := range []struct {
				name                   string
				numerator, denominator int64
			}{
				{"standard", 1, 1},
				{"priority", 2, 1},
				{"fast", 2, 1},
				{"flex", 1, 2},
			} {
				t.Run(model.name+"/"+test.name+"/"+tier.name, func(t *testing.T) {
					usage := domain.TokenUsage{InputTokens: test.input, ImageInputTokens: test.image}
					// Expected Standard amounts use the published Sol rates directly;
					// Astra costs four times as much. Check each calculator against
					// those amounts rather than only checking calculator parity.
					want := domain.CostBreakdown{
						InputMicros:      (test.totalMicros - test.imageMicros) * model.scale * tier.numerator / tier.denominator,
						ImageInputMicros: test.imageMicros * model.scale * tier.numerator / tier.denominator,
						TotalMicros:      test.totalMicros * model.scale * tier.numerator / tier.denominator,
					}
					got, err := VersionedCost(config, model.name, tier.name, []domain.GatewayBillingSegment{{TokenUsage: usage}})
					if err != nil || got != want {
						t.Errorf("versioned cost=%+v err=%v want=%+v", got, err, want)
					}
					if got := AccountCost(model.name, tier.name, usage, 0); got != want {
						t.Errorf("embedded cost=%+v want=%+v", got, want)
					}
				})
			}
		}
	}
}
