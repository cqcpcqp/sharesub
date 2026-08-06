package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

const (
	CurrentTermsVersion         = "2026-08-05"
	CurrentPrivacyPolicyVersion = "2026-08-05"
	CurrentAcceptableUseVersion = "2026-08-05"
)

type RegistrationAgreement struct {
	Accepted             bool   `json:"accepted"`
	TermsVersion         string `json:"terms_version"`
	PrivacyPolicyVersion string `json:"privacy_policy_version"`
	AcceptableUseVersion string `json:"acceptable_use_version"`
}

func (s *Service) Register(ctx context.Context, username, email, password string, agreement RegistrationAgreement) (AuthResult, error) {
	email = normalizeEmail(email)
	username = strings.TrimSpace(username)
	if !validEmail(email) || !validUsername(username) || !validRegistrationAgreement(agreement) {
		return AuthResult{}, fmt.Errorf("%w: invalid registration", domain.ErrInvalidInput)
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return AuthResult{}, fmt.Errorf("%w: %v", domain.ErrInvalidInput, err)
	}
	id, err := security.NewID()
	if err != nil {
		return AuthResult{}, err
	}
	now := s.now()
	user := domain.User{ID: id, Username: username, Email: email, PasswordHash: hash, Status: domain.StatusActive, Role: domain.RoleUser, CreatedAt: now}
	acceptance := domain.AgreementAcceptance{
		UserID: user.ID, TermsVersion: agreement.TermsVersion, PrivacyPolicyVersion: agreement.PrivacyPolicyVersion,
		AcceptableUseVersion: agreement.AcceptableUseVersion, AcceptedAt: now,
	}
	if err := s.store.CreateUserWithAgreement(ctx, user, acceptance); err != nil {
		return AuthResult{}, err
	}
	return s.newSession(ctx, s.decorateUser(user))
}

func validRegistrationAgreement(agreement RegistrationAgreement) bool {
	return agreement.Accepted &&
		agreement.TermsVersion == CurrentTermsVersion &&
		agreement.PrivacyPolicyVersion == CurrentPrivacyPolicyVersion &&
		agreement.AcceptableUseVersion == CurrentAcceptableUseVersion
}

func (s *Service) UpdateUsername(ctx context.Context, userID, username string) (domain.User, error) {
	username = strings.TrimSpace(username)
	if !validUsername(username) {
		return domain.User{}, domain.ErrInvalidInput
	}
	user, err := s.store.UpdateUsername(ctx, userID, username)
	if err != nil {
		return domain.User{}, err
	}
	return s.decorateUser(user), nil
}

func (s *Service) UpdateUserAvatar(ctx context.Context, userID string, data []byte) (domain.User, error) {
	if len(data) == 0 || len(data) > MaxAvatarBytes {
		return domain.User{}, domain.ErrInvalidInput
	}
	mediaType := http.DetectContentType(data)
	if mediaType != "image/jpeg" && mediaType != "image/png" && mediaType != "image/webp" {
		return domain.User{}, domain.ErrInvalidInput
	}
	user, err := s.store.UpdateUserAvatar(ctx, userID, domain.UserAvatar{Data: data, MediaType: mediaType}, s.now())
	if err != nil {
		return domain.User{}, err
	}
	return s.decorateUser(user), nil
}

func (s *Service) DeleteUserAvatar(ctx context.Context, userID string) (domain.User, error) {
	user, err := s.store.DeleteUserAvatar(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}
	return s.decorateUser(user), nil
}

func (s *Service) UserAvatar(ctx context.Context, userID string) (domain.UserAvatar, error) {
	if strings.TrimSpace(userID) == "" {
		return domain.UserAvatar{}, domain.ErrInvalidInput
	}
	return s.store.UserAvatar(ctx, userID)
}

func (s *Service) ChangePassword(ctx context.Context, user domain.User, currentPassword, newPassword, currentSessionToken string) (domain.User, error) {
	if !security.CheckPassword(user.PasswordHash, currentPassword) || security.CheckPassword(user.PasswordHash, newPassword) {
		return domain.User{}, domain.ErrInvalidInput
	}
	hash, err := security.HashPassword(newPassword)
	if err != nil {
		return domain.User{}, domain.ErrInvalidInput
	}
	updated, err := s.store.UpdatePassword(ctx, user.ID, hash, false, s.security.HashToken(currentSessionToken))
	if err != nil {
		return domain.User{}, err
	}
	return s.decorateUser(updated), nil
}

func (s *Service) Login(ctx context.Context, email, password string) (AuthResult, error) {
	user, err := s.store.UserByEmail(ctx, normalizeEmail(email))
	if err != nil || user.Status != domain.StatusActive || !security.CheckPassword(user.PasswordHash, password) {
		return AuthResult{}, domain.ErrUnauthorized
	}
	return s.newSession(ctx, s.decorateUser(user))
}

