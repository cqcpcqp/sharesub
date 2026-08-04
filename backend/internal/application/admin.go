package application

import (
	"context"
	"time"

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
	return s.store.AdminListAccounts(ctx)
}

func (s *Service) AdminUpdateAccountStatus(ctx context.Context, admin domain.User, accountID, status string) (domain.AdminAccount, error) {
	if err := requireAdmin(admin); err != nil {
		return domain.AdminAccount{}, err
	}
	if status != domain.StatusActive && status != domain.StatusDisabled {
		return domain.AdminAccount{}, domain.ErrInvalidInput
	}
	return s.store.AdminUpdateAccountStatus(ctx, accountID, status)
}

func (s *Service) AdminListPlans(ctx context.Context, admin domain.User) ([]domain.AdminPlan, error) {
	if err := requireAdmin(admin); err != nil {
		return nil, err
	}
	return s.store.AdminListPlans(ctx, s.now().Add(-24*time.Hour))
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
