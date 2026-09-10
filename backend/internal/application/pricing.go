package application

import (
	"context"
	"strconv"
	"strings"

	"github.com/sharesub/sharesub/backend/internal/billing"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Service) CurrentPricing(ctx context.Context) (domain.PricingVersion, error) {
	id, err := s.store.CurrentPricingID(ctx)
	if err != nil {
		return domain.PricingVersion{}, err
	}
	if cached, ok := s.pricingCache.Load(id); ok {
		return cached.(domain.PricingVersion), nil
	}
	version, err := s.store.PricingVersion(ctx, id)
	if err != nil {
		return domain.PricingVersion{}, err
	}
	if err := billing.ValidateConfig(version.Config); err != nil {
		return domain.PricingVersion{}, err
	}
	s.pricingCache.Store(id, version)
	return version, nil
}

func (s *Service) PricingVersion(ctx context.Context, id int64) (domain.PricingVersion, error) {
	if id <= 0 {
		return domain.PricingVersion{}, domain.ErrInvalidInput
	}
	return s.store.PricingVersion(ctx, id)
}

func (s *Service) PricingHistory(ctx context.Context, before int64) ([]domain.PricingVersionSummary, error) {
	if before < 0 {
		return nil, domain.ErrInvalidInput
	}
	return s.store.PricingHistory(ctx, before)
}

func (s *Service) PublishPricing(ctx context.Context, actor domain.User, input domain.PublishPricingInput) (domain.PricingVersion, error) {
	if !actor.IsAdmin {
		return domain.PricingVersion{}, domain.ErrForbidden
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.BaseVersionID <= 0 || input.Reason == "" || len([]rune(input.Reason)) > 500 {
		return domain.PricingVersion{}, domain.ErrInvalidInput
	}
	if err := billing.ValidateConfig(input.Config); err != nil {
		return domain.PricingVersion{}, err
	}
	base, err := s.store.PricingVersion(ctx, input.BaseVersionID)
	if err != nil {
		return domain.PricingVersion{}, err
	}
	if len(base.Config.Models) != len(input.Config.Models) {
		return domain.PricingVersion{}, domain.ErrInvalidInput
	}
	models := make(map[string]bool, len(base.Config.Models))
	for _, model := range base.Config.Models {
		models[model.Model] = true
	}
	for _, model := range input.Config.Models {
		if !models[model.Model] {
			return domain.PricingVersion{}, domain.ErrInvalidInput
		}
	}
	event, err := s.newAuditEvent(actor.ID, "pricing.published", "pricing", strconv.FormatInt(input.BaseVersionID, 10), map[string]any{"base_version_id": input.BaseVersionID, "reason": input.Reason})
	if err != nil {
		return domain.PricingVersion{}, err
	}
	return s.store.PublishPricing(ctx, input, event)
}