func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if !strings.HasPrefix(token, "ss_session_") {
		return domain.User{}, domain.ErrUnauthorized
	}
	user, err := s.store.UserBySessionHash(ctx, s.security.HashToken(token), s.now())
	if err != nil {
		return domain.User{}, err
	}
	return s.decorateUser(user), nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.DeleteSession(ctx, s.security.HashToken(token))
}

func (s *Service) newSession(ctx context.Context, user domain.User) (AuthResult, error) {
	token, err := security.NewOpaqueToken("ss_session_")
	if err != nil {
		return AuthResult{}, err
	}
	id, err := security.NewID()
	if err != nil {
		return AuthResult{}, err
	}
	if err := s.store.CreateSession(ctx, id, user.ID, s.security.HashToken(token), s.now().Add(s.sessionTTL)); err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: user, Token: token}, nil
}

func (s *Service) StartOpenAIConnect(ctx context.Context, userID string) (OAuthStart, error) {
	return s.startOpenAIOAuth(ctx, userID, "connect", "")
}

func (s *Service) StartOpenAIReauthorize(ctx context.Context, userID, accountID string) (OAuthStart, error) {
	account, err := s.store.AccountByID(ctx, accountID)
	if err != nil {
		return OAuthStart{}, err
	}
	if account.OwnerUserID != userID {
		return OAuthStart{}, domain.ErrForbidden
	}
	return s.startOpenAIOAuth(ctx, userID, "reauthorize", accountID)
}

func (s *Service) startOpenAIOAuth(ctx context.Context, userID, purpose, targetAccountID string) (OAuthStart, error) {
	state, err := security.NewOpaqueToken("")
	if err != nil {
		return OAuthStart{}, err
	}
	verifier, err := security.NewOpaqueToken("")
	if err != nil {
		return OAuthStart{}, err
	}
	flowID, err := security.NewID()
	if err != nil {
		return OAuthStart{}, err
	}
	challengeRaw := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeRaw[:])
	flow := OAuthFlow{
		ID: flowID, UserID: userID, StateHash: s.security.HashToken(state), CodeVerifier: verifier,
		RedirectURI: s.redirectURI, Purpose: purpose, TargetAccountID: targetAccountID, ExpiresAt: s.now().Add(oauthFlowTTL),
	}
	if err := s.store.CreateOAuthFlow(ctx, flow); err != nil {
		return OAuthStart{}, err
	}
	return OAuthStart{AuthorizationURL: s.oauth.AuthorizationURL(state, challenge, s.redirectURI), FlowID: flowID}, nil
}

func (s *Service) CompleteOpenAIConnect(ctx context.Context, userID, state, code string, config AccountConfigInput) (domain.Account, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return domain.Account{}, domain.ErrInvalidInput
	}
	config.Status = domain.StatusActive
	config, err := normalizeAccountConfig(config)
	if err != nil {
		return domain.Account{}, err
	}
	flow, err := s.store.ConsumeOAuthFlow(ctx, s.security.HashToken(state), s.now())
	if err != nil {
		return domain.Account{}, err
	}
	if flow.UserID != userID {
		return domain.Account{}, domain.ErrForbidden
	}
	if flow.Purpose != "connect" || flow.TargetAccountID != "" {
		return domain.Account{}, domain.ErrConflict
	}
	token, err := s.oauth.Exchange(ctx, code, flow.CodeVerifier, flow.RedirectURI)
	if err != nil {
		return domain.Account{}, err
	}
	if token.AccessToken == "" || token.RefreshToken == "" || token.Email == "" || token.ChatGPTAccountID == "" {
		return domain.Account{}, errors.New("OpenAI OAuth response is missing required account fields")
	}
	id, err := security.NewID()
	if err != nil {
		return domain.Account{}, err
	}
	credentialScope := userID + ":" + token.ChatGPTAccountID
	access, err := s.security.Encrypt(token.AccessToken, []byte(credentialScope+":access"))
	if err != nil {
		return domain.Account{}, err
	}
	refresh, err := s.security.Encrypt(token.RefreshToken, []byte(credentialScope+":refresh"))
	if err != nil {
		return domain.Account{}, err
	}
	account := domain.Account{
		ID: id, OwnerUserID: userID, Name: config.Name, Notes: config.Notes, Email: normalizeEmail(token.Email), ChatGPTAccountID: token.ChatGPTAccountID,
		PlanType: token.PlanType, AccessTokenCiphertext: access, RefreshTokenCiphertext: refresh,
		MaxConcurrency: config.MaxConcurrency, RPMLimit: config.RPMLimit, FastPolicy: config.FastPolicy,
		TokenExpiresAt: token.ExpiresAt, Status: config.Status, CreatedAt: s.now(),
	}
	if err := s.setAccountProxy(&account, config.ProxyURL); err != nil {
		return domain.Account{}, err
	}
	if subscriptionExpiresAt, queryErr := s.oauth.SubscriptionExpiresAt(ctx, token.AccessToken, token.ChatGPTAccountID, account.ProxyURL); queryErr == nil {
		account.SubscriptionExpiresAt = subscriptionExpiresAt
	}
	stored, err := s.store.UpsertAccount(ctx, account)
	if err != nil {
		return domain.Account{}, err
	}
	stored.ProxyURL = config.ProxyURL
	return stored, nil
}

