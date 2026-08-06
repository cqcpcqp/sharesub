package application

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

type tokenRefreshStore struct {
	Store
	mu         sync.Mutex
	account    domain.Account
	lease      bool
	candidates []domain.Account
	updates    int
	marks      int
	updateErr  error
}

func (s *tokenRefreshStore) AccountByID(context.Context, string) (domain.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.account, nil
}

func (s *tokenRefreshStore) TryAcquireAccountRefreshLease(context.Context, string, string, time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lease {
		return false, nil
	}
	s.lease = true
	return true, nil
}

func (s *tokenRefreshStore) ReleaseAccountRefreshLease(context.Context, string, string) error {
	s.mu.Lock()
	s.lease = false
	s.mu.Unlock()
	return nil
}

func (s *tokenRefreshStore) UpdateAccountTokensIfRefreshTokenUnchanged(_ context.Context, _ string, expectedRefresh, access, refresh []byte, expiresAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updateErr != nil {
		return false, s.updateErr
	}
	if !bytes.Equal(s.account.RefreshTokenCiphertext, expectedRefresh) || s.account.Status != domain.StatusActive {
		return false, nil
	}
	s.account.AccessTokenCiphertext = access
	s.account.RefreshTokenCiphertext = refresh
	s.account.TokenExpiresAt = expiresAt
	s.account.Status = domain.StatusActive
	s.updates++
	return true, nil
}

func (s *tokenRefreshStore) ListExpiringAccounts(context.Context, time.Time, int) ([]domain.Account, error) {
	return append([]domain.Account(nil), s.candidates...), nil
}

func (s *tokenRefreshStore) MarkAccountErrorIfRefreshTokenUnchanged(_ context.Context, _ string, expectedRefresh []byte, message string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !bytes.Equal(s.account.RefreshTokenCiphertext, expectedRefresh) || s.account.Status != domain.StatusActive {
		return false, nil
	}
	s.account.Status = domain.StatusRefreshRequired
	s.account.LastError = message
	s.marks++
	return true, nil
}

type tokenRefreshOAuth struct {
	calls atomic.Int32
	token OAuthToken
	err   error
	delay time.Duration
	hook  func()
}

func (o *tokenRefreshOAuth) AuthorizationURL(string, string, string) string { return "" }
func (o *tokenRefreshOAuth) Exchange(context.Context, string, string, string) (OAuthToken, error) {
	return OAuthToken{}, nil
}
func (o *tokenRefreshOAuth) Refresh(context.Context, string) (OAuthToken, error) {
	o.calls.Add(1)
	if o.hook != nil {
		o.hook()
	}
	if o.delay > 0 {
		time.Sleep(o.delay)
	}
	return o.token, o.err
}

func TestRefreshAccountTokenSerializesConcurrentRefresh(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	account := encryptedRefreshAccount(t, manager, now)
	store := &tokenRefreshStore{account: account}
	oauth := &tokenRefreshOAuth{delay: 150 * time.Millisecond, token: OAuthToken{
		AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresAt: now.Add(10 * 24 * time.Hour),
	}}
	service := &Service{store: store, security: manager, oauth: oauth, now: func() time.Time { return now }}

	start := make(chan struct{})
	results := make(chan string, 2)
	for range 2 {
		go func() {
			<-start
			access, _, err := service.refreshAccountToken(context.Background(), account, 2*time.Minute, true)
			if err != nil {
				results <- "error: " + err.Error()
				return
			}
			results <- access
		}()
	}
	close(start)
	for range 2 {
		if got := <-results; got != "new-access" {
			t.Fatalf("access token = %q, want new-access", got)
		}
	}
	if oauth.calls.Load() != 1 || store.updates != 1 {
		t.Fatalf("refresh calls = %d, updates = %d, want one each", oauth.calls.Load(), store.updates)
	}
}

