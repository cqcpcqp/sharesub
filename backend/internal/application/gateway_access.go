package application

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/sharesub/sharesub/backend/internal/billing"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

func (s *Service) CreateAPIKey(ctx context.Context, userID, name, strategy string, routes []domain.APIKeyRoute, fastPolicy []domain.FastPolicyRule) (CreatedAPIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || !validStrategy(strategy) || !validRoutes(routes) {
		return CreatedAPIKey{}, domain.ErrInvalidInput
	}
	fastPolicy, err := normalizeFastPolicy(fastPolicy, false)
	if err != nil {
		return CreatedAPIKey{}, err
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
	key := domain.APIKey{ID: id, UserID: userID, Name: name, Key: plain, KeyAvailable: true, KeyPrefix: prefix, KeyHash: s.security.HashToken(plain), KeyCiphertext: ciphertext, Strategy: strategy, FastPolicy: fastPolicy, Status: domain.StatusActive, CreatedAt: s.now(), Routes: routes}
	if err := s.store.CreateAPIKey(ctx, key, routes); err != nil {
		return CreatedAPIKey{}, err
	}
	return CreatedAPIKey{APIKey: key, Key: plain}, nil
}

func (s *Service) UpdateAPIKey(ctx context.Context, userID, keyID, name, strategy string, routes []domain.APIKeyRoute, fastPolicy []domain.FastPolicyRule) (domain.APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || !validStrategy(strategy) || !validRoutes(routes) {
		return domain.APIKey{}, domain.ErrInvalidInput
	}
	fastPolicy, err := normalizeFastPolicy(fastPolicy, false)
	if err != nil {
		return domain.APIKey{}, err
	}
	key, err := s.store.UpdateAPIKey(ctx, userID, domain.APIKey{ID: keyID, Name: name, Strategy: strategy, FastPolicy: fastPolicy}, routes)
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

// ResolveGatewayAPIKeyID returns the stable identifier used for per-key
// process-local resource limits without retaining the bearer secret in memory.
func (s *Service) ResolveGatewayAPIKeyID(ctx context.Context, apiKey string) (string, error) {
	if !strings.HasPrefix(apiKey, "sk-sharesub-") {
		return "", domain.ErrUnauthorized
	}
	routes, err := s.store.ResolveGatewayRoutes(ctx, s.security.HashToken(apiKey), s.now())
	if err != nil || routes.APIKey.ID == "" {
		return "", domain.ErrUnauthorized
	}
	return routes.APIKey.ID, nil
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
		exhausted, err := s.gatewayCredentialQuotaExhausted(ctx, credential)
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
		var commit func()
		var release func()
		if s.traffic != nil {
			commit, release, err = s.traffic.prepare(credential.Account.ID, credential.Account.MaxConcurrency, credential.Account.RPMLimit, s.now())
			if err != nil {
				limitErr = err
				continue
			}
		}
		access, err := s.resolveCredential(ctx, credential)
		if err != nil {
			if release != nil {
				release()
			}
			accountErr = err
			continue
		}
		if commit != nil {
			commit()
		}
		access.Release = release
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

// ReacquireGatewayAccess validates and reacquires the account-bound access for
// a subsequent turn of a persistent gateway session. Unlike
// ResolveGatewayAccess, it never selects another route: a Responses WebSocket
// continuation must remain on the API key, Plan, member, account, and account
// binding generation selected for its first turn.
func (s *Service) ReacquireGatewayAccess(ctx context.Context, apiKey string, pinned GatewayAccess) (GatewayAccess, error) {
	if !strings.HasPrefix(apiKey, "sk-sharesub-") {
		return GatewayAccess{}, domain.ErrUnauthorized
	}
	routes, err := s.store.ResolveGatewayRoutes(ctx, s.security.HashToken(apiKey), s.now())
	if err != nil || routes.APIKey.ID != pinned.Credential.APIKeyID {
		return GatewayAccess{}, domain.ErrUnauthorized
	}

	var credential domain.GatewayCredential
	found := false
	for _, candidate := range routes.Candidates {
		if sameGatewayAccessBinding(candidate, pinned.Credential) {
			credential = candidate
			found = true
			break
		}
	}
	if !found {
		return GatewayAccess{}, domain.ErrAccountUnavailable
	}

	exhausted, err := s.gatewayCredentialQuotaExhausted(ctx, credential)
	if err != nil {
		return GatewayAccess{}, err
	}
	if exhausted {
		return GatewayAccess{}, domain.ErrQuotaExhausted
	}

	var commit func()
	var release func()
	if s.traffic != nil {
		commit, release, err = s.traffic.prepare(credential.Account.ID, credential.Account.MaxConcurrency, credential.Account.RPMLimit, s.now())
		if err != nil {
			return GatewayAccess{}, err
		}
	}
	access, err := s.resolveCredential(ctx, credential)
	if err != nil {
		if release != nil {
			release()
		}
		return GatewayAccess{}, domain.ErrAccountUnavailable
	}
	if commit != nil {
		commit()
	}
	access.Release = release
	return access, nil
}

func sameGatewayAccessBinding(candidate, pinned domain.GatewayCredential) bool {
	return candidate.APIKeyID == pinned.APIKeyID &&
		candidate.Plan.ID == pinned.Plan.ID &&
		candidate.Member.ID == pinned.Member.ID &&
		candidate.Account.ID == pinned.Account.ID &&
		candidate.AccountBindingGeneration == pinned.AccountBindingGeneration
}

func (s *Service) gatewayCredentialQuotaExhausted(ctx context.Context, credential domain.GatewayCredential) (bool, error) {
	exhausted, err := s.store.AccountQuotaExhausted(ctx, credential.Account.ID, s.now())
	if err != nil || exhausted || credential.Plan.AllocationMode == domain.AllocationShared {
		return exhausted, err
	}
	if credential.Member.ShareBasisPoints == 0 {
		return true, nil
	}
	return s.store.MemberQuotaExhausted(
		ctx,
		credential.Member.ID,
		credential.Plan.ID,
		credential.Account.ID,
		credential.AccountBindingGeneration,
		credential.Member.ShareBasisPoints,
		s.now(),
	)
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
	accessToken, _, err = s.refreshAccountToken(ctx, domain.Account{
		ID: credential.Account.ID, OwnerUserID: credential.Account.OwnerUserID,
		ChatGPTAccountID: credential.Account.ChatGPTAccountID, Status: credential.Account.Status,
		AccessTokenCiphertext:  credential.AccessTokenCiphertext,
		RefreshTokenCiphertext: credential.RefreshTokenCiphertext,
		TokenExpiresAt:         credential.TokenExpiresAt,
	}, 2*time.Minute, true)
	if err != nil {
		return GatewayAccess{}, domain.ErrAccountUnavailable
	}
	if err := s.store.TouchAPIKey(ctx, credential.APIKeyID, s.now()); err != nil {
		return GatewayAccess{}, err
	}
	return GatewayAccess{Credential: credential, AccessToken: accessToken, ProxyURL: proxyURL}, nil
}

func (s *Service) RecordGatewayUsage(ctx context.Context, access GatewayAccess, headers http.Header, observedAt time.Time) error {
	signals := ParseCodexQuotaHeaders(headers, observedAt)
	if len(signals) == 0 {
		return errors.New("Codex response did not contain a recognized 5h or 7d quota signal")
	}
	return s.store.RecordAccountQuotaSignals(
		ctx,
		access.Credential.Plan.ID,
		access.Credential.Account.ID,
		access.Credential.AccountBindingGeneration,
		signals,
		observedAt,
	)
}

// RecordGatewayAccountQuota records an observed account quota snapshot.
func (s *Service) RecordGatewayAccountQuota(ctx context.Context, access GatewayAccess, headers http.Header, observedAt time.Time) error {
	signals := ParseCodexQuotaHeaders(headers, observedAt)
	if len(signals) == 0 {
		return errors.New("Codex response did not contain a recognized 5h or 7d quota signal")
	}
	return s.store.RecordAccountQuotaSignals(
		ctx,
		access.Credential.Plan.ID,
		access.Credential.Account.ID,
		access.Credential.AccountBindingGeneration,
		signals,
		observedAt,
	)
}

func (s *Service) MarkGatewayAccountRefreshRequired(ctx context.Context, access GatewayAccess, message string) error {
	updated, err := s.store.MarkAccountErrorIfRefreshTokenUnchanged(
		ctx, access.Credential.Account.ID, access.Credential.RefreshTokenCiphertext, message,
	)
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("account credentials changed before refresh-required update")
	}
	return nil
}

func (s *Service) RefreshGatewayAccountAccess(ctx context.Context, access GatewayAccess) (string, error) {
	if s == nil || s.oauth == nil {
		return "", errors.New("OpenAI OAuth refresh client is unavailable")
	}
	refreshBefore := access.Credential.TokenExpiresAt.Sub(s.now()) + time.Minute
	if refreshBefore < time.Minute {
		refreshBefore = time.Minute
	}
	token, _, err := s.refreshAccountToken(ctx, domain.Account{
		ID: access.Credential.Account.ID, OwnerUserID: access.Credential.Account.OwnerUserID,
		ChatGPTAccountID: access.Credential.Account.ChatGPTAccountID, Status: access.Credential.Account.Status,
		AccessTokenCiphertext:  access.Credential.AccessTokenCiphertext,
		RefreshTokenCiphertext: access.Credential.RefreshTokenCiphertext,
		TokenExpiresAt:         access.Credential.TokenExpiresAt,
	}, refreshBefore, true)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Service) RecordGatewayMetric(ctx context.Context, access GatewayAccess, metric domain.GatewayMetric, recordedAt time.Time) error {
	metric.APIKeyID = access.Credential.APIKeyID
	metric.PlanID = access.Credential.Plan.ID
	metric.AccountID = access.Credential.Account.ID
	metric.MemberID = access.Credential.Member.ID
	metric.AccountBindingGeneration = access.Credential.AccountBindingGeneration
	metric.Endpoint = truncateGatewayMetricText(metric.Endpoint, 160)
	metric.ErrorCode = truncateGatewayMetricText(metric.ErrorCode, 120)
	metric.ErrorMessage = truncateGatewayErrorMessage(metric.ErrorMessage, 2000)
	if len(metric.BillingSegments) > 0 {
		metric.CostBreakdown = billing.AccountCostForSegments(metric.BillingModel, metric.ServiceTier, metric.BillingSegments)
	} else {
		metric.CostBreakdown = billing.AccountCostForImageSize(metric.BillingModel, metric.ServiceTier, metric.TokenUsage, metric.WebSearchCalls, metric.ImageSize)
	}
	metric.AccountCostMicros = metric.CostBreakdown.TotalMicros
	metric.CreatedAt = recordedAt
	return s.store.RecordGatewayMetric(ctx, metric)
}

func truncateGatewayMetricText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func truncateGatewayErrorMessage(value string, limit int) string {
	value = strings.Map(func(char rune) rune {
		if unicode.IsControl(char) && char != '\n' && char != '\t' {
			return -1
		}
		return char
	}, value)
	return truncateGatewayMetricText(value, limit)
}
