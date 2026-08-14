package application

import (
	"context"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

func requireAdmin(admin domain.User) error {
	if !admin.IsAdmin {
		return domain.ErrForbidden
	}
	return nil
}

func (s *Service) AdminOverview(ctx context.Context, admin domain.User) (domain.AdminOverview, error) {
	if err := requireAdmin(admin); err != nil {
		return domain.AdminOverview{}, err
	}
	return s.store.AdminOverview(ctx, s.now().Add(-24*time.Hour))
}

func (s *Service) AdminListUsers(ctx context.Context, admin domain.User) ([]domain.AdminUser, error) {
	if err := requireAdmin(admin); err != nil {
		return nil, err
	}
	users, err := s.store.AdminListUsers(ctx)
	if err != nil {
		return nil, err
	}
	for index := range users {
		users[index].User = s.decorateUser(users[index].User)
	}
	return users, nil
}

func (s *Service) AdminUpdateUserStatus(ctx context.Context, admin domain.User, userID, status string) (domain.User, error) {
	if err := requireAdmin(admin); err != nil {
		return domain.User{}, err
	}
	if userID == admin.ID || (status != domain.StatusActive && status != domain.StatusDisabled) {
		return domain.User{}, domain.ErrInvalidInput
	}
	user, err := s.store.AdminUpdateUserStatus(ctx, userID, status)
	if err != nil {
		return domain.User{}, err
	}
	return s.decorateUser(user), nil
}

func (s *Service) AdminListAccounts(ctx context.Context, admin domain.User) ([]domain.AdminAccount, error) {
	if err := requireAdmin(admin); err != nil {
		return nil, err
	}
	accounts, err := s.store.AdminListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	for index := range accounts {
		if err := s.hydrateAccountProxy(&accounts[index].Account); err != nil {
			return nil, err
		}
	}
	return accounts, nil
}

func (s *Service) AdminAccount(ctx context.Context, admin domain.User, accountID string) (domain.AdminAccount, error) {
	accounts, err := s.AdminListAccounts(ctx, admin)
	if err != nil {
		return domain.AdminAccount{}, err
	}
	for _, account := range accounts {
		if account.ID == accountID {
			return account, nil
		}
	}
	return domain.AdminAccount{}, domain.ErrNotFound
}

func (s *Service) AdminUpdateAccountConfig(ctx context.Context, admin domain.User, accountID string, config AccountConfigInput) (domain.AdminAccount, error) {
	if err := requireAdmin(admin); err != nil {
		return domain.AdminAccount{}, err
	}
	account, err := s.store.AccountByID(ctx, accountID)
	if err != nil {
		return domain.AdminAccount{}, err
	}
	if _, err := s.updateAccountConfig(ctx, admin.ID, account.OwnerUserID, accountID, config); err != nil {
		return domain.AdminAccount{}, err
	}
	accounts, err := s.AdminListAccounts(ctx, admin)
	if err != nil {
		return domain.AdminAccount{}, err
	}
	for _, item := range accounts {
		if item.ID == accountID {
			return item, nil
		}
	}
	return domain.AdminAccount{}, domain.ErrNotFound
}

func (s *Service) AdminUpdateAccountStatus(ctx context.Context, admin domain.User, accountID, status string) (domain.AdminAccount, error) {
	if err := requireAdmin(admin); err != nil {
		return domain.AdminAccount{}, err
	}
	if status != domain.StatusActive && status != domain.StatusDisabled {
		return domain.AdminAccount{}, domain.ErrInvalidInput
	}
	event, err := s.newAuditEvent(admin.ID, "account.status_updated", "account", accountID, map[string]string{"status": status})
	if err != nil {
		return domain.AdminAccount{}, err
	}
	if _, err := s.store.AdminUpdateAccountStatus(ctx, accountID, status, event); err != nil {
		return domain.AdminAccount{}, err
	}
	accounts, err := s.AdminListAccounts(ctx, admin)
	if err != nil {
		return domain.AdminAccount{}, err
	}
	for _, item := range accounts {
		if item.ID == accountID {
			return item, nil
		}
	}
	return domain.AdminAccount{}, domain.ErrNotFound
}

func (s *Service) AdminRecordPlanAction(ctx context.Context, admin domain.User, planID, action string) error {
	if _, err := s.adminPlan(ctx, admin, planID); err != nil {
		return err
	}
	event, err := s.newAuditEvent(admin.ID, action, "plan", planID, nil)
	if err != nil {
		return err
	}
	return s.store.RecordAuditEvent(ctx, event)
}

func (s *Service) AdminListPlans(ctx context.Context, admin domain.User) ([]domain.AdminPlan, error) {
	if err := requireAdmin(admin); err != nil {
		return nil, err
	}
	return s.store.AdminListPlans(ctx, s.now().Add(-24*time.Hour))
}

func (s *Service) AdminPlanDetail(ctx context.Context, admin domain.User, planID, timezone string) (domain.PlanDetail, error) {
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return domain.PlanDetail{}, err
	}
	return s.PlanDetail(ctx, plan.OwnerUserID, planID, timezone)
}

