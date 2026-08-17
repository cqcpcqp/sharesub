package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

type registrationStore struct {
	Store
	user           domain.User
	acceptance     domain.AgreementAcceptance
	verification   domain.EmailVerificationToken
	created        bool
	sessionMade    bool
	consumeErr     error
	consumedHash   []byte
	createTokenErr error
	createdToken   domain.EmailVerificationToken
	deleteTokenErr error
	deletedTokenID string
	supersedeErr   error
	supersededID   string
	supersededAt   time.Time
	cooldown       time.Duration
	limitWindow    time.Duration
	limit          int
}

func (s *registrationStore) CreateUserWithAgreement(_ context.Context, user domain.User, acceptance domain.AgreementAcceptance) error {
	s.user = user
	s.acceptance = acceptance
	s.created = true
	return nil
}

func (s *registrationStore) CreateUserWithEmailVerification(_ context.Context, user domain.User, acceptance domain.AgreementAcceptance, verification domain.EmailVerificationToken) error {
	s.user = user
	s.acceptance = acceptance
	s.verification = verification
	s.created = true
	return nil
}

func (s *registrationStore) CreateSession(context.Context, string, string, []byte, time.Time) error {
	s.sessionMade = true
	return nil
}

func (s *registrationStore) UserByEmail(context.Context, string) (domain.User, error) {
	if s.user.ID == "" {
		return domain.User{}, domain.ErrNotFound
	}
	return s.user, nil
}

func (s *registrationStore) ConsumeEmailVerificationToken(_ context.Context, hash []byte, _ time.Time) (domain.User, error) {
	s.consumedHash = hash
	if s.consumeErr != nil {
		return domain.User{}, s.consumeErr
	}
	return s.user, nil
}

func (s *registrationStore) CreateEmailVerificationToken(_ context.Context, verification domain.EmailVerificationToken, cooldown, limitWindow time.Duration, limit int) error {
	s.createdToken = verification
	s.cooldown = cooldown
	s.limitWindow = limitWindow
	s.limit = limit
	return s.createTokenErr
}

func (s *registrationStore) DeleteEmailVerificationToken(_ context.Context, verificationID, _ string) error {
	s.deletedTokenID = verificationID
	return s.deleteTokenErr
}

func (s *registrationStore) SupersedeEmailVerificationTokens(_ context.Context, _ string, verificationID string, now time.Time) error {
	s.supersededID = verificationID
	s.supersededAt = now
	return s.supersedeErr
}

type recordingEmailSender struct {
	recipient string
	token     string
	err       error
}

func (s *recordingEmailSender) SendEmailVerification(_ context.Context, recipient, token string) error {
	s.recipient = recipient
	s.token = token
	return s.err
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
	sender := &recordingEmailSender{}
	service := &Service{store: store, security: testSecurityManager(t), emailSender: sender, emailVerificationTTL: time.Hour, emailResendCooldown: time.Minute, now: func() time.Time { return now }}
	result, err := service.Register(context.Background(), " member ", "MEMBER@example.com", "strong-password", currentRegistrationAgreement())
	if err != nil {
		t.Fatal(err)
	}
	if result.Email != "member@example.com" || !result.VerificationExpiresAt.Equal(now.Add(time.Hour)) || !result.ResendAvailableAt.Equal(now.Add(time.Minute)) || !store.created {
		t.Fatalf("registration result = %+v, created = %v", result, store.created)
	}
	if sender.recipient != result.Email || sender.token == "" || store.sessionMade {
		t.Fatalf("email sender = %+v, session made = %v", sender, store.sessionMade)
	}
	if string(store.verification.TokenHash) == sender.token || !store.verification.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("verification = %+v", store.verification)
	}
	if store.acceptance.UserID != store.user.ID || store.acceptance.TermsVersion != CurrentTermsVersion || store.acceptance.PrivacyPolicyVersion != CurrentPrivacyPolicyVersion || store.acceptance.AcceptableUseVersion != CurrentAcceptableUseVersion || !store.acceptance.AcceptedAt.Equal(now) {
		t.Fatalf("agreement acceptance = %+v", store.acceptance)
	}
}

func TestRegisterKeepsUnverifiedAccountWhenDeliveryFails(t *testing.T) {
	store := &registrationStore{}
	sender := &recordingEmailSender{err: errors.New("SES unavailable")}
	service := &Service{store: store, security: testSecurityManager(t), emailSender: sender, emailVerificationTTL: time.Hour, emailResendCooldown: time.Minute, now: time.Now}
	_, err := service.Register(context.Background(), "member", "member@example.com", "strong-password", currentRegistrationAgreement())
	if !errors.Is(err, domain.ErrEmailDeliveryUnavailable) || !store.created || store.sessionMade {
		t.Fatalf("Register() error = %v, created = %v, session = %v", err, store.created, store.sessionMade)
	}
}

