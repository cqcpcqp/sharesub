package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/openai"
)

const (
	defaultGatewayAPIKeyRPM      = 300
	defaultModelCooldown         = 30 * time.Minute
	defaultTransientBackoff      = time.Second
	maximumTransientBackoff      = 5 * time.Second
	maximumGatewayProtectionKeys = 8192
	gatewayTokenRefreshTimeout   = 3 * time.Minute
)

type gatewayProtectionState struct {
	mu            sync.Mutex
	apiKeyRPM     int
	apiKeyWindows map[string]gatewayRateWindow
	modelBlocks   map[string]time.Time
	keyBackoffs   map[string]time.Time
	now           func() time.Time
}

type gatewayRateWindow struct {
	started time.Time
	count   int
}

func newGatewayProtectionState(apiKeyRPM int) *gatewayProtectionState {
	if apiKeyRPM <= 0 {
		apiKeyRPM = defaultGatewayAPIKeyRPM
	}
	return &gatewayProtectionState{
		apiKeyRPM: apiKeyRPM, apiKeyWindows: make(map[string]gatewayRateWindow),
		modelBlocks: make(map[string]time.Time), keyBackoffs: make(map[string]time.Time), now: time.Now,
	}
}

func (s *gatewayProtectionState) admitAPIKey(apiKeyID string) (bool, time.Duration) {
	if s == nil || s.apiKeyRPM <= 0 || strings.TrimSpace(apiKeyID) == "" {
		return true, 0
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	if until := s.keyBackoffs[apiKeyID]; now.Before(until) {
		return false, until.Sub(now)
	}
	window := s.apiKeyWindows[apiKeyID]
	if window.started.IsZero() || now.Sub(window.started) >= time.Minute || now.Before(window.started) {
		window = gatewayRateWindow{started: now}
	}
	if window.count >= s.apiKeyRPM {
		return false, time.Minute - now.Sub(window.started)
	}
	window.count++
	s.apiKeyWindows[apiKeyID] = window
	return true, 0
}

func (s *gatewayProtectionState) backoffAPIKey(apiKeyID string, headers http.Header) {
	if s == nil || strings.TrimSpace(apiKeyID) == "" {
		return
	}
	delay := defaultTransientBackoff
	if seconds, err := strconv.Atoi(strings.TrimSpace(headers.Get("Retry-After"))); err == nil && seconds > 0 {
		delay = time.Duration(seconds) * time.Second
	}
	if delay > maximumTransientBackoff {
		delay = maximumTransientBackoff
	}
	now := s.now()
	s.mu.Lock()
	if until := now.Add(delay); until.After(s.keyBackoffs[apiKeyID]) {
		s.keyBackoffs[apiKeyID] = until
	}
	s.pruneLocked(now)
	s.mu.Unlock()
}

func gatewayModelKey(accountID, model string) string {
	return strings.TrimSpace(accountID) + "\x00" + strings.ToLower(strings.TrimSpace(model))
}

func (s *gatewayProtectionState) blockModel(accountID, model string) {
	key := gatewayModelKey(accountID, model)
	if s == nil || key == "\x00" {
		return
	}
	now := s.now()
	s.mu.Lock()
	s.modelBlocks[key] = now.Add(defaultModelCooldown)
	s.pruneLocked(now)
	s.mu.Unlock()
}

func (s *gatewayProtectionState) modelBlocked(accountID, model string) bool {
	if s == nil {
		return false
	}
	key, now := gatewayModelKey(accountID, model), s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	until := s.modelBlocks[key]
	if !now.Before(until) {
		delete(s.modelBlocks, key)
		return false
	}
	return true
}

func (s *gatewayProtectionState) clearModel(accountID, model string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.modelBlocks, gatewayModelKey(accountID, model))
	s.mu.Unlock()
}

func (s *gatewayProtectionState) pruneLocked(now time.Time) {
	if len(s.apiKeyWindows) > maximumGatewayProtectionKeys {
		for key, window := range s.apiKeyWindows {
			if now.Sub(window.started) >= time.Minute {
				delete(s.apiKeyWindows, key)
			}
		}
	}
	if len(s.modelBlocks) > maximumGatewayProtectionKeys {
		for key, until := range s.modelBlocks {
			if !now.Before(until) {
				delete(s.modelBlocks, key)
			}
		}
	}
	if len(s.keyBackoffs) > maximumGatewayProtectionKeys {
		for key, until := range s.keyBackoffs {
			if !now.Before(until) {
				delete(s.keyBackoffs, key)
			}
		}
	}
}

func retryAfterSeconds(delay time.Duration) string {
	seconds := int(delay.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}

func (s *Server) admitGatewayAPIKey(w http.ResponseWriter, apiKeyID string) bool {
	allowed, retryAfter := s.protections.admitAPIKey(apiKeyID)
	if allowed {
		return true
	}
	w.Header().Set("Retry-After", retryAfterSeconds(retryAfter))
	writeGatewayErrorStatus(w, http.StatusTooManyRequests, "api_key_rate_limited", "API key request rate limit reached")
	return false
}

func isTransientUpstreamStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == 529 || status >= http.StatusInternalServerError
}

func (s *Server) refreshRejectedGatewayAccess(ctx context.Context, access *application.GatewayAccess, status int, body []byte, alreadyRefreshed map[string]bool) bool {
	if status != http.StatusUnauthorized || access == nil || alreadyRefreshed[access.Credential.Account.ID] {
		return false
	}
	alreadyRefreshed[access.Credential.Account.ID] = true
	if openai.IsRevokedCodexTokenError(status, body) {
		markCtx, cancel := metricContext(ctx)
		err := s.app.MarkGatewayAccountRefreshRequired(markCtx, *access, "upstream rejected revoked Codex OAuth token")
		cancel()
		if err != nil {
			s.logger.Warn("mark revoked Codex account refresh required", "account_id", access.Credential.Account.ID, "error", err)
		}
		return false
	}
	refreshCtx, cancel := gatewayTokenRefreshContext(ctx)
	token, err := s.app.RefreshGatewayAccountAccess(refreshCtx, *access)
	cancel()
	if err != nil {
		s.logger.Warn("refresh Codex account after upstream 401", "account_id", access.Credential.Account.ID, "error", err)
		return false
	}
	access.AccessToken = token
	return true
}

func gatewayTokenRefreshContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), gatewayTokenRefreshTimeout)
}
