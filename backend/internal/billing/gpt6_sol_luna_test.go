package billing

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/migrations"
)

func TestGPT6SolLunaStandardPricesAndLongContext(t *testing.T) {
	config := EmbeddedPricingConfig()
	for _, model := range []struct {
		name                  string
		short, boundary, long int64
	}{
		{"gpt-6-sol", 269_000, 613_000, 1_176_004},
		{"gpt-6-luna", 13_450, 30_650, 58_800},
	} {
		for _, test := range []struct {
			name        string
			input, want int64
		}{
			{"short", 100_000, model.short},
			{"boundary", 272_000, model.boundary},
			{"long", 272_001, model.long},
		} {
			t.Run(model.name+"/"+test.name, func(t *testing.T) {
				usage := domain.TokenUsage{InputTokens: test.input, CachedTokens: 20_000, CacheCreationTokens: 10_000, OutputTokens: 10_000}
				// Standard short input = total - cached reads - cache writes.
				cost, err := VersionedCost(config, model.name, "", []domain.GatewayBillingSegment{{TokenUsage: usage}})
				if err != nil || cost.TotalMicros != test.want {
					t.Fatalf("cost=%+v err=%v want=%d", cost, err, test.want)
				}
				if got := AccountCostMicros(model.name, "", usage); got != test.want {
					t.Fatalf("embedded cost=%d want=%d", got, test.want)
				}
			})
		}
		for _, alias := range []string{"openai/" + model.name, model.name + "-2026-09-22"} {
			price, ok := ConfigModel(config, alias)
			if !ok || price.Model != model.name {
				t.Fatalf("alias %s resolved to %+v", alias, price)
			}
		}
	}
	for _, name := range []string{"gpt-6-other", "gpt-6-solar", "gpt-6-lunar"} {
		if _, ok := ConfigModel(config, name); ok {
			t.Fatalf("unrelated model %s acquired a price", name)
		}
	}
}

func TestGPT6PricingMigrationMatchesCatalog(t *testing.T) {
	body, err := migrations.Files.ReadFile("038_gpt6_sol_luna_pricing.sql")
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
			t.Fatalf("migration price mismatch for %s", model.Model)
		}
	}
}
