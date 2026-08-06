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
	flow    OAuthFlow
	account domain.Account
	stored  domain.Account
}

func (s *accountSubscriptionStore) ConsumeOAuthFlow(context.Context, []byte, time.Time) (OAuthFlow, error) {
	return s.flow, nil
}

func (s *accountSubscriptionStore) UpsertAccount(_ context.Context, account domain.Account) (domain.Account, error) {
	s.stored = account
	return account, nil
}

func (s *accountSubscriptionStore) AccountByID(context.Context, string) (domain.Account, error) {
	return s.account, nil
}

func (s *accountSubscriptionStore) UpdateAccountAuthorization(_ context.Context, _ string, account domain.Account, _ domain.AuditEvent) (domain.Account, error) {
	s.stored = account
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
