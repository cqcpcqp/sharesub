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

const clientClosedRequestStatus = 499

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
	gatewayRequestID := gatewayRequestID(r)
	releaseSlot, ok := s.gateway.TryAcquire()
	if !ok {
		s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(gatewayRequestID, r.URL.Path, "", openai.RequestBilling{}, http.StatusServiceUnavailable, domain.GatewayErrorSourceGateway, "server_overloaded", "gateway concurrency limit reached", 0))
		writeGatewayErrorStatus(w, http.StatusServiceUnavailable, "server_overloaded", "gateway concurrency limit reached")
		return
	}
	defer releaseSlot()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxGatewayBody))
	if err != nil {
		s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(gatewayRequestID, r.URL.Path, "", openai.RequestBilling{}, http.StatusRequestEntityTooLarge, domain.GatewayErrorSourceRequest, "request_too_large", gatewayBodyTooLargeMessage, 0))
		writeGatewayErrorStatus(w, http.StatusRequestEntityTooLarge, "request_too_large", gatewayBodyTooLargeMessage)
		return
	}
	compact := strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/responses/compact")
	forwardBody, billingMetadata, err := openai.PrepareRequest(body, compact)
	if err != nil {
		s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(gatewayRequestID, r.URL.Path, "", openai.RequestBilling{}, http.StatusBadRequest, domain.GatewayErrorSourceRequest, "invalid_request_error", err.Error(), 0))
		writeGatewayErrorStatus(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	excludedAccountIDs := make([]string, 0, maxUpstreamAccountSwitches)
	clientWantsStream := billingMetadata.Stream && !compact
	for switches := 0; ; {
		attemptStartedAt := time.Now()
		policyBody, policyMetadata, policyErr := openai.ApplyFastPolicy(forwardBody, billingMetadata, access.Credential.Account.FastPolicy, access.Credential.Member.UserID)
		if policyErr != nil {
			if blocked, ok := policyErr.(*openai.FastPolicyBlockedError); ok {
				s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(gatewayRequestID, r.URL.Path, billingMetadata.Model, billingMetadata, http.StatusForbidden, domain.GatewayErrorSourceRequest, "permission_error", blocked.Message, time.Since(attemptStartedAt)))
				writeGatewayErrorStatus(w, http.StatusForbidden, "permission_error", blocked.Message)
				return
			}
			s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(gatewayRequestID, r.URL.Path, billingMetadata.Model, billingMetadata, http.StatusInternalServerError, domain.GatewayErrorSourceGateway, "policy_error", policyErr.Error(), time.Since(attemptStartedAt)))
			writeGatewayErrorStatus(w, http.StatusInternalServerError, "policy_error", policyErr.Error())
			return
		}
		attemptCtx, cancelAttempt := upstreamAttemptContext(r.Context())
		upstream, err := s.gateway.Forward(attemptCtx, r, policyBody, policyMetadata, access.AccessToken, access.Credential.Account.ChatGPTAccountID, access.Credential.APIKeyID, access.ProxyURL)
		if err != nil {
			cancelAttempt()
			if r.Context().Err() != nil {
				s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(gatewayRequestID, r.URL.Path, billingMetadata.Model, policyMetadata, clientClosedRequestStatus, domain.GatewayErrorSourceRequest, "client_disconnected", "client disconnected before response completed", time.Since(attemptStartedAt)))
				return
			}
			s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(gatewayRequestID, r.URL.Path, billingMetadata.Model, policyMetadata, http.StatusBadGateway, domain.GatewayErrorSourceUpstream, "upstream_unavailable", err.Error(), time.Since(attemptStartedAt)))
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
			s.recordGatewayMetric(r.Context(), access, gatewayMetric(requestID, billingMetadata.Model, r.URL.Path, policyMetadata, upstream.StatusCode, metrics))
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
		metricStatus := gatewayMetricStatus(upstream.StatusCode, metrics, copyErr)
		var failoverErr *openai.StreamFailoverError
		metric := gatewayMetric(requestID, billingMetadata.Model, r.URL.Path, policyMetadata, metricStatus, metrics)
		if metrics.ClientDisconnected || r.Context().Err() != nil {
			metric.StatusCode = clientClosedRequestStatus
			metric.ErrorSource = domain.GatewayErrorSourceRequest
			metric.ErrorCode = "client_disconnected"
			metric.ErrorMessage = "client disconnected before response completed"
		} else if copyErr != nil && metric.ErrorMessage == "" {
			metric.ErrorSource = domain.GatewayErrorSourceUpstream
			metric.ErrorCode = "upstream_error"
			metric.ErrorMessage = copyErr.Error()
		}
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