func (s *Service) CompleteOpenAIReauthorize(ctx context.Context, userID, accountID, state, code string) (domain.Account, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return domain.Account{}, domain.ErrInvalidInput
	}
	flow, err := s.store.ConsumeOAuthFlow(ctx, s.security.HashToken(state), s.now())
	if err != nil {
		return domain.Account{}, err
	}
	if flow.UserID != userID {
		return domain.Account{}, domain.ErrForbidden
	}
	if flow.Purpose != "reauthorize" || flow.TargetAccountID == "" || flow.TargetAccountID != accountID {
		return domain.Account{}, domain.ErrConflict
	}
	account, err := s.store.AccountByID(ctx, flow.TargetAccountID)
	if err != nil {
		return domain.Account{}, err
	}
	if account.OwnerUserID != userID {
		return domain.Account{}, domain.ErrForbidden
	}
	token, err := s.oauth.Exchange(ctx, code, flow.CodeVerifier, flow.RedirectURI)
	if err != nil {
		return domain.Account{}, err
	}
	if token.AccessToken == "" || token.RefreshToken == "" || token.Email == "" || token.ChatGPTAccountID == "" {
		return domain.Account{}, errors.New("OpenAI OAuth response is missing required account fields")
	}
	if token.ChatGPTAccountID != account.ChatGPTAccountID {
		return domain.Account{}, domain.ErrConflict
	}
	scope := userID + ":" + account.ChatGPTAccountID
	access, err := s.security.Encrypt(token.AccessToken, []byte(scope+":access"))
	if err != nil {
		return domain.Account{}, err
	}
	refresh, err := s.security.Encrypt(token.RefreshToken, []byte(scope+":refresh"))
	if err != nil {
		return domain.Account{}, err
	}
	account.Email = normalizeEmail(token.Email)
	account.PlanType = token.PlanType
	account.AccessTokenCiphertext = access
	account.RefreshTokenCiphertext = refresh
	account.TokenExpiresAt = token.ExpiresAt
	account.Status = domain.StatusActive
	if err := s.hydrateAccountProxy(&account); err != nil {
		return domain.Account{}, err
	}
	if subscriptionExpiresAt, queryErr := s.oauth.SubscriptionExpiresAt(ctx, token.AccessToken, token.ChatGPTAccountID, account.ProxyURL); queryErr == nil {
		account.SubscriptionExpiresAt = subscriptionExpiresAt
	}
	event, err := s.newAuditEvent(userID, "account.reauthorized", "account", account.ID, map[string]string{"account_name": account.Name})
	if err != nil {
		return domain.Account{}, err
	}
	stored, err := s.store.UpdateAccountAuthorization(ctx, userID, account, event)
	if err != nil {
		return domain.Account{}, err
	}
	if err := s.hydrateAccountProxy(&stored); err != nil {
		return domain.Account{}, err
	}
	return stored, nil
}

func (s *Service) ListAccounts(ctx context.Context, userID string) ([]domain.Account, error) {
	accounts, err := s.store.ListAccounts(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if err := s.hydrateAccountProxy(&accounts[i]); err != nil {
			return nil, err
		}
	}
	return accounts, nil
}

func (s *Service) UpdateAccountConfig(ctx context.Context, userID, accountID string, config AccountConfigInput) (domain.Account, error) {
	config, err := normalizeAccountConfig(config)
	if err != nil {
		return domain.Account{}, err
	}
	account, err := s.store.AccountByID(ctx, accountID)
	if err != nil {
		return domain.Account{}, err
	}
	if account.OwnerUserID != userID {
		return domain.Account{}, domain.ErrForbidden
	}
	account.Name = config.Name
	account.Notes = config.Notes
	account.MaxConcurrency = config.MaxConcurrency
	account.RPMLimit = config.RPMLimit
	account.FastPolicy = config.FastPolicy
	account.Status = config.Status
	if err := s.setAccountProxy(&account, config.ProxyURL); err != nil {
		return domain.Account{}, err
	}
	stored, err := s.store.UpdateAccountConfig(ctx, userID, account)
	if err != nil {
		return domain.Account{}, err
	}
	stored.ProxyURL = config.ProxyURL
	return stored, nil
}

func (s *Service) Dashboard(ctx context.Context, userID, timezone string) (domain.Dashboard, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return domain.Dashboard{}, domain.ErrInvalidInput
	}
	now := s.now()
	localNow := now.In(location)
	todayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	currentHour := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), localNow.Hour(), 0, 0, 0, location)
	trendStart := currentHour.Add(-23 * time.Hour)
	return s.store.Dashboard(ctx, userID, todayStart, trendStart, now)
}
