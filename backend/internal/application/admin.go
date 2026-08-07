package application

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sharesub/sharesub/backend/internal/domain"
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

func (s *Service) AdminUpdateAccountConfig(ctx context.Context, admin domain.User, accountID string, config AccountConfigInput) (domain.AdminAccount, error) {
	if err := requireAdmin(admin); err != nil {
		return domain.AdminAccount{}, err
	}
	account, err := s.store.AccountByID(ctx, accountID)
	if err != nil {
		return domain.AdminAccount{}, err
	}
	if _, err := s.UpdateAccountConfig(ctx, account.OwnerUserID, accountID, config); err != nil {
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
	if _, err := s.store.AdminUpdateAccountStatus(ctx, accountID, status); err != nil {
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

func (s *Service) AdminListPlans(ctx context.Context, admin domain.User) ([]domain.AdminPlan, error) {
	if err := requireAdmin(admin); err != nil {
		return nil, err
	}
	return s.store.AdminListPlans(ctx, s.now().Add(-24*time.Hour))
}

func (s *Service) adminPlan(ctx context.Context, admin domain.User, planID string) (domain.AdminPlan, error) {
	if err := requireAdmin(admin); err != nil {
		return domain.AdminPlan{}, err
	}
	plans, err := s.store.AdminListPlans(ctx, s.now().Add(-24*time.Hour))
	if err != nil {
		return domain.AdminPlan{}, err
	}
	for _, plan := range plans {
		if plan.ID == planID {
			return plan, nil
		}
	}
	return domain.AdminPlan{}, domain.ErrNotFound
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
	event, err := s.newAuditEvent(admin.ID, "plan.account_rebound", "plan", planID, map[string]string{"account_id": accountID})
	if err != nil {
		return domain.Plan{}, err
	}
	return s.store.RebindPlanAccount(ctx, planID, plan.OwnerUserID, accountID, event)
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