func (s *Server) images(w http.ResponseWriter, r *http.Request) {
	apiKey := bearerToken(r)
	access, err := s.app.ResolveGatewayAccess(r.Context(), apiKey)
	if err != nil {
		writeGatewayDomainError(w, err)
		return
	}
	defer func() { releaseGatewayAccess(&access) }()
	gatewayRequestID := gatewayRequestID(r)
	releaseSlot, ok := s.gateway.TryAcquire()
	if !ok {
		s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(gatewayRequestID, r.URL.Path, "", openai.RequestBilling{}, http.StatusServiceUnavailable, domain.GatewayErrorSourceGateway, "server_overloaded", "gateway concurrency limit reached", 0))
		writeGatewayErrorStatus(w, http.StatusServiceUnavailable, "server_overloaded", "gateway concurrency limit reached")
		return
	}
	defer releaseSlot()

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxGatewayBody))
	if err != nil {
		s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(gatewayRequestID, r.URL.Path, "", openai.RequestBilling{}, http.StatusRequestEntityTooLarge, domain.GatewayErrorSourceRequest, "request_too_large", gatewayBodyTooLargeMessage, 0))
		writeGatewayErrorStatus(w, http.StatusRequestEntityTooLarge, "request_too_large", gatewayBodyTooLargeMessage)
		return
	}
	forwardBody, imageRequest, billingMetadata, err := openai.PrepareImagesRequest(body, r.Header.Get("Content-Type"), r.URL.Path)
	if err != nil {
		s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(gatewayRequestID, r.URL.Path, "", openai.RequestBilling{}, http.StatusBadRequest, domain.GatewayErrorSourceRequest, "invalid_request_error", err.Error(), 0))
		writeGatewayErrorStatus(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	excludedAccountIDs := make([]string, 0, maxUpstreamAccountSwitches)
	for switches := 0; ; {
		attemptStartedAt := time.Now()
		attemptCtx, cancelAttempt := upstreamAttemptContext(r.Context())
		upstream, forwardErr := s.gateway.Forward(attemptCtx, r, forwardBody, billingMetadata, access.AccessToken, access.Credential.Account.ChatGPTAccountID, access.Credential.APIKeyID, access.ProxyURL)
		if forwardErr != nil {
			cancelAttempt()
			if r.Context().Err() != nil {
				s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(gatewayRequestID, r.URL.Path, imageRequest.Model, billingMetadata, clientClosedRequestStatus, domain.GatewayErrorSourceRequest, "client_disconnected", "client disconnected before response completed", time.Since(attemptStartedAt)))
				return
			}
			metric := gatewayErrorMetric(gatewayRequestID, r.URL.Path, imageRequest.Model, billingMetadata, http.StatusBadGateway, domain.GatewayErrorSourceUpstream, "upstream_unavailable", forwardErr.Error(), time.Since(attemptStartedAt))
			metric.UpstreamModel = imageRequest.Model
			s.recordGatewayMetric(r.Context(), access, metric)
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
			writeGatewayErrorStatus(w, http.StatusBadGateway, "upstream_unavailable", forwardErr.Error())
			return
		}

		requestID := upstreamRequestID(upstream, gatewayRequestID)
		if upstream.StatusCode >= 200 && upstream.StatusCode < 300 {
			s.recordGatewayUsage(r.Context(), access, upstream.Header, requestID)
		}
		if shouldSwitchUpstreamAccount(upstream.StatusCode) {
			metrics, rejectedBody, drainErr := openai.DrainResponse(upstream, attemptStartedAt)
			metrics.UpstreamModel = imageRequest.Model
			_ = upstream.Body.Close()
			cancelAttempt()
			if drainErr != nil {
				s.logger.Warn("drain rejected images response", "request_id", requestID, "error", drainErr)
			}
			s.recordGatewayMetric(r.Context(), access, gatewayMetric(requestID, imageRequest.Model, r.URL.Path, billingMetadata, upstream.StatusCode, metrics))
			if err := s.recordGatewayAccountQuota(r.Context(), access, upstream.Header); err != nil {
				s.logger.Debug("upstream image rejection did not include Codex quota signal", "account_id", access.Credential.Account.ID, "status", upstream.StatusCode)
			}
			if switches >= maxUpstreamAccountSwitches {
				_ = openai.WriteDrainedResponse(w, upstream, rejectedBody)
				return
			}
			excludedAccountIDs = append(excludedAccountIDs, access.Credential.Account.ID)
			releaseGatewayAccess(&access)
			next, resolveErr := s.app.ResolveGatewayAccess(r.Context(), apiKey, excludedAccountIDs...)
			if resolveErr != nil {
				_ = openai.WriteDrainedResponse(w, upstream, rejectedBody)
				return
			}
			access = next
			switches++
			continue
		}

		metrics, copyErr := openai.CopyImagesResponse(w, upstream, attemptStartedAt, imageRequest)
		_ = upstream.Body.Close()
		cancelAttempt()
		metrics.UpstreamModel = imageRequest.Model
		metricStatus := gatewayMetricStatus(upstream.StatusCode, metrics, copyErr)
		var failoverErr *openai.StreamFailoverError
		metric := gatewayMetric(requestID, imageRequest.Model, r.URL.Path, billingMetadata, metricStatus, metrics)
		if metrics.ClientDisconnected || r.Context().Err() != nil {
			metric.StatusCode = clientClosedRequestStatus
			metric.ErrorSource = domain.GatewayErrorSourceRequest
			metric.ErrorCode = "client_disconnected"
			metric.ErrorMessage = "client disconnected before response completed"
		} else if copyErr != nil && metric.ErrorMessage == "" {
			metric.ErrorSource = domain.GatewayErrorSourceUpstream
			metric.ErrorCode = "upstream_error"
			metric.ErrorMessage = copyErr.Error()
		}
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
			writeGatewayErrorStatus(w, failoverErr.StatusCode, "upstream_error", "upstream image generation failed")
			return
		}
		if copyErr != nil {
			s.logger.Warn("copy upstream images response", "request_id", requestID, "error", copyErr)
			if !imageRequest.Stream {
				writeGatewayErrorStatus(w, http.StatusBadGateway, "upstream_error", copyErr.Error())
			}
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

func gatewayRequestID(r *http.Request) string {
	requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
	if requestID == "" {
		requestID = strings.TrimSpace(r.Header.Get("Openai-Request-Id"))
	}
	if requestID == "" {
		requestID, _ = security.NewID()
	}
	return requestID
}

func gatewayMetric(requestID, requestedModel, endpoint string, metadata openai.RequestBilling, status int, metrics openai.ProxyMetrics) domain.GatewayMetric {
	upstreamModel := metrics.UpstreamModel
	if upstreamModel == "" {
		upstreamModel = metadata.Model
	}
	errorSource := ""
	if status < http.StatusOK || status >= http.StatusMultipleChoices || metrics.ErrorCode != "" || metrics.ErrorMessage != "" {
		errorSource = domain.GatewayErrorSourceUpstream
	}
	return domain.GatewayMetric{
		RequestID: requestID, Model: metadata.Model, RequestedModel: requestedModel, UpstreamModel: upstreamModel, BillingModel: upstreamModel,
		ServiceTier: metadata.ServiceTier, Endpoint: endpoint, IsStream: metadata.Stream, StatusCode: status,
		ErrorSource: errorSource, ErrorCode: metrics.ErrorCode, ErrorMessage: metrics.ErrorMessage,
		TTFT: metrics.TTFT, Duration: metrics.Duration,
		TokenUsage: domain.TokenUsage{
			InputTokens: metrics.InputTokens, OutputTokens: metrics.OutputTokens, CachedTokens: metrics.CachedTokens,
			CacheCreationTokens: metrics.CacheCreationTokens, ImageInputTokens: metrics.ImageInputTokens, ImageOutputTokens: metrics.ImageOutputTokens,
			ImageCount: metrics.ImageCount, TotalTokens: metrics.InputTokens + metrics.OutputTokens,
		},
		ImageCount: metrics.ImageCount, WebSearchCalls: metrics.WebSearchCalls,
		ImageSize: metrics.ImageSize,
	}
}

func gatewayErrorMetric(requestID, endpoint, requestedModel string, metadata openai.RequestBilling, status int, source, code, message string, duration time.Duration) domain.GatewayMetric {
	metric := gatewayMetric(requestID, requestedModel, endpoint, metadata, status, openai.ProxyMetrics{Duration: duration})
	metric.ErrorSource = source
	metric.ErrorCode = code
	metric.ErrorMessage = message
	return metric
}

func gatewayMetricStatus(upstreamStatus int, metrics openai.ProxyMetrics, copyErr error) int {
	status := upstreamStatus
	if upstreamStatus >= http.StatusOK && upstreamStatus < http.StatusMultipleChoices && metrics.ErrorStatusCode != 0 {
		status = metrics.ErrorStatusCode
	}
	if copyErr != nil {
		status = http.StatusBadGateway
	}
	var failoverErr *openai.StreamFailoverError
	if errors.As(copyErr, &failoverErr) {
		status = failoverErr.StatusCode
	}
	return status
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
