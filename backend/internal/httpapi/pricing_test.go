package httpapi

import (
	"context"

	"github.com/sharesub/sharesub/backend/internal/billing"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (*gatewayHandlerStore) CurrentPricingID(context.Context) (int64, error) { return 1, nil }
func (*gatewayHandlerStore) PricingVersion(context.Context, int64) (domain.PricingVersion, error) {
	return domain.PricingVersion{PricingVersionSummary: domain.PricingVersionSummary{ID: 1}, Config: billing.EmbeddedPricingConfig()}, nil
}

func (*responsesWebSocketHTTPStore) CurrentPricingID(context.Context) (int64, error) { return 1, nil }
func (*responsesWebSocketHTTPStore) PricingVersion(context.Context, int64) (domain.PricingVersion, error) {
	config := billing.EmbeddedPricingConfig()
	config.Models = append(config.Models, domain.ModelPrice{Model: "gpt-old", Standard: domain.TokenPrices{Input: 1_000_000, Output: 1_000_000}, LongInputBPS: 10_000, LongOutputBPS: 10_000})
	return domain.PricingVersion{PricingVersionSummary: domain.PricingVersionSummary{ID: 1}, Config: config}, nil
}
