package httpapi

import (
	"context"
	"errors"
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
	releaseSlot, ok := s.gateway.TryAcquire()
	if !ok {
		writeGatewayErrorStatus(w, http.StatusServiceUnavailable, "server_overloaded", "gateway concurrency limit reached")
		return
	}
	defer releaseSlot()

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
	apiKey := bearerToken(r)
	access, err := s.app.ResolveGatewayAccess(r.Context(), apiKey)
	if err != nil {
		writeGatewayDomainError(w, err)
		return
	}
	defer func() { releaseGatewayAccess(&access) }()
	releaseSlot, ok := s.gateway.TryAcquire()
	if !ok {
		writeGatewayErrorStatus(w, http.StatusServiceUnavailable, "server_overloaded", "gateway concurrency limit reached")
		return
	}
	defer releaseSlot()
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
	clientWantsStream := billingMetadata.Stream && !compact
	gatewayRequestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
	if gatewayRequestID == "" {
		gatewayRequestID = strings.TrimSpace(r.Header.Get("Openai-Request-Id"))
	}
	if gatewayRequestID == "" {
		gatewayRequestID, _ = security.NewID()
	}
	for switches := 0; ; {
		attemptStartedAt := time.Now()
		policyBody, policyMetadata, policyErr := openai.ApplyFastPolicy(forwardBody, billingMetadata, access.Credential.Account.FastPolicy, access.Credential.Member.UserID)
		if policyErr != nil {
			if blocked, ok := policyErr.(*openai.FastPolicyBlockedError); ok {
				writeGatewayErrorStatus(w, http.StatusForbidden, "permission_error", blocked.Message)
				return
			}
			writeGatewayErrorStatus(w, http.StatusInternalServerError, "policy_error", policyErr.Error())
			return
		}
		attemptCtx, cancelAttempt := upstreamAttemptContext(r.Context())
		upstream, err := s.gateway.Forward(attemptCtx, r, policyBody, policyMetadata, access.AccessToken, access.Credential.Account.ChatGPTAccountID, access.Credential.APIKeyID, access.ProxyURL)
		if err != nil {
			cancelAttempt()
			s.recordGatewayMetric(r.Context(), access, gatewayMetric(gatewayRequestID, billingMetadata.Model, policyMetadata, http.StatusBadGateway, openai.ProxyMetrics{Duration: time.Since(attemptStartedAt)}))
			if r.Context().Err() != nil {
				return
			}
			if switches < maxUpstreamAccountSwitches {
				excludedAccountIDs = append(excludedAccountIDs, access.Credential.Account.ID)
				releaseGatewayAccess(&access)
				next, resolveErr := s.app.ResolveGatewayAccess(r.Context(), apiKey, excludedAccountIDs...)
				if resolveErr == nil {
					access = next
					switches++
					continue
				}
			}
			writeGatewayErrorStatus(w, http.StatusBadGateway, "upstream_unavailable", err.Error())
			return
		}

		requestID := upstreamRequestID(upstream, gatewayRequestID)
		if upstream.StatusCode >= 200 && upstream.StatusCode < 300 {
			s.recordGatewayUsage(r.Context(), access, upstream.Header, requestID)
		}

		if shouldSwitchUpstreamAccount(upstream.StatusCode) {
			metrics, rejectedBody, drainErr := openai.DrainResponse(upstream, attemptStartedAt)
			_ = upstream.Body.Close()
			cancelAttempt()
			if drainErr != nil {
				s.logger.Warn("drain rejected upstream response", "request_id", requestID, "error", drainErr)
			}
			s.recordGatewayMetric(r.Context(), access, gatewayMetric(requestID, billingMetadata.Model, policyMetadata, upstream.StatusCode, metrics))
			if err := s.recordGatewayAccountQuota(r.Context(), access, upstream.Header); err != nil {
				s.logger.Debug("upstream rejection did not include Codex quota signal", "account_id", access.Credential.Account.ID, "status", upstream.StatusCode)
			}
			if r.Context().Err() != nil {
				return
			}
			if switches >= maxUpstreamAccountSwitches {
				if err := openai.WriteDrainedResponse(w, upstream, rejectedBody); err != nil {
					s.logger.Warn("write rejected upstream response", "request_id", requestID, "error", err)
				}
				return
			}
			excludedAccountIDs = append(excludedAccountIDs, access.Credential.Account.ID)
			releaseGatewayAccess(&access)
			next, resolveErr := s.app.ResolveGatewayAccess(r.Context(), apiKey, excludedAccountIDs...)
			if resolveErr != nil {
				if err := openai.WriteDrainedResponse(w, upstream, rejectedBody); err != nil {
					s.logger.Warn("write rejected upstream response", "request_id", requestID, "error", err)
				}
				return
			}
			access = next
			switches++
			continue
		}

		metrics, copyErr := openai.CopyResponseForRequest(w, upstream, attemptStartedAt, policyMetadata.Stream && !compact)
		_ = upstream.Body.Close()
		cancelAttempt()
		metricStatus := upstream.StatusCode
		var failoverErr *openai.StreamFailoverError
		if errors.As(copyErr, &failoverErr) {
			metricStatus = failoverErr.StatusCode
		}
		metric := gatewayMetric(requestID, billingMetadata.Model, policyMetadata, metricStatus, metrics)
		s.recordGatewayMetric(r.Context(), access, metric)

		if errors.As(copyErr, &failoverErr) && switches < maxUpstreamAccountSwitches && r.Context().Err() == nil {
			excludedAccountIDs = append(excludedAccountIDs, access.Credential.Account.ID)
			releaseGatewayAccess(&access)
			next, resolveErr := s.app.ResolveGatewayAccess(r.Context(), apiKey, excludedAccountIDs...)
			if resolveErr == nil {
				access = next
				switches++
				continue
			}
		}
		if errors.As(copyErr, &failoverErr) {
			if err := openai.WriteStreamFailoverError(w, upstream, failoverErr, clientWantsStream); err != nil {
				s.logger.Warn("write terminal upstream failure", "request_id", requestID, "error", err)
			}
			return
		}
		if copyErr != nil {
			s.logger.Warn("copy upstream response", "request_id", requestID, "error", copyErr)
		}
		return
	}
}

