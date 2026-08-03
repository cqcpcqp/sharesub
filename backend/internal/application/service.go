package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"

	"github.com/sharesub/sharesub/backend/internal/billing"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

const oauthFlowTTL = 15 * time.Minute
const MaxAvatarBytes = 2 << 20

type Service struct {
	store       Store
	security    *security.Manager
	oauth       OpenAIOAuth
	sessionTTL  time.Duration
	redirectURI string
	publicURL   string
	now         func() time.Time
	traffic     *accountTrafficController
}

type AuthResult struct {
	User  domain.User `json:"user"`
	Token string      `json:"token"`
}

type OAuthStart struct {
	AuthorizationURL string `json:"authorization_url"`
	FlowID           string `json:"flow_id"`
}

type CreatedInvite struct {
	Invite    domain.Invite `json:"invite"`
	InviteURL string        `json:"invite_url"`
}

type CreatedAPIKey struct {
	APIKey domain.APIKey `json:"api_key"`
	Key    string        `json:"key"`
}

type AccountConfigInput struct {
	Name           string `json:"name"`
	Notes          string `json:"notes"`
	ProxyURL       string `json:"proxy_url"`
	MaxConcurrency int    `json:"max_concurrency"`
	RPMLimit       int    `json:"rpm_limit"`
	Status         string `json:"status"`
}

func NewService(store Store, securityManager *security.Manager, oauth OpenAIOAuth, sessionTTL time.Duration, redirectURI, publicURL string) *Service {
	return &Service{store: store, security: securityManager, oauth: oauth, sessionTTL: sessionTTL, redirectURI: redirectURI, publicURL: strings.TrimRight(publicURL, "/"), now: time.Now, traffic: newAccountTrafficController()}
}

func (s *Service) Register(ctx context.Context, username, email, password string) (AuthResult, error) {
	email = normalizeEmail(email)
	username = strings.TrimSpace(username)
	if !validEmail(email) || !validUsername(username) {
		return AuthResult{}, fmt.Errorf("%w: invalid email", domain.ErrInvalidInput)
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return AuthResult{}, fmt.Errorf("%w: %v", domain.ErrInvalidInput, err)
	}
	id, err := security.NewID()
	if err != nil {
		return AuthResult{}, err
	}
	user := domain.User{ID: id, Username: username, Email: email, PasswordHash: hash, Status: domain.StatusActive, CreatedAt: s.now()}
	if err := s.store.CreateUser(ctx, user); err != nil {
		return AuthResult{}, err
	}
	return s.newSession(ctx, user)
}

func (s *Service) UpdateUsername(ctx context.Context, userID, username string) (domain.User, error) {
	username = strings.TrimSpace(username)
	if !validUsername(username) {
		return domain.User{}, domain.ErrInvalidInput
	}
	return s.store.UpdateUsername(ctx, userID, username)
}

func (s *Service) UpdateUserAvatar(ctx context.Context, userID string, data []byte) (domain.User, error) {
	if len(data) == 0 || len(data) > MaxAvatarBytes {
		return domain.User{}, domain.ErrInvalidInput
	}
	mediaType := http.DetectContentType(data)
	if mediaType != "image/jpeg" && mediaType != "image/png" && mediaType != "image/webp" {
		return domain.User{}, domain.ErrInvalidInput
	}
	return s.store.UpdateUserAvatar(ctx, userID, domain.UserAvatar{Data: data, MediaType: mediaType}, s.now())
}

func (s *Service) DeleteUserAvatar(ctx context.Context, userID string) (domain.User, error) {
	return s.store.DeleteUserAvatar(ctx, userID)
}

func (s *Service) UserAvatar(ctx context.Context, userID string) (domain.UserAvatar, error) {
	if strings.TrimSpace(userID) == "" {
		return domain.UserAvatar{}, domain.ErrInvalidInput
	}
	return s.store.UserAvatar(ctx, userID)
}

func (s *Service) Login(ctx context.Context, email, password string) (AuthResult, error) {
	user, err := s.store.UserByEmail(ctx, normalizeEmail(email))
	if err != nil || user.Status != domain.StatusActive || !security.CheckPassword(user.PasswordHash, password) {
		return AuthResult{}, domain.ErrUnauthorized
	}
	return s.newSession(ctx, user)
}

