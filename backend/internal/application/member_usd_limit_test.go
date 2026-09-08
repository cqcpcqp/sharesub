package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestMemberUSDLimitValidationAndAdminScope(t *testing.T) {
	store := &adminStore{plans: []domain.AdminPlan{{Plan: domain.Plan{ID: "plan", OwnerUserID: "owner"}}}}
	service := NewService(store, nil, nil, 0, "", "")
	admin := service.decorateUser(domain.User{ID: "admin", Role: domain.RoleAdmin})
	for _, limit := range []int64{0, -1, 9_007_199_254_740_992} {
		if _, err := service.UpdateMemberShare(context.Background(), "owner", "plan", "member", 3300, &limit); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("owner accepted invalid limit %d: %v", limit, err)
		}
		if _, err := service.AdminUpdateMemberShare(context.Background(), admin, "plan", "member", 3300, &limit); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("admin accepted invalid limit %d: %v", limit, err)
		}
	}
	limit := int64(800_000_000)
	member, err := service.AdminUpdateMemberShare(context.Background(), admin, "plan", "member", 3300, &limit)
	if err != nil || member.USDLimitMicros == nil || *member.USDLimitMicros != limit || store.planOwnerID != "owner" || store.planEvent.ActorUserID != "admin" {
		t.Fatalf("admin limit update = %+v, %v", member, err)
	}
	member, err = service.UpdateMemberShare(context.Background(), "owner", "plan", "member", 3300, nil)
	if err != nil || member.USDLimitMicros != nil || member.ShareBasisPoints != 3300 {
		t.Fatalf("unlimited must preserve share = %+v, %v", member, err)
	}
	if _, err := service.AdminUpdateMemberShare(context.Background(), domain.User{ID: "member", Role: domain.RoleUser}, "plan", "member", 3300, &limit); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("non-admin update = %v", err)
	}
}

func TestGatewayUSDLimitKeepsExistingQuotaChecks(t *testing.T) {
	limit := int64(600_000_000)
	for _, test := range []struct {
		name             string
		mode             string
		share            int
		limit            *int64
		memberExhausted  bool
		accountExhausted bool
		want             bool
		checks           int
	}{
		{"fixed unlimited keeps percentage", domain.AllocationFixed, 2500, nil, true, false, true, 1},
		{"fixed USD cap", domain.AllocationFixed, 2500, &limit, true, false, true, 1},
		{"fixed below cap", domain.AllocationFixed, 2500, &limit, false, false, false, 1},
		{"zero share stays view only", domain.AllocationFixed, 0, nil, false, false, true, 0},
		{"shared defaults unlimited", domain.AllocationShared, 0, nil, true, false, false, 0},
		{"shared explicit USD cap", domain.AllocationShared, 0, &limit, true, false, true, 1},
		{"account cap retained", domain.AllocationFixed, 2500, nil, false, true, true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &gatewayStore{exhausted: map[string]bool{"member": test.memberExhausted}, accountExhausted: map[string]bool{"account": test.accountExhausted}}
			service := &Service{store: store, now: time.Now}
			got, err := service.gatewayCredentialQuotaExhausted(context.Background(), domain.GatewayCredential{
				Plan:    domain.Plan{ID: "plan", AllocationMode: test.mode},
				Member:  domain.Member{ID: "member", ShareBasisPoints: test.share, USDLimitMicros: test.limit},
				Account: domain.Account{ID: "account"},
			})
			if err != nil || got != test.want || len(store.memberChecks) != test.checks {
				t.Fatalf("exhausted = %v, checks = %v, error = %v", got, store.memberChecks, err)
			}
		})
	}
}
