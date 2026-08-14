package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type accountSubscriptionStore struct {
	Store
	flow                       OAuthFlow
	account                    domain.Account
	stored                     domain.Account
	storedSubscriptionObserved bool
	createOrRotateResult       domain.Account
	updatedOwnerID             string
	updatedEvent               domain.AuditEvent
}

func (s *accountSubscriptionStore) ConsumeOAuthFlow(context.Context, []byte, time.Time) (OAuthFlow, error) {
	return s.flow, nil
}

func (s *accountSubscriptionStore) CreateOrRotateAccountAuthorization(_ context.Context, account domain.Account, subscriptionObserved bool) (domain.Account, error) {
	s.stored = account
	s.storedSubscriptionObserved = subscriptionObserved
	if s.createOrRotateResult.ID != "" {
		return s.createOrRotateResult, nil
	}
	return account, nil
}

func (s *accountSubscriptionStore) AccountByID(context.Context, string) (domain.Account, error) {
	return s.account, nil
}

func (s *accountSubscriptionStore) UpdateAccountAuthorization(_ context.Context, ownerID string, account domain.Account, event domain.AuditEvent) (domain.Account, error) {
	s.stored = account
	s.updatedOwnerID = ownerID
	s.updatedEvent = event
	return account, nil
}

type accountSubscriptionOAuth struct {
	token                 OAuthToken
	subscriptionExpiresAt time.Time
	subscriptionErr       error
	accountID             string
	proxyURL              string
}

func (o *accountSubscriptionOAuth) AuthorizationURL(string, string, string) string { return "" }
func (o *accountSubscriptionOAuth) Exchange(context.Context, string, string, string) (OAuthToken, error) {
	return o.token, nil
}
func (o *accountSubscriptionOAuth) Refresh(context.Context, string) (OAuthToken, error) {
	return o.token, nil
}
func (o *accountSubscriptionOAuth) SubscriptionExpiresAt(_ context.Context, _ string, accountID, proxyURL string) (*time.Time, error) {
	o.accountID = accountID
	o.proxyURL = proxyURL
	if o.subscriptionErr != nil {
		return nil, o.subscriptionErr
	}
	return &o.subscriptionExpiresAt, nil
}

