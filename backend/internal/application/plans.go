package application

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

func (s *Service) CreatePlan(ctx context.Context, userID, accountID, name, allocationMode string, ownerShareBPS int) (domain.PlanDetail, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || !validAllocationMode(allocationMode) {
		return domain.PlanDetail{}, domain.ErrInvalidInput
	}
	if allocationMode == domain.AllocationFixed && (ownerShareBPS < 0 || ownerShareBPS > domain.MaxShareBPS) {
		return domain.PlanDetail{}, domain.ErrInvalidInput
	}
	if allocationMode == domain.AllocationShared && ownerShareBPS != 0 {
		return domain.PlanDetail{}, domain.ErrInvalidInput
	}
	if accountID != "" {
		account, err := s.store.AccountByID(ctx, accountID)
		if err != nil {
			return domain.PlanDetail{}, err
		}
		if account.OwnerUserID != userID {
			return domain.PlanDetail{}, domain.ErrForbidden
		}
		if account.Status != domain.StatusActive {
			return domain.PlanDetail{}, domain.ErrAccountUnavailable
		}
	}
	planID, err := security.NewID()
	if err != nil {
		return domain.PlanDetail{}, err
	}
	memberID, err := security.NewID()
	if err != nil {
		return domain.PlanDetail{}, err
	}
	createdAt := s.now()
	plan := domain.Plan{ID: planID, OwnerUserID: userID, AccountID: accountID, Name: name, Status: domain.StatusActive, Visibility: domain.VisibilityPrivate, AllocationMode: allocationMode, CreatedAt: createdAt}
	owner := domain.Member{ID: memberID, PlanID: planID, UserID: userID, Role: domain.RoleOwner, Status: domain.StatusActive, ShareBasisPoints: ownerShareBPS, CreatedAt: createdAt}
	event, err := s.newAuditEvent(userID, "plan.created", "plan", planID, map[string]any{"name": name, "account_id": accountID})
	if err != nil {
		return domain.PlanDetail{}, err
	}
	if err := s.store.CreatePlan(ctx, plan, owner, event); err != nil {
		return domain.PlanDetail{}, err
	}
	return s.PlanDetail(ctx, userID, planID, "UTC")
}

func (s *Service) ListPlans(ctx context.Context, userID string) ([]domain.Plan, error) {
	return s.store.ListPlans(ctx, userID)
}

func (s *Service) PlanDetail(ctx context.Context, userID, planID, timezone string) (domain.PlanDetail, error) {
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return domain.PlanDetail{}, domain.ErrInvalidInput
	}
	now := s.now()
	localNow := now.In(location)
	todayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	detail, err := s.store.PlanDetail(ctx, planID, userID, todayStart, now)
	if err != nil {
		return detail, err
	}
	if detail.Account != nil {
		if err := s.hydrateAccountProxy(detail.Account); err != nil {
			return detail, err
		}
	}
	return detail, nil
}

func (s *Service) planPerformanceWindow(period, timezone string) (time.Time, time.Time, time.Duration, error) {
	type periodConfig struct {
		duration   time.Duration
		bucketSize time.Duration
	}
	periods := map[string]periodConfig{
		"30m": {duration: 30 * time.Minute, bucketSize: time.Minute},
		"6h":  {duration: 6 * time.Hour, bucketSize: 15 * time.Minute},
		"12h": {duration: 12 * time.Hour, bucketSize: 30 * time.Minute},
		"24h": {duration: 24 * time.Hour, bucketSize: time.Hour},
	}
	now := s.now()
	if period == "today" {
		if timezone == "" {
			timezone = "UTC"
		}
		location, err := time.LoadLocation(timezone)
		if err != nil {
			return time.Time{}, time.Time{}, 0, domain.ErrInvalidInput
		}
		localNow := now.In(location)
		windowStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
		duration := now.Sub(windowStart)
		bucketSize := time.Hour
		switch {
		case duration <= 30*time.Minute:
			bucketSize = time.Minute
		case duration <= 6*time.Hour:
			bucketSize = 15 * time.Minute
		case duration <= 12*time.Hour:
			bucketSize = 30 * time.Minute
		}
		return windowStart, now, bucketSize, nil
	}
	config, ok := periods[period]
	if !ok {
		return time.Time{}, time.Time{}, 0, domain.ErrInvalidInput
	}
	return now.Add(-config.duration), now, config.bucketSize, nil
}

func (s *Service) PlanPerformance(ctx context.Context, userID, planID, period, timezone string) (domain.PlanPerformance, error) {
	windowStart, windowEnd, bucketSize, err := s.planPerformanceWindow(period, timezone)
	if err != nil {
		return domain.PlanPerformance{}, err
	}
	return s.store.PlanPerformance(ctx, planID, userID, windowStart, windowEnd, bucketSize)
}

func (s *Service) PlanRequestErrors(ctx context.Context, userID, planID, period, timezone string, page, pageSize int) (domain.PlanRequestErrorList, error) {
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return domain.PlanRequestErrorList{}, domain.ErrInvalidInput
	}
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/pageSize {
		return domain.PlanRequestErrorList{}, domain.ErrInvalidInput
	}
	windowStart, windowEnd, _, err := s.planPerformanceWindow(period, timezone)
	if err != nil {
		return domain.PlanRequestErrorList{}, err
	}
	return s.store.PlanRequestErrors(ctx, planID, userID, windowStart, windowEnd, page, pageSize)
}

