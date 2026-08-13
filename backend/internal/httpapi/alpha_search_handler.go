package httpapi

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/openai"
)

func (s *Server) alphaSearch(w http.ResponseWriter, r *http.Request) {
	apiKey := bearerToken(r)
	access, err := s.app.ResolveGatewayAccess(r.Context(), apiKey)
	if err != nil {
		writeGatewayDomainError(w, err)
		return
	}
	defer func() { releaseGatewayAccess(&access) }()
	requestID := gatewayRequestID(r)

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTextGatewayBody))
	if err != nil {
		s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(requestID, r.URL.Path, "", openai.RequestBilling{}, http.StatusRequestEntityTooLarge, domain.GatewayErrorSourceRequest, "request_too_large", textGatewayBodyTooLargeMessage, 0), time.Now())
		writeGatewayErrorStatus(w, http.StatusRequestEntityTooLarge, "request_too_large", textGatewayBodyTooLargeMessage)
		return
	}
	forwardBody, metadata, err := openai.PrepareAlphaSearchRequest(body)
	if err != nil {
		s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(requestID, r.URL.Path, "", openai.RequestBilling{}, http.StatusBadRequest, domain.GatewayErrorSourceRequest, "invalid_request_error", err.Error(), 0), time.Now())
		writeGatewayErrorStatus(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	excludedAccountIDs := make([]string, 0, maxUpstreamAccountSwitches)
	for switches := 0; ; {
		attemptStartedAt := time.Now()
		attemptCtx, cancelAttempt := upstreamAttemptContext(r.Context())
		upstream, forwardErr := s.gateway.ForwardAlphaSearch(attemptCtx, r, forwardBody, access.AccessToken, access.Credential.Account.ChatGPTAccountID, access.ProxyURL, openai.CodexFingerprintConfig{
			AccountID: access.Credential.Account.ID, APIKeyID: access.Credential.APIKeyID, Mode: access.Credential.Account.CodexFingerprintMode,
		})
		if forwardErr != nil {
			cancelAttempt()
			var fingerprintErr *openai.CodexFingerprintRequestError
			if errors.As(forwardErr, &fingerprintErr) {
				s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(requestID, r.URL.Path, metadata.Model, metadata, http.StatusBadRequest, domain.GatewayErrorSourceRequest, "invalid_request_error", forwardErr.Error(), time.Since(attemptStartedAt)), attemptStartedAt)
				writeGatewayErrorStatus(w, http.StatusBadRequest, "invalid_request_error", forwardErr.Error())
				return
			}
			if r.Context().Err() != nil {
				s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(requestID, r.URL.Path, metadata.Model, metadata, clientClosedRequestStatus, domain.GatewayErrorSourceRequest, "client_disconnected", "client disconnected before response completed", time.Since(attemptStartedAt)), attemptStartedAt)
				return
			}
			s.recordGatewayMetric(r.Context(), access, gatewayErrorMetric(requestID, r.URL.Path, metadata.Model, metadata, http.StatusBadGateway, domain.GatewayErrorSourceUpstream, "upstream_unavailable", forwardErr.Error(), time.Since(attemptStartedAt)), attemptStartedAt)
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

		selectedRequestID := upstreamRequestID(upstream, requestID)
		quotaObservedAt := time.Now()
		if shouldSwitchUpstreamAccount(upstream.StatusCode) {
			metrics, rejectedBody, drainErr := openai.DrainResponse(upstream, attemptStartedAt)
			_ = upstream.Body.Close()
			cancelAttempt()
			if drainErr != nil {
				s.logger.Warn("drain rejected alpha search response", "request_id", selectedRequestID, "error", drainErr)
			}
			s.recordGatewayMetric(r.Context(), access, gatewayMetric(selectedRequestID, metadata.Model, r.URL.Path, metadata, upstream.StatusCode, metrics), attemptStartedAt)
			if err := s.recordGatewayAccountQuota(r.Context(), access, upstream.Header, quotaObservedAt); err != nil {
				s.logger.Debug("upstream alpha search rejection did not include Codex quota signal", "account_id", access.Credential.Account.ID, "status", upstream.StatusCode)
			}
			if r.Context().Err() != nil {
				return
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

		metrics, copyErr := openai.CopyAlphaSearchResponse(w, upstream, attemptStartedAt)
		_ = upstream.Body.Close()
		cancelAttempt()
		metricStatus := gatewayMetricStatus(upstream.StatusCode, metrics, copyErr)
		metric := gatewayMetric(selectedRequestID, metadata.Model, r.URL.Path, metadata, metricStatus, metrics)
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
		metricErr := s.recordGatewayMetric(r.Context(), access, metric, attemptStartedAt)
		if metricErr == nil && upstream.StatusCode >= http.StatusOK && upstream.StatusCode < http.StatusMultipleChoices {
			s.recordGatewayUsage(r.Context(), access, upstream.Header, selectedRequestID, quotaObservedAt)
		}
		if copyErr != nil {
			s.logger.Warn("copy upstream alpha search response", "request_id", selectedRequestID, "error", copyErr)
		}
		return
	}
}
