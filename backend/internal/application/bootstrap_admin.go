package application

import (
	"context"
	"strings"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

const BootstrapAdminEmail = "admin@underelay.com"

type BootstrapAdminCredential struct {
	Email             string
	TemporaryPassword string
}

func (s *Service) EnsureBootstrapAdmin(ctx context.Context) (*BootstrapAdminCredential, error) {
	password, err := security.NewOpaqueToken("")
	if err != nil {
		return nil, err
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return nil, err
	}
	id, err := security.NewID()
	if err != nil {
		return nil, err
	}
	created, err := s.store.EnsureBootstrapAdmin(ctx, domain.User{
		ID: id, Username: "admin", Email: BootstrapAdminEmail, PasswordHash: hash,
		Status: domain.StatusActive, Role: domain.RoleAdmin, MustChangePassword: true, CreatedAt: s.now(),
	})
	if err != nil || !created {
		return nil, err
	}
	return &BootstrapAdminCredential{Email: BootstrapAdminEmail, TemporaryPassword: password}, nil
}

func (s *Service) ResetAdminPassword(ctx context.Context, email string) (*BootstrapAdminCredential, error) {
	email = normalizeEmail(email)
	if email == "" {
		email = BootstrapAdminEmail
	}
	password, err := security.NewOpaqueToken("")
	if err != nil {
		return nil, err
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.ResetAdminPassword(ctx, strings.TrimSpace(email), hash); err != nil {
		return nil, err
	}
	return &BootstrapAdminCredential{Email: email, TemporaryPassword: password}, nil
}