func (s *Service) ListPublicPlans(ctx context.Context, userID string) ([]domain.PublicPlan, error) {
	return s.store.ListPublicPlans(ctx, userID)
}

func (s *Service) UpdatePlanPublication(ctx context.Context, ownerID, planID, visibility string, slots, shareBPS int) (domain.Plan, error) {
	if visibility != domain.VisibilityPrivate && visibility != domain.VisibilityPublic {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	if visibility == domain.VisibilityPublic && (slots < 1 || slots > 100 || shareBPS < 0 || shareBPS > domain.MaxShareBPS) {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	event, err := s.newAuditEvent(ownerID, "plan.publication_updated", "plan", planID, map[string]any{"visibility": visibility, "public_slots": slots, "public_share_basis_points": shareBPS})
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.UpdatePlanPublication(ctx, ownerID, planID, visibility, slots, shareBPS, event)
}

func (s *Service) ApplyToPublicPlan(ctx context.Context, userID, planID, message string) (domain.JoinApplication, error) {
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		return domain.JoinApplication{}, domain.ErrInvalidInput
	}
	id, err := security.NewID()
	if err != nil {
		return domain.JoinApplication{}, err
	}
	event, err := s.newAuditEvent(userID, "application.created", "plan", planID, map[string]string{"application_id": id})
	if err != nil {
		return domain.JoinApplication{}, err
	}
	return s.store.CreateJoinApplication(ctx, domain.JoinApplication{ID: id, PlanID: planID, UserID: userID, Message: message, Status: "pending", CreatedAt: s.now()}, event)
}

func (s *Service) ReviewJoinApplication(ctx context.Context, ownerID, applicationID string, approve bool) (domain.JoinApplication, error) {
	memberID, err := security.NewID()
	if err != nil {
		return domain.JoinApplication{}, err
	}
	action := "application.rejected"
	if approve {
		action = "application.approved"
	}
	event, err := s.newAuditEvent(ownerID, action, "join_application", applicationID, nil)
	if err != nil {
		return domain.JoinApplication{}, err
	}
	return s.store.ReviewJoinApplication(ctx, ownerID, applicationID, approve, memberID, s.now(), event)
}

func (s *Service) Invite(ctx context.Context, ownerID, planID string, shareBPS int) (CreatedInvite, error) {
	if shareBPS < 0 || shareBPS > domain.MaxShareBPS {
		return CreatedInvite{}, domain.ErrInvalidInput
	}
	token, err := security.NewOpaqueToken("ss_invite_")
	if err != nil {
		return CreatedInvite{}, err
	}
	id, err := security.NewID()
	if err != nil {
		return CreatedInvite{}, err
	}
	invite := domain.Invite{
		ID: id, PlanID: planID, TokenHash: s.security.HashToken(token), ShareBasisPoints: shareBPS,
		Status: "pending", ExpiresAt: s.now().Add(7 * 24 * time.Hour), CreatedAt: s.now(),
	}
	event, err := s.newAuditEvent(ownerID, "invite.created", "plan", planID, map[string]any{"invite_id": id, "share_basis_points": shareBPS})
	if err != nil {
		return CreatedInvite{}, err
	}
	if err := s.store.CreateInvite(ctx, planID, ownerID, invite, event); err != nil {
		return CreatedInvite{}, err
	}
	return CreatedInvite{Invite: invite, InviteURL: s.publicURL + "/#/invite/" + url.PathEscape(token)}, nil
}

func (s *Service) PreviewInvite(ctx context.Context, token string) (domain.InvitePreview, error) {
	if !strings.HasPrefix(token, "ss_invite_") {
		return domain.InvitePreview{}, domain.ErrInvalidInput
	}
	return s.store.InvitePreview(ctx, s.security.HashToken(token), s.now())
}

func (s *Service) AcceptInvite(ctx context.Context, user domain.User, token string) (domain.Member, error) {
	if !strings.HasPrefix(token, "ss_invite_") {
		return domain.Member{}, domain.ErrInvalidInput
	}
	id, err := security.NewID()
	if err != nil {
		return domain.Member{}, err
	}
	event, err := s.newAuditEvent(user.ID, "invite.accepted", "invite", "", nil)
	if err != nil {
		return domain.Member{}, err
	}
	return s.store.AcceptInvite(ctx, s.security.HashToken(token), user, id, s.now(), event)
}

func (s *Service) RevokeInvite(ctx context.Context, ownerID, planID, inviteID string) (domain.Invite, error) {
	event, err := s.newAuditEvent(ownerID, "invite.revoked", "plan", planID, map[string]string{"invite_id": inviteID})
	if err != nil {
		return domain.Invite{}, err
	}
	return s.store.RevokeInvite(ctx, planID, ownerID, inviteID, event)
}

func (s *Service) UpdateMemberShare(ctx context.Context, ownerID, planID, memberID string, shareBPS int) (domain.Member, error) {
	if shareBPS < 0 || shareBPS > domain.MaxShareBPS {
		return domain.Member{}, domain.ErrInvalidInput
	}
	event, err := s.newAuditEvent(ownerID, "member.share_updated", "plan", planID, map[string]any{"member_id": memberID, "share_basis_points": shareBPS})
	if err != nil {
		return domain.Member{}, err
	}
	return s.store.UpdateMemberShare(ctx, planID, ownerID, memberID, shareBPS, event)
}
