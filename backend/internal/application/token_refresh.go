package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

const (
	accountRefreshLeaseTTL   = 3 * time.Minute
	accountRefreshWait       = 5 * time.Second
	accountRefreshPoll       = 100 * time.Millisecond
	tokenRefreshRetryBackoff = 2 * time.Second
)

var (
	errAccountRefreshInProgress  = errors.New("account token refresh is already in progress")
	errAccountTokenRefreshFailed = domain.ErrAccountTokenRefresh
)

type TokenRefreshResult struct {
	Scanned   int
	Refreshed int
	Skipped   int
	Failed    int
}

func (s *Service) ManualRefreshAccountToken(ctx context.Context, userID, accountID string) (domain.Account, error) {
	return s.manualRefreshAccountToken(ctx, userID, userID, accountID)
}

func (s *Service) AdminManualRefreshAccountToken(ctx context.Context, admin domain.User, accountID string) (domain.Account, error) {
	if err := requireAdmin(admin); err != nil {
		return domain.Account{}, err
	}
	return s.manualRefreshAccountToken(ctx, admin.ID, "", accountID)
}

func (s *Service) manualRefreshAccountToken(ctx context.Context, actorUserID, requiredOwnerUserID, accountID string) (domain.Account, error) {
	account, err := s.store.AccountByID(ctx, accountID)
	if err != nil {
		return domain.Account{}, err
	}
	if requiredOwnerUserID != "" && account.OwnerUserID != requiredOwnerUserID {
		return domain.Account{}, domain.ErrForbidden
	}
	if account.Status != domain.StatusActive {
		return domain.Account{}, domain.ErrAccountUnavailable
	}
	event, err := s.newAuditEvent(actorUserID, "account.token_refreshed", "account", account.ID, map[string]string{"account_name": account.Name})
	if err != nil {
		return domain.Account{}, err
	}
	refreshBefore := account.TokenExpiresAt.Sub(s.now()) + time.Minute
	if refreshBefore < time.Minute {
		refreshBefore = time.Minute
	}
	markAccountError := !account.TokenExpiresAt.After(s.now())
	_, refreshed, err := s.refreshAccountTokenWithAttempts(ctx, account, refreshBefore, true, 1, &event, markAccountError)
	if err != nil {
		if errors.Is(err, errAccountRefreshInProgress) {
			return domain.Account{}, domain.ErrConflict
		}
		return domain.Account{}, err
	}
	if !refreshed {
		if err := s.store.RecordAuditEvent(ctx, event); err != nil {
			return domain.Account{}, err
		}
	}
	updated, err := s.store.AccountByID(ctx, account.ID)
	if err != nil {
		return domain.Account{}, err
	}
	if err := s.hydrateAccountProxy(&updated); err != nil {
		return domain.Account{}, err
	}
	return updated, nil
}

