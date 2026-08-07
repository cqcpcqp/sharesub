package application

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sharesub/sharesub/backend/internal/billing"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

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

func (s *Service) RecordGatewayMetric(ctx context.Context, access GatewayAccess, metric domain.GatewayMetric) error {
	metric.APIKeyID = access.Credential.APIKeyID
	metric.PlanID = access.Credential.Plan.ID
	metric.AccountID = access.Credential.Account.ID
	metric.MemberID = access.Credential.Member.ID
	metric.CostBreakdown = billing.AccountCostForImageSize(metric.BillingModel, metric.ServiceTier, metric.TokenUsage, metric.WebSearchCalls, metric.ImageSize)
	metric.AccountCostMicros = metric.CostBreakdown.TotalMicros
	metric.CreatedAt = s.now()
	return s.store.RecordGatewayMetric(ctx, metric)
}