func shouldSwitchUpstreamAccount(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusPaymentRequired || status == http.StatusForbidden || status == http.StatusTooManyRequests || status == 529 || (status >= http.StatusInternalServerError && status < 600)
}

const gatewayMetricWriteTimeout = 5 * time.Second
const gatewayUpstreamTimeout = 10 * time.Minute

func upstreamAttemptContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, gatewayUpstreamTimeout)
}

func upstreamRequestID(upstream *http.Response, fallback string) string {
	requestID := upstream.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = upstream.Header.Get("Openai-Request-Id")
	}
	if requestID == "" {
		requestID = fallback
	}
	return requestID
}

func gatewayMetric(requestID, requestedModel string, metadata openai.RequestBilling, status int, metrics openai.ProxyMetrics) domain.GatewayMetric {
	upstreamModel := metrics.UpstreamModel
	if upstreamModel == "" {
		upstreamModel = metadata.Model
	}
	return domain.GatewayMetric{
		RequestID: requestID, Model: metadata.Model, RequestedModel: requestedModel, UpstreamModel: upstreamModel, BillingModel: upstreamModel,
		ServiceTier: metadata.ServiceTier, StatusCode: status, TTFT: metrics.TTFT, Duration: metrics.Duration,
		TokenUsage: domain.TokenUsage{
			InputTokens: metrics.InputTokens, OutputTokens: metrics.OutputTokens, CachedTokens: metrics.CachedTokens,
			CacheCreationTokens: metrics.CacheCreationTokens, ImageInputTokens: metrics.ImageInputTokens, ImageOutputTokens: metrics.ImageOutputTokens,
			ImageCount: metrics.ImageCount, TotalTokens: metrics.InputTokens + metrics.OutputTokens,
		},
		ImageCount: metrics.ImageCount, WebSearchCalls: metrics.WebSearchCalls,
	}
}

func metricContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), gatewayMetricWriteTimeout)
}

func (s *Server) recordGatewayMetric(parent context.Context, access application.GatewayAccess, metric domain.GatewayMetric) {
	ctx, cancel := metricContext(parent)
	defer cancel()
	if err := s.app.RecordGatewayMetric(ctx, access, metric); err != nil {
		s.logger.Warn("record gateway metric", "request_id", metric.RequestID, "error", err)
	}
}

func (s *Server) recordGatewayUsage(parent context.Context, access application.GatewayAccess, headers http.Header, requestID string) {
	ctx, cancel := metricContext(parent)
	defer cancel()
	if err := s.app.RecordGatewayUsage(ctx, access, headers, requestID); err != nil {
		s.logger.Warn("record Codex quota signal", "request_id", requestID, "account_id", access.Credential.Account.ID, "error", err)
	}
}

func (s *Server) recordGatewayAccountQuota(parent context.Context, access application.GatewayAccess, headers http.Header) error {
	ctx, cancel := metricContext(parent)
	defer cancel()
	return s.app.RecordGatewayAccountQuota(ctx, access, headers)
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