func TestCompleteOpenAIConnectStoresSubscriptionExpiry(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	subscriptionExpiresAt := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	store := &accountSubscriptionStore{flow: OAuthFlow{UserID: "owner", Purpose: "connect"}}
	oauth := &accountSubscriptionOAuth{
		token: OAuthToken{
			AccessToken: "access", RefreshToken: "refresh", Email: "owner@example.com",
			ChatGPTAccountID: "chatgpt-account", PlanType: "pro", ExpiresAt: now.Add(time.Hour),
		},
		subscriptionExpiresAt: subscriptionExpiresAt,
	}
	service := &Service{store: store, security: testSecurityManager(t), oauth: oauth, now: func() time.Time { return now }}

	_, err := service.CompleteOpenAIConnect(context.Background(), "owner", "state", "code", AccountConfigInput{
		Name: "Pro 账号", ProxyURL: "socks5://proxy.example:1080", Status: domain.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.stored.SubscriptionExpiresAt == nil || !store.stored.SubscriptionExpiresAt.Equal(subscriptionExpiresAt) {
		t.Fatalf("stored subscription expiry = %v", store.stored.SubscriptionExpiresAt)
	}
	if oauth.accountID != "chatgpt-account" || oauth.proxyURL != "socks5://proxy.example:1080" {
		t.Fatalf("subscription query account = %q, proxy = %q", oauth.accountID, oauth.proxyURL)
	}
	if !store.storedSubscriptionObserved {
		t.Fatal("successful subscription query was not propagated to the store")
	}
}

func TestCompleteOpenAIReauthorizeUpdatesSubscriptionExpiry(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	subscriptionExpiresAt := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	proxyCiphertext, err := manager.Encrypt("https://proxy.example", []byte("owner:chatgpt-account:proxy"))
	if err != nil {
		t.Fatal(err)
	}
	store := &accountSubscriptionStore{
		flow: OAuthFlow{UserID: "owner", Purpose: "reauthorize", TargetAccountID: "account"},
		account: domain.Account{
			ID: "account", OwnerUserID: "owner", Name: "Pro 账号", ChatGPTAccountID: "chatgpt-account",
			ProxyURLCiphertext: proxyCiphertext,
		},
	}
	oauth := &accountSubscriptionOAuth{
		token: OAuthToken{
			AccessToken: "access", RefreshToken: "refresh", Email: "owner@example.com",
			ChatGPTAccountID: "chatgpt-account", PlanType: "pro", ExpiresAt: now.Add(time.Hour),
		},
		subscriptionExpiresAt: subscriptionExpiresAt,
	}
	service := &Service{store: store, security: manager, oauth: oauth, now: func() time.Time { return now }}

	_, err = service.CompleteOpenAIReauthorize(context.Background(), "owner", "account", "state", "code")
	if err != nil {
		t.Fatal(err)
	}
	if store.stored.SubscriptionExpiresAt == nil || !store.stored.SubscriptionExpiresAt.Equal(subscriptionExpiresAt) {
		t.Fatalf("stored subscription expiry = %v", store.stored.SubscriptionExpiresAt)
	}
	if oauth.proxyURL != "https://proxy.example" {
		t.Fatalf("subscription query proxy = %q", oauth.proxyURL)
	}
}

func TestAdminReauthorizeUsesOwnerEncryptionScopeAndAdminAuditActor(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	store := &accountSubscriptionStore{
		flow:    OAuthFlow{UserID: "admin", Purpose: "reauthorize", TargetAccountID: "account"},
		account: domain.Account{ID: "account", OwnerUserID: "owner", Name: "团队账号", ChatGPTAccountID: "chatgpt-account"},
	}
	oauth := &accountSubscriptionOAuth{token: OAuthToken{
		AccessToken: "admin-refreshed-access", RefreshToken: "admin-refreshed-refresh", Email: "owner@example.com",
		ChatGPTAccountID: "chatgpt-account", PlanType: "pro", ExpiresAt: now.Add(time.Hour),
	}, subscriptionExpiresAt: now.Add(30 * 24 * time.Hour)}
	service := &Service{store: store, security: manager, oauth: oauth, now: func() time.Time { return now }}
	admin := domain.User{ID: "admin", IsAdmin: true, Role: domain.RoleAdmin}

	if _, err := service.AdminCompleteOpenAIReauthorize(context.Background(), admin, "account", "state", "code"); err != nil {
		t.Fatal(err)
	}
	access, err := manager.Decrypt(store.stored.AccessTokenCiphertext, []byte("owner:chatgpt-account:access"))
	if err != nil || access != "admin-refreshed-access" {
		t.Fatalf("owner-scoped access = %q, error = %v", access, err)
	}
	if store.updatedOwnerID != "owner" || store.updatedEvent.ActorUserID != "admin" {
		t.Fatalf("update owner = %q, event = %+v", store.updatedOwnerID, store.updatedEvent)
	}
}

func TestCompleteOpenAIConnectPersistsAccountWhenSubscriptionQueryFails(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	store := &accountSubscriptionStore{flow: OAuthFlow{UserID: "owner", Purpose: "connect"}}
	oauth := &accountSubscriptionOAuth{
		token: OAuthToken{
			AccessToken: "access", RefreshToken: "refresh", Email: "owner@example.com",
			ChatGPTAccountID: "chatgpt-account", PlanType: "pro", ExpiresAt: now.Add(time.Hour),
		},
		subscriptionErr: errors.New("subscription endpoint unavailable"),
	}
	service := &Service{store: store, security: testSecurityManager(t), oauth: oauth, now: func() time.Time { return now }}

	_, err := service.CompleteOpenAIConnect(context.Background(), "owner", "state", "code", AccountConfigInput{
		Name: "Pro 账号", Status: domain.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.stored.AccessTokenCiphertext == nil || store.stored.RefreshTokenCiphertext == nil {
		t.Fatal("OAuth credentials were not persisted")
	}
	if store.stored.SubscriptionExpiresAt != nil {
		t.Fatalf("stored subscription expiry = %v, want nil", store.stored.SubscriptionExpiresAt)
	}
	if store.storedSubscriptionObserved {
		t.Fatal("failed subscription query was marked as observed")
	}
}

func TestCompleteOpenAIConnectReturnsRotatedAccountConfiguration(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	proxyCiphertext, err := manager.Encrypt("https://preserved-proxy.example", []byte("owner:chatgpt-account:proxy"))
	if err != nil {
		t.Fatal(err)
	}
	store := &accountSubscriptionStore{
		flow: OAuthFlow{UserID: "owner", Purpose: "connect"},
		createOrRotateResult: domain.Account{
			ID: "existing-account", OwnerUserID: "owner", Name: "保留名称", Notes: "保留备注",
			ChatGPTAccountID: "chatgpt-account", ProxyURLCiphertext: proxyCiphertext,
			MaxConcurrency: 7, RPMLimit: 88, FastPolicy: []domain.FastPolicyRule{{Action: "fast"}},
		},
	}
	oauth := &accountSubscriptionOAuth{token: OAuthToken{
		AccessToken: "new-access", RefreshToken: "new-refresh", Email: "new@example.com",
		ChatGPTAccountID: "chatgpt-account", PlanType: "pro", ExpiresAt: now.Add(time.Hour),
	}}
	service := &Service{store: store, security: manager, oauth: oauth, now: func() time.Time { return now }}

	got, err := service.CompleteOpenAIConnect(context.Background(), "owner", "state", "code", AccountConfigInput{
		Name: "不得覆盖名称", Notes: "不得覆盖备注", ProxyURL: "https://new-proxy.example",
		MaxConcurrency: 1, RPMLimit: 2, Status: domain.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "existing-account" || got.Name != "保留名称" || got.Notes != "保留备注" || got.ProxyURL != "https://preserved-proxy.example" || got.MaxConcurrency != 7 || got.RPMLimit != 88 || len(got.FastPolicy) != 1 {
		t.Fatalf("rotated account configuration = %+v", got)
	}
}

func TestCompleteOpenAIReauthorizePersistsTokensAndKeepsSubscriptionExpiryWhenQueryFails(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	existingSubscriptionExpiresAt := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	store := &accountSubscriptionStore{
		flow: OAuthFlow{UserID: "owner", Purpose: "reauthorize", TargetAccountID: "account"},
		account: domain.Account{
			ID: "account", OwnerUserID: "owner", Name: "Pro 账号", ChatGPTAccountID: "chatgpt-account",
			SubscriptionExpiresAt: &existingSubscriptionExpiresAt,
		},
	}
	oauth := &accountSubscriptionOAuth{
		token: OAuthToken{
			AccessToken: "new-access", RefreshToken: "new-refresh", Email: "owner@example.com",
			ChatGPTAccountID: "chatgpt-account", PlanType: "pro", ExpiresAt: now.Add(time.Hour),
		},
		subscriptionErr: errors.New("subscription endpoint unavailable"),
	}
	service := &Service{store: store, security: testSecurityManager(t), oauth: oauth, now: func() time.Time { return now }}

	_, err := service.CompleteOpenAIReauthorize(context.Background(), "owner", "account", "state", "code")
	if err != nil {
		t.Fatal(err)
	}
	if store.stored.AccessTokenCiphertext == nil || store.stored.RefreshTokenCiphertext == nil {
		t.Fatal("refreshed OAuth credentials were not persisted")
	}
	if store.stored.SubscriptionExpiresAt == nil || !store.stored.SubscriptionExpiresAt.Equal(existingSubscriptionExpiresAt) {
		t.Fatalf("stored subscription expiry = %v, want %v", store.stored.SubscriptionExpiresAt, existingSubscriptionExpiresAt)
	}
}