func (s *Service) AdminPlanPerformance(ctx context.Context, admin domain.User, planID, period, timezone string) (domain.PlanPerformance, error) {
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return domain.PlanPerformance{}, err
	}
	return s.PlanPerformance(ctx, plan.OwnerUserID, planID, period, timezone)
}

func (s *Service) AdminPlanRequestErrors(ctx context.Context, admin domain.User, planID, period, timezone string, page, pageSize int) (domain.PlanRequestErrorList, error) {
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return domain.PlanRequestErrorList{}, err
	}
	return s.PlanRequestErrors(ctx, plan.OwnerUserID, planID, period, timezone, page, pageSize)
}

func (s *Service) AdminListPlanAuditEvents(ctx context.Context, admin domain.User, planID string) ([]domain.AuditEvent, error) {
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return nil, err
	}
	return s.ListPlanAuditEvents(ctx, plan.OwnerUserID, planID)
}

func (s *Service) adminPlan(ctx context.Context, admin domain.User, planID string) (domain.Plan, error) {
	if err := requireAdmin(admin); err != nil {
		return domain.Plan{}, err
	}
	plan, err := s.store.AdminPlanByID(ctx, planID)
	if err != nil {
		return domain.Plan{}, err
	}
	return plan, nil
}

func (s *Service) AdminRenamePlan(ctx context.Context, admin domain.User, planID, name string) (domain.Plan, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return domain.Plan{}, err
	}
	event, err := s.newAuditEvent(admin.ID, "plan.renamed", "plan", planID, map[string]string{"name": name})
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.RenamePlan(ctx, planID, plan.OwnerUserID, name, event)
}

func (s *Service) AdminUpdatePlanDescription(ctx context.Context, admin domain.User, planID, description string) (domain.Plan, error) {
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) > maxPlanDescriptionLength {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return domain.Plan{}, err
	}
	event, err := s.newAuditEvent(admin.ID, "plan.description_updated", "plan", planID, nil)
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.UpdatePlanDescription(ctx, planID, plan.OwnerUserID, description, event)
}

func (s *Service) AdminUpdatePlanStatus(ctx context.Context, admin domain.User, planID, status string) (domain.Plan, error) {
	if status != domain.StatusActive && status != domain.StatusArchived {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return domain.Plan{}, err
	}
	action := "plan.restored"
	if status == domain.StatusArchived {
		action = "plan.archived"
	}
	event, err := s.newAuditEvent(admin.ID, action, "plan", planID, nil)
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.UpdatePlanStatus(ctx, planID, plan.OwnerUserID, status, event)
}

func (s *Service) AdminRebindPlanAccount(ctx context.Context, admin domain.User, planID, accountID string) (domain.Plan, error) {
	if strings.TrimSpace(accountID) == "" {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return domain.Plan{}, err
	}
	_, release, err := s.quiescePlanBinding(ctx, plan.OwnerUserID, planID, accountID)
	if err != nil {
		return domain.Plan{}, err
	}
	defer release()
	account, signals, observedAt, err := s.probeAccountQuota(ctx, plan.OwnerUserID, accountID)
	if err != nil {
		return domain.Plan{}, err
	}
	event, err := s.newAuditEvent(admin.ID, "plan.account_rebound", "plan", planID, map[string]string{"account_id": accountID})
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.RebindPlanAccount(ctx, planID, plan.OwnerUserID, account.ID, signals, observedAt, event)
}

func (s *Service) AdminUpdatePlanPublication(ctx context.Context, admin domain.User, planID, visibility string, slots, shareBPS int) (domain.Plan, error) {
	if visibility != domain.VisibilityPrivate && visibility != domain.VisibilityPublic {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	if visibility == domain.VisibilityPublic && (slots < 1 || slots > 100 || shareBPS < 0 || shareBPS > domain.MaxShareBPS) {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return domain.Plan{}, err
	}
	event, err := s.newAuditEvent(admin.ID, "plan.publication_updated", "plan", planID, map[string]any{"visibility": visibility, "public_slots": slots, "public_share_basis_points": shareBPS})
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.UpdatePlanPublication(ctx, plan.OwnerUserID, planID, visibility, slots, shareBPS, event)
}

func (s *Service) AdminDeletePlan(ctx context.Context, admin domain.User, planID string) error {
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return err
	}
	_, release, err := s.quiescePlanBinding(ctx, plan.OwnerUserID, planID)
	if err != nil {
		return err
	}
	defer release()
	event, err := s.newAuditEvent(admin.ID, "plan.deleted", "plan", planID, nil)
	if err != nil {
		return err
	}
	return s.store.DeletePlan(ctx, planID, plan.OwnerUserID, event)
}

