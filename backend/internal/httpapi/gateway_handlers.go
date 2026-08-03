package httpapi

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/openai"
	"github.com/sharesub/sharesub/backend/internal/security"
)

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("client_version") != "" {
		s.codexModels(w, r)
		return
	}
	if err := s.app.AuthenticateGatewayKey(r.Context(), bearerToken(r)); err != nil {
		writeGatewayDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": openai.CodexModels})
}

func (s *Server) codexModels(w http.ResponseWriter, r *http.Request) {
	apiKey := bearerToken(r)
	access, err := s.app.ResolveGatewayAccess(r.Context(), apiKey)
	if err != nil {
		writeGatewayDomainError(w, err)
		return
	}
	defer func() { releaseGatewayAccess(&access) }()

	excludedAccountIDs := make([]string, 0, maxUpstreamAccountSwitches)
	var upstream *http.Response
	for switches := 0; ; {
		upstream, err = s.gateway.FetchModels(r.Context(), r, access.AccessToken, access.Credential.Account.ChatGPTAccountID, access.ProxyURL)
		if err == nil && !shouldSwitchModelsAccount(upstream.StatusCode) {
			break
		}
		if switches >= maxUpstreamAccountSwitches {
			break
		}

		excludedAccountIDs = append(excludedAccountIDs, access.Credential.Account.ID)
		releaseGatewayAccess(&access)
		next, resolveErr := s.app.ResolveGatewayAccess(r.Context(), apiKey, excludedAccountIDs...)
		if resolveErr != nil {
			break
		}
		if upstream != nil {
			drainAndCloseResponse(upstream)
		}
		access = next
		switches++
	}
	if err != nil {
		writeGatewayErrorStatus(w, http.StatusBadGateway, "upstream_unavailable", err.Error())
		return
	}
	defer upstream.Body.Close()
	copyModelsResponse(w, upstream)
}

func shouldSwitchModelsAccount(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusTooManyRequests || (status >= http.StatusInternalServerError && status < 600)
}

func copyModelsResponse(w http.ResponseWriter, upstream *http.Response) {
	for _, key := range []string{"Content-Type", "Cache-Control", "ETag", "Retry-After"} {
		for _, value := range upstream.Header.Values(key) {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(upstream.StatusCode)
	_, _ = io.Copy(w, upstream.Body)
}

func (s *Server) responses(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	apiKey := bearerToken(r)
	access, err := s.app.ResolveGatewayAccess(r.Context(), apiKey)
	if err != nil {
		writeGatewayDomainError(w, err)
		return
	}
	defer func() { releaseGatewayAccess(&access) }()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxGatewayBody))
	if err != nil {
		writeGatewayErrorStatus(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds 32 MiB")
		return
	}
	compact := strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/responses/compact")
	forwardBody, billingMetadata, err := openai.PrepareRequest(body, compact)
	if err != nil {
		writeGatewayErrorStatus(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	excludedAccountIDs := make([]string, 0, maxUpstreamAccountSwitches)
	var upstream *http.Response
	effectiveMetadata := billingMetadata
	for switches := 0; ; {
		policyBody, policyMetadata, policyErr := openai.ApplyFastPolicy(forwardBody, billingMetadata, access.Credential.Account.FastPolicy, access.Credential.Member.UserID)
		if policyErr != nil {
			if blocked, ok := policyErr.(*openai.FastPolicyBlockedError); ok {
				writeGatewayErrorStatus(w, http.StatusForbidden, "permission_error", blocked.Message)
				return
			}
			writeGatewayErrorStatus(w, http.StatusInternalServerError, "policy_error", policyErr.Error())
			return
		}
		effectiveMetadata = policyMetadata
		upstream, err = s.gateway.Forward(r.Context(), r, policyBody, policyMetadata, access.AccessToken, access.Credential.Account.ChatGPTAccountID, access.Credential.APIKeyID, access.ProxyURL)
		if err != nil {
			requestID, _ := security.NewID()
			_ = s.app.RecordGatewayMetric(r.Context(), access, requestID, policyMetadata.Model, policyMetadata.ServiceTier, http.StatusBadGateway, 0, time.Since(startedAt), domain.TokenUsage{})
			writeGatewayErrorStatus(w, http.StatusBadGateway, "upstream_unavailable", err.Error())
			return
		}
		if !shouldSwitchUpstreamAccount(upstream.StatusCode) {
			break
		}
		if err := s.app.RecordGatewayAccountQuota(r.Context(), access, upstream.Header); err != nil {
			s.logger.Debug("upstream rejection did not include Codex quota signal", "account_id", access.Credential.Account.ID, "status", upstream.StatusCode)
		}
		if switches >= maxUpstreamAccountSwitches {
			break
		}

		excludedAccountIDs = append(excludedAccountIDs, access.Credential.Account.ID)
		releaseGatewayAccess(&access)
		next, resolveErr := s.app.ResolveGatewayAccess(r.Context(), apiKey, excludedAccountIDs...)
		if resolveErr != nil {
			break
		}
		drainAndCloseResponse(upstream)
		access = next
		switches++
	}
	defer upstream.Body.Close()
	requestID := upstream.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = upstream.Header.Get("Openai-Request-Id")
	}
	if requestID == "" {
		requestID, _ = security.NewID()
	}
	if upstream.StatusCode >= 200 && upstream.StatusCode < 300 {
		if err := s.app.RecordGatewayUsage(r.Context(), access, upstream.Header, requestID); err != nil {
			s.logger.Warn("record Codex quota signal", "request_id", requestID, "account_id", access.Credential.Account.ID, "error", err)
		}
	}
	metrics, copyErr := openai.CopyResponseForRequest(w, upstream, startedAt, effectiveMetadata.Stream && !compact)
	tokenUsage := domain.TokenUsage{InputTokens: metrics.InputTokens, OutputTokens: metrics.OutputTokens, CachedTokens: metrics.CachedTokens, TotalTokens: metrics.InputTokens + metrics.OutputTokens}
	metricStatus := upstream.StatusCode
	if copyErr != nil {
		metricStatus = http.StatusBadGateway
	}
	if err := s.app.RecordGatewayMetric(r.Context(), access, requestID, effectiveMetadata.Model, effectiveMetadata.ServiceTier, metricStatus, metrics.TTFT, metrics.Duration, tokenUsage); err != nil {
		s.logger.Warn("record gateway metric", "error", err)
	}
	if copyErr != nil {
		s.logger.Warn("copy upstream response", "error", copyErr)
	}
}

func shouldSwitchUpstreamAccount(status int) bool {
	return status == http.StatusTooManyRequests || status == 529
}

func releaseGatewayAccess(access *application.GatewayAccess) {
	if access != nil && access.Release != nil {
		access.Release()
		access.Release = nil
	}
}

func drainAndCloseResponse(response *http.Response) {
	if response == nil || response.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	_ = response.Body.Close()
}