func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if !strings.HasPrefix(token, "ss_session_") {
		return domain.User{}, domain.ErrUnauthorized
	}
	return s.store.UserBySessionHash(ctx, s.security.HashToken(token), s.now())
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
		MaxConcurrency: config.MaxConcurrency, RPMLimit: config.RPMLimit,
		TokenExpiresAt: token.ExpiresAt, Status: config.Status, CreatedAt: s.now(),
	}
	if err := s.setAccountProxy(&account, config.ProxyURL); err != nil {
		return domain.Account{}, err
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

func (s *Service) CreatePlan(ctx context.Context, userID, accountID, name, allocationMode string, ownerShareBPS int) (domain.PlanDetail, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || !validAllocationMode(allocationMode) {
		return domain.PlanDetail{}, domain.ErrInvalidInput
	}
	if allocationMode == domain.AllocationFixed && (ownerShareBPS < 1 || ownerShareBPS > domain.MaxShareBPS) {
		return domain.PlanDetail{}, domain.ErrInvalidInput
	}
	if allocationMode == domain.AllocationShared && ownerShareBPS != 0 {
		return domain.PlanDetail{}, domain.ErrInvalidInput
	}
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
	owner := domain.Member{ID: memberID, PlanID: planID, UserID: userID, Email: account.Email, Role: domain.RoleOwner, Status: domain.StatusActive, ShareBasisPoints: ownerShareBPS, CreatedAt: createdAt}
	event, err := s.newAuditEvent(userID, "plan.created", "plan", planID, map[string]any{"name": name, "account_id": accountID})
	if err != nil {
		return domain.PlanDetail{}, err
	}
	if err := s.store.CreatePlan(ctx, plan, owner, event); err != nil {
		return domain.PlanDetail{}, err
	}
	return s.PlanDetail(ctx, userID, planID)
}

func (s *Service) ListPlans(ctx context.Context, userID string) ([]domain.Plan, error) {
	return s.store.ListPlans(ctx, userID)
}

func (s *Service) PlanDetail(ctx context.Context, userID, planID string) (domain.PlanDetail, error) {
	detail, err := s.store.PlanDetail(ctx, planID, userID)
	if err != nil {
		return detail, err
	}
	if err := s.hydrateAccountProxy(&detail.Account); err != nil {
		return detail, err
	}
	return detail, nil
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

func (s *Service) CreateAPIKey(ctx context.Context, userID, name, strategy string, routes []domain.APIKeyRoute) (CreatedAPIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || !validStrategy(strategy) || !validRoutes(routes) {
		return CreatedAPIKey{}, domain.ErrInvalidInput
	}
	plain, err := security.NewOpaqueToken("sk-sharesub-")
	if err != nil {
		return CreatedAPIKey{}, err
	}
	id, err := security.NewID()
	if err != nil {
		return CreatedAPIKey{}, err
	}
	ciphertext, err := s.security.Encrypt(plain, apiKeySecretAssociatedData(userID, id))
	if err != nil {
		return CreatedAPIKey{}, err
	}
	prefix := plain
	if len(prefix) > 20 {
		prefix = prefix[:20]
	}
	key := domain.APIKey{ID: id, UserID: userID, Name: name, Key: plain, KeyAvailable: true, KeyPrefix: prefix, KeyHash: s.security.HashToken(plain), KeyCiphertext: ciphertext, Strategy: strategy, Status: domain.StatusActive, CreatedAt: s.now(), Routes: routes}
	if err := s.store.CreateAPIKey(ctx, key, routes); err != nil {
		return CreatedAPIKey{}, err
	}
	return CreatedAPIKey{APIKey: key, Key: plain}, nil
}

func (s *Service) UpdateAPIKey(ctx context.Context, userID, keyID, name, strategy string, routes []domain.APIKeyRoute) (domain.APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || !validStrategy(strategy) || !validRoutes(routes) {
		return domain.APIKey{}, domain.ErrInvalidInput
	}
	key, err := s.store.UpdateAPIKey(ctx, userID, domain.APIKey{ID: keyID, Name: name, Strategy: strategy}, routes)
	if err != nil {
		return domain.APIKey{}, err
	}
	return s.hydrateAPIKeySecret(key)
}

func (s *Service) ListAPIKeys(ctx context.Context, userID string) ([]domain.APIKey, error) {
	keys, err := s.store.ListAPIKeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	for index := range keys {
		keys[index], err = s.hydrateAPIKeySecret(keys[index])
		if err != nil {
			return nil, err
		}
	}
	return keys, nil
}

func (s *Service) hydrateAPIKeySecret(key domain.APIKey) (domain.APIKey, error) {
	if len(key.KeyCiphertext) == 0 {
		key.Key = ""
		key.KeyAvailable = false
		return key, nil
	}
	plain, err := s.security.Decrypt(key.KeyCiphertext, apiKeySecretAssociatedData(key.UserID, key.ID))
	if err != nil {
		return domain.APIKey{}, fmt.Errorf("decrypt API key: %w", err)
	}
	key.Key = plain
	key.KeyAvailable = true
	return key, nil
}

func apiKeySecretAssociatedData(userID, keyID string) []byte {
	return []byte("api-key:" + userID + ":" + keyID)
}

func (s *Service) RevokeAPIKey(ctx context.Context, userID, keyID string) error {
	return s.store.RevokeAPIKey(ctx, userID, keyID)
}

type GatewayAccess struct {
	Credential  domain.GatewayCredential
	AccessToken string
	ProxyURL    string
	Release     func()
}

func (s *Service) AuthenticateGatewayKey(ctx context.Context, apiKey string) error {
	if !strings.HasPrefix(apiKey, "sk-sharesub-") {
		return domain.ErrUnauthorized
	}
	routes, err := s.store.ResolveGatewayRoutes(ctx, s.security.HashToken(apiKey), s.now())
	if err != nil {
		return domain.ErrUnauthorized
	}
	if len(routes.Candidates) == 0 {
		return domain.ErrNoRouteAvailable
	}
	return nil
}

func (s *Service) ResolveGatewayAccess(ctx context.Context, apiKey string, excludedAccountIDs ...string) (GatewayAccess, error) {
	if !strings.HasPrefix(apiKey, "sk-sharesub-") {
		return GatewayAccess{}, domain.ErrUnauthorized
	}
	routes, err := s.store.ResolveGatewayRoutes(ctx, s.security.HashToken(apiKey), s.now())
	if err != nil {
		return GatewayAccess{}, domain.ErrUnauthorized
	}
	excluded := make(map[string]struct{}, len(excludedAccountIDs))
	for _, accountID := range excludedAccountIDs {
		excluded[accountID] = struct{}{}
	}
	available := make([]domain.GatewayCredential, 0, len(routes.Candidates))
	eligibleCount := 0
	for _, credential := range routes.Candidates {
		if _, skip := excluded[credential.Account.ID]; skip {
			continue
		}
		eligibleCount++
		exhausted, err := s.store.AccountQuotaExhausted(ctx, credential.Account.ID, s.now())
		if err != nil {
			return GatewayAccess{}, err
		}
		if !exhausted && credential.Plan.AllocationMode != domain.AllocationShared {
			exhausted, err = s.store.MemberQuotaExhausted(ctx, credential.Member.ID, credential.Account.ID, credential.Member.ShareBasisPoints, s.now())
		}
		if err != nil {
			return GatewayAccess{}, err
		}
		if !exhausted {
			available = append(available, credential)
		}
	}
	if len(routes.Candidates) == 0 {
		return GatewayAccess{}, domain.ErrNoRouteAvailable
	}
	if eligibleCount == 0 {
		return GatewayAccess{}, domain.ErrNoRouteAvailable
	}
	if len(available) == 0 {
		return GatewayAccess{}, domain.ErrQuotaExhausted
	}
	if routes.APIKey.Strategy == domain.RouteBalanced {
		sort.SliceStable(available, func(i, j int) bool {
			leftUsage, leftCapacity := credentialQuotaLoad(available[i])
			rightUsage, rightCapacity := credentialQuotaLoad(available[j])
			left := leftUsage * rightCapacity
			right := rightUsage * leftCapacity
			if left == right {
				return available[i].RoutePriority < available[j].RoutePriority
			}
			return left < right
		})
	}
	var accountErr, limitErr error
	for _, credential := range available {
		access, err := s.resolveCredential(ctx, credential)
		if err != nil {
			accountErr = err
			continue
		}
		if s.traffic != nil {
			release, err := s.traffic.acquire(credential.Account.ID, credential.Account.MaxConcurrency, credential.Account.RPMLimit, s.now())
			if err != nil {
				limitErr = err
				continue
			}
			access.Release = release
		}
		return access, nil
	}
	if limitErr != nil {
		return GatewayAccess{}, limitErr
	}
	if accountErr != nil {
		return GatewayAccess{}, domain.ErrAccountUnavailable
	}
	return GatewayAccess{}, domain.ErrNoRouteAvailable
}

func (s *Service) resolveCredential(ctx context.Context, credential domain.GatewayCredential) (GatewayAccess, error) {
	scope := credential.Account.OwnerUserID + ":" + credential.Account.ChatGPTAccountID
	accessToken, err := s.security.Decrypt(credential.AccessTokenCiphertext, []byte(scope+":access"))
	if err != nil {
		return GatewayAccess{}, err
	}
	proxyURL := ""
	if len(credential.ProxyURLCiphertext) > 0 {
		proxyURL, err = s.security.Decrypt(credential.ProxyURLCiphertext, []byte(scope+":proxy"))
		if err != nil {
			return GatewayAccess{}, err
		}
	}
	if credential.TokenExpiresAt.After(s.now().Add(2 * time.Minute)) {
		if err := s.store.TouchAPIKey(ctx, credential.APIKeyID, s.now()); err != nil {
			return GatewayAccess{}, err
		}
		return GatewayAccess{Credential: credential, AccessToken: accessToken, ProxyURL: proxyURL}, nil
	}
	refreshToken, err := s.security.Decrypt(credential.RefreshTokenCiphertext, []byte(scope+":refresh"))
	if err != nil {
		return GatewayAccess{}, err
	}
	refreshed, err := s.oauth.Refresh(ctx, refreshToken)
	if err != nil {
		_ = s.store.MarkAccountError(ctx, credential.Account.ID, err.Error())
		return GatewayAccess{}, domain.ErrAccountUnavailable
	}
	newAccess, err := s.security.Encrypt(refreshed.AccessToken, []byte(scope+":access"))
	if err != nil {
		return GatewayAccess{}, err
	}
	newRefresh, err := s.security.Encrypt(refreshed.RefreshToken, []byte(scope+":refresh"))
	if err != nil {
		return GatewayAccess{}, err
	}
	if err := s.store.UpdateAccountTokens(ctx, credential.Account.ID, newAccess, newRefresh, refreshed.ExpiresAt); err != nil {
		return GatewayAccess{}, err
	}
	if err := s.store.TouchAPIKey(ctx, credential.APIKeyID, s.now()); err != nil {
		return GatewayAccess{}, err
	}
	return GatewayAccess{Credential: credential, AccessToken: refreshed.AccessToken, ProxyURL: proxyURL}, nil
}

func (s *Service) RecordGatewayUsage(ctx context.Context, access GatewayAccess, headers http.Header, requestID string) error {
	signals := ParseCodexQuotaHeaders(headers, s.now())
	if len(signals) == 0 {
		return errors.New("Codex response did not contain complete 5h or 7d quota signals")
	}
	return s.store.RecordQuotaSignals(ctx, access.Credential.Account.ID, access.Credential.Member.ID, signals, requestID, s.now())
}

// RecordGatewayAccountQuota records an observed account limit without
// attributing quota delta to the member whose rejected request exposed it.
func (s *Service) RecordGatewayAccountQuota(ctx context.Context, access GatewayAccess, headers http.Header) error {
	signals := ParseCodexQuotaHeaders(headers, s.now())
	if len(signals) == 0 {
		return errors.New("Codex response did not contain complete 5h or 7d quota signals")
	}
	return s.store.RecordAccountQuotaSignals(ctx, access.Credential.Account.ID, signals, s.now())
}

func (s *Service) RecordGatewayMetric(ctx context.Context, access GatewayAccess, requestID, model, serviceTier string, statusCode int, ttft, duration time.Duration, tokenUsage domain.TokenUsage) error {
	return s.store.RecordGatewayMetric(ctx, domain.GatewayMetric{
		RequestID: requestID, APIKeyID: access.Credential.APIKeyID, PlanID: access.Credential.Plan.ID,
		AccountID: access.Credential.Account.ID, MemberID: access.Credential.Member.ID,
		Model: model, ServiceTier: serviceTier, StatusCode: statusCode, TTFT: ttft, Duration: duration,
		TokenUsage: tokenUsage, AccountCostMicros: billing.AccountCostMicros(model, serviceTier, tokenUsage), CreatedAt: s.now(),
	})
}

func (s *Service) newAuditEvent(actorUserID, action, resourceType, resourceID string, metadata any) (domain.AuditEvent, error) {
	id, err := security.NewID()
	if err != nil {
		return domain.AuditEvent{}, err
	}
	body := []byte("{}")
	if metadata != nil {
		body, err = json.Marshal(metadata)
		if err != nil {
			return domain.AuditEvent{}, err
		}
	}
	return domain.AuditEvent{ID: id, ActorUserID: actorUserID, Action: action, ResourceType: resourceType, ResourceID: resourceID, Metadata: body, CreatedAt: s.now()}, nil
}

func normalizeEmail(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func normalizeAccountConfig(config AccountConfigInput) (AccountConfigInput, error) {
	config.Name = strings.TrimSpace(config.Name)
	config.Notes = strings.TrimSpace(config.Notes)
	config.ProxyURL = strings.TrimSpace(config.ProxyURL)
	if utf8.RuneCountInString(config.Name) < 1 || utf8.RuneCountInString(config.Name) > 100 || utf8.RuneCountInString(config.Notes) > 2000 {
		return AccountConfigInput{}, domain.ErrInvalidInput
	}
	if config.MaxConcurrency < 0 || config.MaxConcurrency > 100 || config.RPMLimit < 0 || config.RPMLimit > 10_000 {
		return AccountConfigInput{}, domain.ErrInvalidInput
	}
	if config.Status != domain.StatusActive && config.Status != domain.StatusDisabled && config.Status != domain.StatusRefreshRequired {
		return AccountConfigInput{}, domain.ErrInvalidInput
	}
	if config.ProxyURL != "" {
		parsed, err := url.Parse(config.ProxyURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "socks5") {
			return AccountConfigInput{}, domain.ErrInvalidInput
		}
	}
	return config, nil
}

func (s *Service) setAccountProxy(account *domain.Account, proxyURL string) error {
	account.ProxyURL = proxyURL
	account.ProxyURLCiphertext = nil
	if proxyURL == "" {
		return nil
	}
	ciphertext, err := s.security.Encrypt(proxyURL, []byte(account.OwnerUserID+":"+account.ChatGPTAccountID+":proxy"))
	if err != nil {
		return err
	}
	account.ProxyURLCiphertext = ciphertext
	return nil
}

func (s *Service) hydrateAccountProxy(account *domain.Account) error {
	if len(account.ProxyURLCiphertext) == 0 {
		account.ProxyURL = ""
		return nil
	}
	proxyURL, err := s.security.Decrypt(account.ProxyURLCiphertext, []byte(account.OwnerUserID+":"+account.ChatGPTAccountID+":proxy"))
	if err != nil {
		return err
	}
	account.ProxyURL = proxyURL
	return nil
}

func validAllocationMode(value string) bool {
	return value == domain.AllocationFixed || value == domain.AllocationShared
}

func credentialQuotaLoad(credential domain.GatewayCredential) (int64, int64) {
	if credential.Plan.AllocationMode == domain.AllocationShared {
		return credential.AccountUsageMicros, domain.MaxShareBPS
	}
	return credential.UsageMicros, int64(credential.Member.ShareBasisPoints)
}

func validEmail(value string) bool {
	at := strings.LastIndexByte(value, '@')
	return at > 0 && at < len(value)-1 && strings.Contains(value[at+1:], ".") && len(value) <= 254
}

func validUsername(value string) bool {
	length := utf8.RuneCountInString(value)
	if length < 2 || length > 32 {
		return false
	}
	for _, char := range value {
		if !unicode.IsLetter(char) && !unicode.IsNumber(char) && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func validStrategy(value string) bool {
	return value == domain.RoutePriority || value == domain.RouteBalanced
}

func validRoutes(routes []domain.APIKeyRoute) bool {
	if len(routes) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		if route.PlanID == "" || route.Priority < 1 || route.Priority > 10_000 {
			return false
		}
		if _, exists := seen[route.PlanID]; exists {
			return false
		}
		seen[route.PlanID] = struct{}{}
	}
	return true
}