func (s *Service) AdminTransferPlanOwnership(ctx context.Context, admin domain.User, planID, memberID string) (domain.Plan, error) {
	if strings.TrimSpace(memberID) == "" {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return domain.Plan{}, err
	}
	event, err := s.newAuditEvent(admin.ID, "plan.owner_transferred", "plan", planID, map[string]string{"member_id": memberID})
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.TransferPlanOwnership(ctx, planID, plan.OwnerUserID, memberID, event)
}

func (s *Service) AdminInvite(ctx context.Context, admin domain.User, planID string, shareBPS int) (CreatedInvite, error) {
	if shareBPS < 0 || shareBPS > domain.MaxShareBPS {
		return CreatedInvite{}, domain.ErrInvalidInput
	}
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return CreatedInvite{}, err
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
	event, err := s.newAuditEvent(admin.ID, "invite.created", "plan", planID, map[string]any{"invite_id": id, "share_basis_points": shareBPS})
	if err != nil {
		return CreatedInvite{}, err
	}
	if err := s.store.CreateInvite(ctx, planID, plan.OwnerUserID, invite, event); err != nil {
		return CreatedInvite{}, err
	}
	return CreatedInvite{Invite: invite, InviteURL: s.publicURL + "/#/invite/" + url.PathEscape(token)}, nil
}

func (s *Service) AdminRevokeInvite(ctx context.Context, admin domain.User, planID, inviteID string) (domain.Invite, error) {
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return domain.Invite{}, err
	}
	event, err := s.newAuditEvent(admin.ID, "invite.revoked", "plan", planID, map[string]string{"invite_id": inviteID})
	if err != nil {
		return domain.Invite{}, err
	}
	return s.store.RevokeInvite(ctx, planID, plan.OwnerUserID, inviteID, event)
}

func (s *Service) AdminReviewJoinApplication(ctx context.Context, admin domain.User, planID, applicationID string, approve bool) (domain.JoinApplication, error) {
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return domain.JoinApplication{}, err
	}
	memberID, err := security.NewID()
	if err != nil {
		return domain.JoinApplication{}, err
	}
	action := "application.rejected"
	if approve {
		action = "application.approved"
	}
	event, err := s.newAuditEvent(admin.ID, action, "join_application", applicationID, nil)
	if err != nil {
		return domain.JoinApplication{}, err
	}
	return s.store.ReviewJoinApplication(ctx, plan.OwnerUserID, planID, applicationID, approve, memberID, s.now(), event)
}

func (s *Service) AdminUpdateMemberShare(ctx context.Context, admin domain.User, planID, memberID string, shareBPS int) (domain.Member, error) {
	if shareBPS < 0 || shareBPS > domain.MaxShareBPS {
		return domain.Member{}, domain.ErrInvalidInput
	}
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return domain.Member{}, err
	}
	event, err := s.newAuditEvent(admin.ID, "member.share_updated", "plan", planID, map[string]any{"member_id": memberID, "share_basis_points": shareBPS})
	if err != nil {
		return domain.Member{}, err
	}
	return s.store.UpdateMemberShare(ctx, planID, plan.OwnerUserID, memberID, shareBPS, event)
}

func (s *Service) AdminRemovePlanMember(ctx context.Context, admin domain.User, planID, memberID string) error {
	if strings.TrimSpace(memberID) == "" {
		return domain.ErrInvalidInput
	}
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return err
	}
	event, err := s.newAuditEvent(admin.ID, "member.removed", "plan", planID, map[string]string{"member_id": memberID})
	if err != nil {
		return err
	}
	return s.store.RemovePlanMember(ctx, planID, plan.OwnerUserID, memberID, event)
}

func (s *Service) AdminReservePlanQuotaProbe(ctx context.Context, admin domain.User, planID string) (PlanQuotaProbe, func(), error) {
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return PlanQuotaProbe{}, nil, err
	}
	return s.ReservePlanQuotaProbe(ctx, plan.OwnerUserID, planID)
}

func (s *Service) AdminPreparePlanQuotaProbe(ctx context.Context, admin domain.User, planID string) (PlanQuotaProbe, error) {
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return PlanQuotaProbe{}, err
	}
	return s.PreparePlanQuotaProbe(ctx, plan.OwnerUserID, planID)
}

func (s *Service) AdminQuiescePlanQuota(ctx context.Context, admin domain.User, planID string) (PlanQuotaProbe, func(), error) {
	plan, err := s.adminPlan(ctx, admin, planID)
	if err != nil {
		return PlanQuotaProbe{}, nil, err
	}
	return s.QuiescePlanQuota(ctx, plan.OwnerUserID, planID)
}

func (s *Service) AdminListAPIKeys(ctx context.Context, admin domain.User) ([]domain.AdminAPIKey, error) {
	if err := requireAdmin(admin); err != nil {
		return nil, err
	}
	return s.store.AdminListAPIKeys(ctx)
}

func (s *Service) AdminRevokeAPIKey(ctx context.Context, admin domain.User, keyID string) error {
	if err := requireAdmin(admin); err != nil {
		return err
	}
	return s.store.AdminRevokeAPIKey(ctx, keyID)
}