func TestResendEmailVerificationCreatesLimitedTokenAndSendsIt(t *testing.T) {
	now := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	store := &registrationStore{user: domain.User{ID: "member", Email: "member@example.com", Status: domain.StatusActive}}
	sender := &recordingEmailSender{}
	service := &Service{store: store, security: testSecurityManager(t), emailSender: sender, emailVerificationTTL: time.Hour, emailResendCooldown: time.Minute, now: func() time.Time { return now }}
	result, err := service.ResendEmailVerification(context.Background(), " MEMBER@example.com ")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || !result.ResendAvailableAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("dispatch = %+v", result)
	}
	if sender.recipient != store.user.Email || sender.token == "" || string(store.createdToken.TokenHash) == sender.token {
		t.Fatalf("sender = %+v, verification = %+v", sender, store.createdToken)
	}
	if store.createdToken.UserID != store.user.ID || !store.createdToken.ExpiresAt.Equal(now.Add(time.Hour)) || store.cooldown != time.Minute || store.limitWindow != time.Hour || store.limit != 5 {
		t.Fatalf("verification limits = token %+v, cooldown %s, window %s, limit %d", store.createdToken, store.cooldown, store.limitWindow, store.limit)
	}
	if store.supersededID != store.createdToken.ID || !store.supersededAt.Equal(now) || store.deletedTokenID != "" {
		t.Fatalf("verification finalization = superseded %q at %s, deleted %q", store.supersededID, store.supersededAt, store.deletedTokenID)
	}
}

func TestResendEmailVerificationDiscardsUnsentTokenWithoutSupersedingOldLink(t *testing.T) {
	store := &registrationStore{user: domain.User{ID: "member", Email: "member@example.com", Status: domain.StatusActive}}
	sender := &recordingEmailSender{err: errors.New("SES unavailable")}
	service := &Service{store: store, security: testSecurityManager(t), emailSender: sender, emailVerificationTTL: time.Hour, emailResendCooldown: time.Minute, now: time.Now}
	_, err := service.ResendEmailVerification(context.Background(), store.user.Email)
	if !errors.Is(err, domain.ErrEmailDeliveryUnavailable) {
		t.Fatalf("ResendEmailVerification() error = %v", err)
	}
	if store.deletedTokenID != store.createdToken.ID || store.supersededID != "" {
		t.Fatalf("failed delivery deleted %q and superseded %q", store.deletedTokenID, store.supersededID)
	}
}

func TestResendEmailVerificationDoesNotRevealUnknownOrVerifiedEmail(t *testing.T) {
	now := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name  string
		store *registrationStore
	}{
		{name: "unknown", store: &registrationStore{}},
		{name: "verified", store: &registrationStore{user: domain.User{ID: "member", Email: "member@example.com", EmailVerifiedAt: &now}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sender := &recordingEmailSender{}
			service := &Service{store: tt.store, security: testSecurityManager(t), emailSender: sender, emailVerificationTTL: time.Hour, emailResendCooldown: time.Minute, now: func() time.Time { return now }}
			result, err := service.ResendEmailVerification(context.Background(), "member@example.com")
			if err != nil || !result.Accepted || sender.token != "" || tt.store.createdToken.ID != "" {
				t.Fatalf("dispatch = %+v, error = %v, sender = %+v, token = %+v", result, err, sender, tt.store.createdToken)
			}
		})
	}
}

func TestResendEmailVerificationPropagatesRateLimitsWithoutSending(t *testing.T) {
	for _, expected := range []error{domain.ErrEmailResendTooSoon, domain.ErrEmailVerificationLimited} {
		store := &registrationStore{user: domain.User{ID: "member", Email: "member@example.com"}, createTokenErr: expected}
		sender := &recordingEmailSender{}
		service := &Service{store: store, security: testSecurityManager(t), emailSender: sender, emailVerificationTTL: time.Hour, emailResendCooldown: time.Minute, now: time.Now}
		_, err := service.ResendEmailVerification(context.Background(), store.user.Email)
		if !errors.Is(err, expected) || sender.token != "" {
			t.Fatalf("ResendEmailVerification() error = %v, sender = %+v", err, sender)
		}
	}
}

func TestLoginRequiresVerifiedEmailAfterPasswordCheck(t *testing.T) {
	password := "strong-password"
	hash, err := security.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	store := &registrationStore{user: domain.User{ID: "member", Email: "member@example.com", PasswordHash: hash, Status: domain.StatusActive}}
	service := &Service{store: store, security: testSecurityManager(t), now: time.Now}
	if _, err := service.Login(context.Background(), store.user.Email, "wrong-password"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("wrong password error = %v", err)
	}
	if _, err := service.Login(context.Background(), store.user.Email, password); !errors.Is(err, domain.ErrEmailVerificationRequired) {
		t.Fatalf("unverified login error = %v", err)
	}
}

func TestVerifyEmailConsumesHashedTokenAndCreatesSession(t *testing.T) {
	now := time.Date(2026, time.August, 17, 9, 0, 0, 0, time.UTC)
	verifiedAt := now
	store := &registrationStore{user: domain.User{ID: "member", Email: "member@example.com", Status: domain.StatusActive, EmailVerifiedAt: &verifiedAt}}
	manager := testSecurityManager(t)
	service := &Service{store: store, security: manager, sessionTTL: time.Hour, now: func() time.Time { return now }}
	token := `ss_verify_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ`
	result, err := service.VerifyEmail(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if result.Token == "" || result.User.ID != store.user.ID || !store.sessionMade {
		t.Fatalf("verification result = %+v, session = %v", result, store.sessionMade)
	}
	if string(store.consumedHash) != string(manager.HashToken(token)) {
		t.Fatal("verification token was not hashed before lookup")
	}
}

func TestVerifyEmailRejectsExpiredOrConsumedToken(t *testing.T) {
	store := &registrationStore{consumeErr: domain.ErrNotFound}
	service := &Service{store: store, security: testSecurityManager(t), now: time.Now}
	if _, err := service.VerifyEmail(context.Background(), `ss_verify_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ`); !errors.Is(err, domain.ErrEmailVerificationInvalid) {
		t.Fatalf("VerifyEmail() error = %v", err)
	}
}