func TestRefreshAccountTokenDoesNotOverwriteReauthorization(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	account := encryptedRefreshAccount(t, manager, now)
	store := &tokenRefreshStore{account: account}
	oauth := &tokenRefreshOAuth{token: OAuthToken{
		AccessToken: "refreshed-access", RefreshToken: "refreshed-refresh", ExpiresAt: now.Add(10 * 24 * time.Hour),
	}}
	oauth.hook = func() {
		store.mu.Lock()
		defer store.mu.Unlock()
		scope := store.account.OwnerUserID + ":" + store.account.ChatGPTAccountID
		store.account.AccessTokenCiphertext, _ = manager.Encrypt("reauthorized-access", []byte(scope+":access"))
		store.account.RefreshTokenCiphertext, _ = manager.Encrypt("reauthorized-refresh", []byte(scope+":refresh"))
		store.account.TokenExpiresAt = now.Add(20 * 24 * time.Hour)
	}
	service := &Service{store: store, security: manager, oauth: oauth, now: func() time.Time { return now }}

	access, refreshed, err := service.refreshAccountToken(context.Background(), account, 2*time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	if access != "reauthorized-access" || refreshed || store.updates != 0 {
		t.Fatalf("access = %q, refreshed = %v, updates = %d", access, refreshed, store.updates)
	}
}

func TestRefreshAccountTokenDoesNotReactivateDisabledAccount(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	account := encryptedRefreshAccount(t, manager, now)
	store := &tokenRefreshStore{account: account}
	oauth := &tokenRefreshOAuth{token: OAuthToken{
		AccessToken: "refreshed-access", RefreshToken: "refreshed-refresh", ExpiresAt: now.Add(10 * 24 * time.Hour),
	}}
	oauth.hook = func() {
		store.mu.Lock()
		store.account.Status = domain.StatusDisabled
		store.mu.Unlock()
	}
	service := &Service{store: store, security: manager, oauth: oauth, now: func() time.Time { return now }}

	_, _, err := service.refreshAccountToken(context.Background(), account, 2*time.Minute, true)
	if !errors.Is(err, domain.ErrAccountUnavailable) {
		t.Fatalf("error = %v, want account unavailable", err)
	}
	if store.account.Status != domain.StatusDisabled || store.updates != 0 {
		t.Fatalf("status = %q, updates = %d", store.account.Status, store.updates)
	}
}

func TestRefreshFailureDoesNotOverwriteReauthorization(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	account := encryptedRefreshAccount(t, manager, now)
	store := &tokenRefreshStore{account: account}
	oauth := &tokenRefreshOAuth{err: errors.New("refresh rejected")}
	oauth.hook = func() {
		store.mu.Lock()
		defer store.mu.Unlock()
		scope := store.account.OwnerUserID + ":" + store.account.ChatGPTAccountID
		store.account.AccessTokenCiphertext, _ = manager.Encrypt("reauthorized-access", []byte(scope+":access"))
		store.account.RefreshTokenCiphertext, _ = manager.Encrypt("reauthorized-refresh", []byte(scope+":refresh"))
		store.account.TokenExpiresAt = now.Add(20 * 24 * time.Hour)
		store.account.Status = domain.StatusActive
	}
	service := &Service{store: store, security: manager, oauth: oauth, now: func() time.Time { return now }}

	_, _, err := service.refreshAccountToken(context.Background(), account, 2*time.Minute, true)
	if !errors.Is(err, errAccountTokenRefreshFailed) {
		t.Fatalf("error = %v, want token refresh failure", err)
	}
	if store.account.Status != domain.StatusActive || store.marks != 0 {
		t.Fatalf("status = %q, marks = %d", store.account.Status, store.marks)
	}
}

func TestRefreshDoesNotRepeatOAuthAfterPersistenceFailure(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	account := encryptedRefreshAccount(t, manager, now)
	store := &tokenRefreshStore{account: account, updateErr: errors.New("database unavailable")}
	oauth := &tokenRefreshOAuth{token: OAuthToken{
		AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresAt: now.Add(10 * 24 * time.Hour),
	}}
	service := &Service{store: store, security: manager, oauth: oauth, now: func() time.Time { return now }}

	_, err := service.refreshAccountWithRetry(context.Background(), account, 30*time.Minute, 3)
	if err == nil {
		t.Fatal("expected persistence error")
	}
	if oauth.calls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want one", oauth.calls.Load())
	}
}

func TestRefreshExpiringAccountTokens(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	account := encryptedRefreshAccount(t, manager, now)
	store := &tokenRefreshStore{account: account, candidates: []domain.Account{account}}
	oauth := &tokenRefreshOAuth{token: OAuthToken{
		AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresAt: now.Add(10 * 24 * time.Hour),
	}}
	service := &Service{store: store, security: manager, oauth: oauth, now: func() time.Time { return now }}

	result, err := service.RefreshExpiringAccountTokens(context.Background(), 30*time.Minute, 200, 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	if result != (TokenRefreshResult{Scanned: 1, Refreshed: 1}) {
		t.Fatalf("result = %+v", result)
	}
}

func encryptedRefreshAccount(t *testing.T, manager *security.Manager, now time.Time) domain.Account {
	t.Helper()
	account := domain.Account{
		ID: "account", OwnerUserID: "owner", ChatGPTAccountID: "chatgpt",
		TokenExpiresAt: now.Add(time.Minute), Status: domain.StatusActive,
	}
	scope := account.OwnerUserID + ":" + account.ChatGPTAccountID
	var err error
	account.AccessTokenCiphertext, err = manager.Encrypt("old-access", []byte(scope+":access"))
	if err != nil {
		t.Fatal(err)
	}
	account.RefreshTokenCiphertext, err = manager.Encrypt("old-refresh", []byte(scope+":refresh"))
	if err != nil {
		t.Fatal(err)
	}
	return account
}
