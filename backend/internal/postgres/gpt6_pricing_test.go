package postgres

import (
	"context"
	"reflect"
	"testing"

	"github.com/sharesub/sharesub/backend/internal/billing"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/migrations"
)

func TestGPT6PricingMigrationPreservesPublishedPrices(t *testing.T) {
	for _, present := range []int{0, 1, 2} {
		t.Run(string(rune('0'+present))+" existing models", func(t *testing.T) {
			store := membershipTestStore(t)
			ctx := context.Background()
			id, err := store.CurrentPricingID(ctx)
			if err != nil {
				t.Fatal(err)
			}
			version, err := store.PricingVersion(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"gpt-6-sol", "gpt-6-luna"} {
				if _, ok := billing.ConfigModel(version.Config, name); !ok {
					t.Fatalf("fresh install missing %s", name)
				}
			}
			config := version.Config
			config.FastMultiplierBPS = 31_000
			config.FlexMultiplierBPS = 7_000
			config.WebSearchMicros = 12_345
			var kept []domain.ModelPrice
			for _, model := range config.Models {
				if model.Model == "gpt-6-sol" && present < 1 || model.Model == "gpt-6-luna" && present < 2 {
					continue
				}
				model.Standard.Input = 987_654
				kept = append(kept, model)
			}
			config.Models = kept
			// Model an installation with administrator-edited prices before migration.
			if _, err := store.pool.Exec(ctx, `UPDATE pricing_versions SET config=$1 WHERE id=$2`, config, id); err != nil {
				t.Fatal(err)
			}
			body, err := migrations.Files.ReadFile("038_gpt6_sol_luna_pricing.sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.pool.Exec(ctx, string(body)); err != nil {
				t.Fatal(err)
			}
			nextID, err := store.CurrentPricingID(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if present < 2 && nextID <= id || present == 2 && nextID != id {
				t.Fatalf("unexpected version %d -> %d", id, nextID)
			}
			old, err := store.PricingVersion(ctx, id)
			if err != nil || !reflect.DeepEqual(old.Config, config) {
				t.Fatalf("historical config changed: %v", err)
			}
			next, err := store.PricingVersion(ctx, nextID)
			if err != nil {
				t.Fatal(err)
			}
			for _, price := range config.Models {
				got, ok := billing.ConfigModel(next.Config, price.Model)
				if !ok || !reflect.DeepEqual(got, price) {
					t.Fatalf("existing price changed for %s", price.Model)
				}
			}
			for _, name := range []string{"gpt-6-sol", "gpt-6-luna"} {
				got, ok := billing.ConfigModel(next.Config, name)
				want, existed := billing.ConfigModel(config, name)
				if !existed {
					want, _ = billing.ConfigModel(billing.EmbeddedPricingConfig(), name)
				}
				if !ok || !reflect.DeepEqual(got, want) {
					t.Fatalf("incorrect new price for %s", name)
				}
			}
			withoutModels := next.Config
			withoutModels.Models = config.Models
			if !reflect.DeepEqual(withoutModels, config) {
				t.Fatal("migration changed global configuration")
			}
			if err := billing.ValidateConfig(next.Config); err != nil {
				t.Fatal(err)
			}
			if _, err := store.pool.Exec(ctx, string(body)); err != nil {
				t.Fatal(err)
			}
			repeatedID, err := store.CurrentPricingID(ctx)
			if err != nil || repeatedID != nextID {
				t.Fatal("repeated migration created another version")
			}
		})
	}
}
