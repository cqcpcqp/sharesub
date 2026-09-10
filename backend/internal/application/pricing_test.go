package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/billing"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

type pricingTestStore struct {
	Store
	current   int64
	versions  map[int64]domain.PricingVersion
	loadErr   error
	reads     int
	publishes int
	event     domain.AuditEvent
}

func (s *pricingTestStore) CurrentPricingID(context.Context) (int64, error) {
	return s.current, s.loadErr
}
func (s *pricingTestStore) PricingVersion(_ context.Context, id int64) (domain.PricingVersion, error) {
	s.reads++
	version, ok := s.versions[id]
	if !ok {
		return version, domain.ErrNotFound
	}
	return version, nil
}
func (s *pricingTestStore) PublishPricing(_ context.Context, input domain.PublishPricingInput, event domain.AuditEvent) (domain.PricingVersion, error) {
	if input.BaseVersionID != s.current {
		return domain.PricingVersion{}, domain.ErrConflict
	}
	s.publishes++
	s.event = event
	s.current++
	version := domain.PricingVersion{PricingVersionSummary: domain.PricingVersionSummary{ID: s.current}, Config: input.Config}
	s.versions[s.current] = version
	return version, nil
}

func TestPricingCurrentReadsGlobalPointerAndCachesImmutableVersion(t *testing.T) {
	ctx := context.Background()
	first := domain.PricingVersion{PricingVersionSummary: domain.PricingVersionSummary{ID: 1}, Config: billing.EmbeddedPricingConfig()}
	second := domain.PricingVersion{PricingVersionSummary: domain.PricingVersionSummary{ID: 2}, Config: billing.EmbeddedPricingConfig()}
	second.Config.WebSearchMicros = 20_000
	store := &pricingTestStore{current: 1, versions: map[int64]domain.PricingVersion{1: first, 2: second}}
	service := &Service{store: store}
	pinned, err := service.CurrentPricing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CurrentPricing(ctx); err != nil || store.reads != 1 {
		t.Fatalf("cache reads=%d err=%v", store.reads, err)
	}
	store.current = 2
	updated, err := service.CurrentPricing(ctx)
	if err != nil || updated.ID != 2 || updated.Config.WebSearchMicros != 20_000 {
		t.Fatalf("did not see new version: %+v %v", updated, err)
	}
	if pinned.ID != 1 || pinned.Config.WebSearchMicros != 10_000 {
		t.Fatal("in-flight snapshot changed")
	}
	store.loadErr = errors.New("database offline")
	if _, err := service.CurrentPricing(ctx); err == nil {
		t.Fatal("must not use cached old pricing when pointer lookup fails")
	}
}

func TestPublishPricingAuthorizationValidationConflict(t *testing.T) {
	ctx := context.Background()
	config := billing.EmbeddedPricingConfig()
	store := &pricingTestStore{current: 1, versions: map[int64]domain.PricingVersion{1: {PricingVersionSummary: domain.PricingVersionSummary{ID: 1}, Config: config}}}
	service := &Service{store: store, now: time.Now}
	input := domain.PublishPricingInput{BaseVersionID: 1, Reason: "更新模型价格", Config: billing.EmbeddedPricingConfig()}
	if _, err := service.PublishPricing(ctx, domain.User{ID: "owner"}, input); !errors.Is(err, domain.ErrForbidden) || store.publishes != 0 {
		t.Fatalf("non-admin publish: %v", err)
	}
	admin := domain.User{ID: "admin", IsAdmin: true}
	bad := input
	bad.Reason = "  "
	if _, err := service.PublishPricing(ctx, admin, bad); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("empty reason: %v", err)
	}
	bad = input
	bad.Config = billing.EmbeddedPricingConfig()
	bad.Config.Models[0].Model = "new-model"
	if _, err := service.PublishPricing(ctx, admin, bad); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("model identity changed: %v", err)
	}
	input.Config.WebSearchMicros = 30_000
	version, err := service.PublishPricing(ctx, admin, input)
	if err != nil || version.ID != 2 || store.event.ActorUserID != "admin" {
		t.Fatalf("publish: %+v %v", version, err)
	}
	if _, err := service.PublishPricing(ctx, admin, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale publish: %v", err)
	}
}

func TestRecordGatewayMetricRequiresPinnedPricing(t *testing.T) {
	service := &Service{store: &gatewayStore{}}
	if err := service.RecordGatewayMetric(context.Background(), GatewayAccess{}, domain.GatewayMetric{}, time.Now()); err == nil {
		t.Fatal("unversioned new metric accepted")
	}
}
