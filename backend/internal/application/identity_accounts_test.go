package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type registrationStore struct {
	Store
	user       domain.User
	acceptance domain.AgreementAcceptance
	created    bool
}

func (s *registrationStore) CreateUserWithAgreement(_ context.Context, user domain.User, acceptance domain.AgreementAcceptance) error {
	s.user = user
	s.acceptance = acceptance
	s.created = true
	return nil
}

func (s *registrationStore) CreateSession(context.Context, string, string, []byte, time.Time) error {
	return nil
}

func currentRegistrationAgreement() RegistrationAgreement {
	return RegistrationAgreement{
		Accepted: true, TermsVersion: CurrentTermsVersion, PrivacyPolicyVersion: CurrentPrivacyPolicyVersion,
		AcceptableUseVersion: CurrentAcceptableUseVersion,
	}
}

func TestRegisterRequiresCurrentAgreementVersions(t *testing.T) {
	tests := []struct {
		name      string
		agreement RegistrationAgreement
	}{
		{name: "not accepted", agreement: RegistrationAgreement{TermsVersion: CurrentTermsVersion, PrivacyPolicyVersion: CurrentPrivacyPolicyVersion, AcceptableUseVersion: CurrentAcceptableUseVersion}},
		{name: "stale terms", agreement: RegistrationAgreement{Accepted: true, TermsVersion: "2026-01-01", PrivacyPolicyVersion: CurrentPrivacyPolicyVersion, AcceptableUseVersion: CurrentAcceptableUseVersion}},
		{name: "missing privacy", agreement: RegistrationAgreement{Accepted: true, TermsVersion: CurrentTermsVersion, AcceptableUseVersion: CurrentAcceptableUseVersion}},
		{name: "missing acceptable use", agreement: RegistrationAgreement{Accepted: true, TermsVersion: CurrentTermsVersion, PrivacyPolicyVersion: CurrentPrivacyPolicyVersion}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &registrationStore{}
			service := &Service{store: store, security: testSecurityManager(t), now: time.Now}
			_, err := service.Register(context.Background(), "member", "member@example.com", "strong-password", tt.agreement)
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("Register() error = %v, want invalid input", err)
			}
			if store.created {
				t.Fatal("user was created without accepting every current agreement")
			}
		})
	}
}

func TestRegisterPersistsAgreementWithServerTime(t *testing.T) {
	now := time.Date(2026, time.August, 5, 9, 30, 0, 0, time.UTC)
	store := &registrationStore{}
	service := &Service{store: store, security: testSecurityManager(t), sessionTTL: time.Hour, now: func() time.Time { return now }}
	result, err := service.Register(context.Background(), " member ", "MEMBER@example.com", "strong-password", currentRegistrationAgreement())
	if err != nil {
		t.Fatal(err)
	}
	if result.User.ID == "" || result.Token == "" || !store.created {
		t.Fatalf("registration result = %+v, created = %v", result, store.created)
	}
	if store.acceptance.UserID != store.user.ID || store.acceptance.TermsVersion != CurrentTermsVersion || store.acceptance.PrivacyPolicyVersion != CurrentPrivacyPolicyVersion || store.acceptance.AcceptableUseVersion != CurrentAcceptableUseVersion || !store.acceptance.AcceptedAt.Equal(now) {
		t.Fatalf("agreement acceptance = %+v", store.acceptance)
	}
}