func (s *Service) RefreshExpiringAccountTokens(ctx context.Context, refreshBefore time.Duration, limit, concurrency, maxRetries int) (TokenRefreshResult, error) {
	accounts, err := s.store.ListExpiringAccounts(ctx, s.now().Add(refreshBefore), limit)
	if err != nil {
		return TokenRefreshResult{}, err
	}
	result := TokenRefreshResult{Scanned: len(accounts)}
	if len(accounts) == 0 {
		return result, nil
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(accounts) {
		concurrency = len(accounts)
	}
	if maxRetries < 1 {
		maxRetries = 1
	}

	jobs := make(chan domain.Account)
	var mu sync.Mutex
	var workers sync.WaitGroup
	workers.Add(concurrency)
	for range concurrency {
		go func() {
			defer workers.Done()
			for account := range jobs {
				refreshed, refreshErr := s.refreshAccountWithRetry(ctx, account, refreshBefore, maxRetries)
				mu.Lock()
				switch {
				case refreshErr == nil && refreshed:
					result.Refreshed++
				case refreshErr == nil || errors.Is(refreshErr, errAccountRefreshInProgress):
					result.Skipped++
				default:
					result.Failed++
				}
				mu.Unlock()
			}
		}()
	}
	for _, account := range accounts {
		select {
		case jobs <- account:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return result, ctx.Err()
		}
	}
	close(jobs)
	workers.Wait()
	return result, nil
}

func (s *Service) refreshAccountWithRetry(ctx context.Context, account domain.Account, refreshBefore time.Duration, maxRetries int) (bool, error) {
	_, refreshed, err := s.refreshAccountTokenWithAttempts(ctx, account, refreshBefore, false, maxRetries, nil, true)
	return refreshed, err
}

func (s *Service) refreshAccountToken(ctx context.Context, account domain.Account, refreshBefore time.Duration, waitForPeer bool) (string, bool, error) {
	return s.refreshAccountTokenWithAttempts(ctx, account, refreshBefore, waitForPeer, 1, nil, true)
}

func (s *Service) refreshAccountTokenWithAttempts(ctx context.Context, account domain.Account, refreshBefore time.Duration, waitForPeer bool, maxAttempts int, auditEvent *domain.AuditEvent, markAccountError bool) (string, bool, error) {
	if account.TokenExpiresAt.After(s.now().Add(refreshBefore)) {
		accessToken, err := s.decryptAccountAccessToken(account)
		return accessToken, false, err
	}
	holderID, err := security.NewID()
	if err != nil {
		return "", false, err
	}
	acquired, err := s.store.TryAcquireAccountRefreshLease(ctx, account.ID, holderID, s.now().Add(accountRefreshLeaseTTL))
	if err != nil {
		return "", false, err
	}
	if !acquired {
		if !waitForPeer {
			return "", false, errAccountRefreshInProgress
		}
		return s.waitForAccountRefresh(ctx, account.ID, refreshBefore)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.store.ReleaseAccountRefreshLease(releaseCtx, account.ID, holderID)
	}()

	latest, err := s.store.AccountByID(ctx, account.ID)
	if err != nil {
		return "", false, err
	}
	if latest.Status != domain.StatusActive {
		return "", false, domain.ErrAccountUnavailable
	}
	if latest.TokenExpiresAt.After(s.now().Add(refreshBefore)) {
		accessToken, decryptErr := s.decryptAccountAccessToken(latest)
		return accessToken, false, decryptErr
	}

	scope := latest.OwnerUserID + ":" + latest.ChatGPTAccountID
	refreshToken, err := s.security.Decrypt(latest.RefreshTokenCiphertext, []byte(scope+":refresh"))
	if err != nil {
		refreshErr := fmt.Errorf("%w: %w", errAccountTokenRefreshFailed, err)
		if markAccountError {
			_, _ = s.store.MarkAccountErrorIfRefreshTokenUnchanged(ctx, latest.ID, latest.RefreshTokenCiphertext, refreshErr.Error())
		}
		return "", false, refreshErr
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var refreshed OAuthToken
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		refreshed, err = s.oauth.Refresh(ctx, refreshToken)
		if err == nil {
			break
		}
		if attempt < maxAttempts {
			timer := time.NewTimer(time.Duration(attempt) * tokenRefreshRetryBackoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", false, ctx.Err()
			case <-timer.C:
			}
		}
	}
	if err != nil {
		refreshErr := fmt.Errorf("%w: %w", errAccountTokenRefreshFailed, err)
		if markAccountError {
			_, _ = s.store.MarkAccountErrorIfRefreshTokenUnchanged(ctx, latest.ID, latest.RefreshTokenCiphertext, refreshErr.Error())
		}
		return "", false, refreshErr
	}
	accessCiphertext, err := s.security.Encrypt(refreshed.AccessToken, []byte(scope+":access"))
	if err != nil {
		return "", false, err
	}
	refreshCiphertext, err := s.security.Encrypt(refreshed.RefreshToken, []byte(scope+":refresh"))
	if err != nil {
		return "", false, err
	}
	updated, err := s.store.UpdateAccountTokensIfRefreshTokenUnchanged(ctx, latest.ID, latest.RefreshTokenCiphertext, accessCiphertext, refreshCiphertext, refreshed.ExpiresAt, auditEvent)
	if err != nil {
		return "", false, err
	}
	if updated {
		if err := s.hydrateAccountProxy(&latest); err == nil {
			if subscriptionExpiresAt, queryErr := s.oauth.SubscriptionExpiresAt(ctx, refreshed.AccessToken, latest.ChatGPTAccountID, latest.ProxyURL); queryErr == nil {
				_, _ = s.store.UpdateAccountSubscriptionExpiresAtIfRefreshTokenUnchanged(ctx, latest.ID, refreshCiphertext, subscriptionExpiresAt)
			}
		}
		return refreshed.AccessToken, true, nil
	}
	current, err := s.store.AccountByID(ctx, latest.ID)
	if err != nil {
		return "", false, err
	}
	if current.Status != domain.StatusActive {
		return "", false, domain.ErrAccountUnavailable
	}
	accessToken, err := s.decryptAccountAccessToken(current)
	return accessToken, false, err
}

func (s *Service) waitForAccountRefresh(ctx context.Context, accountID string, refreshBefore time.Duration) (string, bool, error) {
	timeout := time.NewTimer(accountRefreshWait)
	defer timeout.Stop()
	ticker := time.NewTicker(accountRefreshPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-timeout.C:
			return "", false, errAccountRefreshInProgress
		case <-ticker.C:
			account, err := s.store.AccountByID(ctx, accountID)
			if err != nil {
				return "", false, err
			}
			if account.Status != domain.StatusActive {
				return "", false, domain.ErrAccountUnavailable
			}
			if account.TokenExpiresAt.After(s.now().Add(refreshBefore)) {
				accessToken, decryptErr := s.decryptAccountAccessToken(account)
				return accessToken, false, decryptErr
			}
		}
	}
}

func (s *Service) decryptAccountAccessToken(account domain.Account) (string, error) {
	scope := account.OwnerUserID + ":" + account.ChatGPTAccountID
	accessToken, err := s.security.Decrypt(account.AccessTokenCiphertext, []byte(scope+":access"))
	if err != nil {
		return "", fmt.Errorf("decrypt account access token: %w", err)
	}
	return accessToken, nil
}
