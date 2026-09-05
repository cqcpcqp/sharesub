package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/openai"
)

// ResponsesWebSocketConfig contains the HTTP ingress policy for Responses
// WebSocket v2. Transport details are passed through to the OpenAI session.
type ResponsesWebSocketConfig struct {
	FirstMessageTimeout           time.Duration
	InterTurnIdleTimeout          time.Duration
	MaxSessionDuration            time.Duration
	MaxConnectionsPerAPIKey       int
	OutboundProxyURL              string
	DialTimeout                   time.Duration
	ReadTimeout                   time.Duration
	WriteTimeout                  time.Duration
	UpstreamDrainTimeout          time.Duration
	ClientReadLimitBytes          int64
	UpstreamReadLimitBytes        int64
	ReplayMemoryLimitBytes        int64
	MaxRequestsPerMinutePerAPIKey int
	FirstOutputTimeout            time.Duration
}

func DefaultResponsesWebSocketConfig() ResponsesWebSocketConfig {
	return ResponsesWebSocketConfig{
		FirstMessageTimeout: 30 * time.Second, InterTurnIdleTimeout: 5 * time.Minute,
		MaxSessionDuration: time.Hour, MaxConnectionsPerAPIKey: 64,
		DialTimeout: 10 * time.Second, ReadTimeout: 15 * time.Minute,
		WriteTimeout: 2 * time.Minute, UpstreamDrainTimeout: 1200 * time.Millisecond,
		ClientReadLimitBytes:          64 << 20,
		UpstreamReadLimitBytes:        16 << 20,
		ReplayMemoryLimitBytes:        64 << 20,
		MaxRequestsPerMinutePerAPIKey: defaultGatewayAPIKeyRPM,
		FirstOutputTimeout:            2 * time.Minute,
	}
}

type responsesWebSocketIngressLimiter struct {
	mu     sync.Mutex
	limit  int
	active map[string]int
}

type responsesWebSocketFirstRead struct {
	messageType websocket.MessageType
	frame       []byte
	err         error
}

func newResponsesWebSocketIngressLimiter(limit int) *responsesWebSocketIngressLimiter {
	return &responsesWebSocketIngressLimiter{limit: limit, active: make(map[string]int)}
}

func (l *responsesWebSocketIngressLimiter) acquire(apiKey string) (func(), bool) {
	if l == nil || l.limit <= 0 {
		return func() {}, true
	}
	l.mu.Lock()
	if l.active[apiKey] >= l.limit {
		l.mu.Unlock()
		return nil, false
	}
	l.active[apiKey]++
	l.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.active[apiKey]--
			if l.active[apiKey] == 0 {
				delete(l.active, apiKey)
			}
			l.mu.Unlock()
		})
	}, true
}

func (s *Server) responsesWebSocketHandler(w http.ResponseWriter, r *http.Request) {
	if s.webSocketSessions != nil && s.webSocketSessions.isStopping() {
		writeGatewayErrorStatus(w, http.StatusServiceUnavailable, "server_shutting_down", "server is shutting down")
		return
	}
	if !isWebSocketUpgrade(r) {
		w.Header().Set("Upgrade", "websocket")
		writeGatewayErrorStatus(w, http.StatusUpgradeRequired, "websocket_upgrade_required", "Responses WebSocket v2 requires a WebSocket Upgrade request")
		return
	}
	apiKey := bearerToken(r)
	apiKeyID, err := s.app.ResolveGatewayAPIKeyID(r.Context(), apiKey)
	if err != nil {
		writeGatewayDomainError(w, err)
		return
	}
	if allowed, retryAfter := s.protections.admitAPIKey(apiKeyID); !allowed {
		w.Header().Set("Retry-After", retryAfterSeconds(retryAfter))
		writeGatewayErrorStatus(w, http.StatusTooManyRequests, "api_key_rate_limited", "API key request rate limit reached")
		return
	}
	releaseIngress, acquired := s.webSocketIngress.acquire(apiKeyID)
	if !acquired {
		w.Header().Set("Retry-After", "5")
		writeGatewayErrorStatus(w, http.StatusTooManyRequests, "websocket_connection_limit", "too many open WebSocket connections for this API key")
		return
	}
	defer releaseIngress()

	// Register before Accept so Shutdown cannot miss an in-flight upgrade. The
	// registry closes the registration if shutdown wins before the accepted
	// connection can be bound.
	sessionParent, cancelSessionCause := context.WithCancelCause(context.WithoutCancel(r.Context()))
	sessionCtx, cancelSessionTimeout := contextWithPositiveTimeout(sessionParent, s.webSocketConfig.MaxSessionDuration)
	cancelSession := func() {
		cancelSessionTimeout()
		cancelSessionCause(context.Canceled)
	}
	defer cancelSession()
	activeSession, registered := s.webSocketSessions.register(cancelSessionCause)
	if !registered {
		writeGatewayErrorStatus(w, http.StatusServiceUnavailable, "server_shutting_down", "server is shutting down")
		return
	}
	defer s.webSocketSessions.unregister(activeSession)

	client, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		s.logger.Warn("accept Responses WebSocket", "error", err)
		return
	}
	defer client.CloseNow()
	client.SetReadLimit(s.webSocketConfig.ClientReadLimitBytes)
	if !activeSession.bindClient(client) {
		_ = client.Close(websocket.StatusGoingAway, responsesWebSocketShutdownReason)
		return
	}
	messageType, firstFrame, err := readResponsesWebSocketFirstFrame(sessionCtx, client, s.webSocketConfig.FirstMessageTimeout)
	if err != nil {
		return
	}
	if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
		_ = client.Close(websocket.StatusPolicyViolation, "unsupported WebSocket message type")
		return
	}

	var pinned application.GatewayAccess
	var turnAccess application.GatewayAccess
	defer func() { releaseGatewayAccess(&turnAccess) }()
	connectionRequestID := gatewayRequestID(r)
	firstTurn := true
	failedFirstAccountIDs := make([]string, 0, maxUpstreamAccountSwitches)
	firstAccountSwitches := 0
	turnAttempt := 0
	authRefreshed := make(map[string]bool)
	replacePinnedAfterTurn := false
	setTurnAccess := func(access application.GatewayAccess, request openai.ResponsesWebSocketTurnRequest) (openai.ResponsesWebSocketTurnConfig, error) {
		turnAccess = access
		policyStartedAt := time.Now()
		policyFrame, policyBilling, policyErr := openai.ApplyFastPolicy(
			request.Frame, request.Billing,
			access.Credential.Account.FastPolicy,
			access.Credential.APIKeyFastPolicy,
			access.Credential.Member.UserID,
		)
		if policyErr != nil {
			status := http.StatusInternalServerError
			source := domain.GatewayErrorSourceGateway
			code := "policy_error"
			message := policyErr.Error()
			closeReason := "Responses WebSocket policy evaluation failed"
			if blocked, ok := policyErr.(*openai.FastPolicyBlockedError); ok {
				status = http.StatusForbidden
				source = domain.GatewayErrorSourceRequest
				code = "permission_error"
				message = blocked.Message
				closeReason = blocked.Message
			}
			requestID := connectionRequestID + "-ws-" + strconv.Itoa(request.Turn)
			metric := gatewayErrorMetric(requestID, r.URL.Path, request.Billing.Model, request.Billing, status, source, code, message, time.Since(policyStartedAt))
			metric.IsStream = true
			s.recordGatewayMetric(sessionCtx, access, metric, policyStartedAt)
			writeResponsesWebSocketError(client, s.webSocketConfig.WriteTimeout, source, code, message)
			releaseGatewayAccess(&turnAccess)
			return openai.ResponsesWebSocketTurnConfig{}, openai.NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, closeReason, policyErr)
		}
		dial := &openai.ResponsesWebSocketDialConfig{
			AccessToken: access.AccessToken, ChatGPTAccountID: access.Credential.Account.ChatGPTAccountID,
			APIKeyID: access.Credential.APIKeyID, InternalAccountID: access.Credential.Account.ID,
			FingerprintMode: access.Credential.Account.CodexFingerprintMode, ProxyURL: access.ProxyURL, InboundHeader: r.Header,
			Model: policyBilling.Model, ServiceTier: policyBilling.ServiceTier,
		}
		policyFrame, fingerprintErr := openai.PrepareResponsesWebSocketFingerprint(dial, policyFrame, request.Billing.PromptCacheKey)
		if fingerprintErr != nil {
			releaseGatewayAccess(&turnAccess)
			return openai.ResponsesWebSocketTurnConfig{}, openai.NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "invalid Codex fingerprint metadata", fingerprintErr)
		}
		return openai.ResponsesWebSocketTurnConfig{Frame: policyFrame, Dial: dial}, nil
	}
	retryFirstAccount := func(
		ctx context.Context,
		request openai.ResponsesWebSocketTurnRequest,
		result openai.ResponsesWebSocketTurnResult,
		responseHeaders http.Header,
		metricCode, metricMessage string,
		cause error,
	) (openai.ResponsesWebSocketTurnConfig, error) {
		failedAccess := turnAccess
		turnAccess = application.GatewayAccess{}
		if failedAccess.Credential.APIKeyID == "" {
			return openai.ResponsesWebSocketTurnConfig{}, openai.NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket retry state is unavailable", cause)
		}
		recordedAt := result.StartedAt
		if recordedAt.IsZero() {
			recordedAt = time.Now()
		}
		requestID := connectionRequestID + "-ws-" + strconv.Itoa(request.Turn) + "-attempt-" + strconv.Itoa(turnAttempt)
		metric := gatewayErrorMetric(requestID, r.URL.Path, request.Billing.Model, result.Billing, http.StatusTooManyRequests, domain.GatewayErrorSourceUpstream, metricCode, metricMessage, time.Since(recordedAt))
		metric.IsStream = true
		s.recordGatewayMetric(ctx, failedAccess, metric, recordedAt)
		if err := s.recordGatewayAccountQuota(ctx, failedAccess, responseHeaders, time.Now()); err != nil {
			s.logger.Debug("Responses WebSocket rate limit did not include Codex quota signal", "account_id", failedAccess.Credential.Account.ID)
		}
		failedFirstAccountIDs = append(failedFirstAccountIDs, failedAccess.Credential.Account.ID)
		releaseGatewayAccess(&failedAccess)
		if firstAccountSwitches >= maxUpstreamAccountSwitches {
			return openai.ResponsesWebSocketTurnConfig{}, openai.NewResponsesWebSocketCloseError(websocket.StatusTryAgainLater, "upstream rate limit exceeded, please retry later", cause)
		}
		firstAccountSwitches++
		nextAccess, resolveErr := s.app.ResolveGatewayAccess(ctx, apiKey, failedFirstAccountIDs...)
		if resolveErr != nil {
			return openai.ResponsesWebSocketTurnConfig{}, openai.NewResponsesWebSocketCloseError(websocket.StatusTryAgainLater, "upstream rate limit exceeded, please retry later", resolveErr)
		}
		turnAttempt++
		replacePinnedAfterTurn = true
		return setTurnAccess(nextAccess, request)
	}
	hooks := openai.ResponsesWebSocketHooks{
		BeforeTurn: func(ctx context.Context, request openai.ResponsesWebSocketTurnRequest) (openai.ResponsesWebSocketTurnConfig, error) {
			replacePinnedAfterTurn = false
			turnAttempt = 1
			if request.Turn > 1 {
				if allowed, _ := s.protections.admitAPIKey(apiKeyID); !allowed {
					return openai.ResponsesWebSocketTurnConfig{}, openai.NewResponsesWebSocketCloseError(websocket.StatusTryAgainLater, "API key request rate limit reached", nil)
				}
			}
			turnAccess = application.GatewayAccess{}
			if firstTurn {
				access, resolveErr := s.app.ResolveGatewayAccess(ctx, apiKey, failedFirstAccountIDs...)
				if resolveErr != nil {
					return openai.ResponsesWebSocketTurnConfig{}, responsesWebSocketAccessError(resolveErr)
				}
				return setTurnAccess(access, request)
			}
			access, resolveErr := s.app.ReacquireGatewayAccess(ctx, apiKey, pinned)
			if resolveErr != nil {
				return openai.ResponsesWebSocketTurnConfig{}, responsesWebSocketAccessError(resolveErr)
			}
			return setTurnAccess(access, request)
		},
		OnDialError: func(ctx context.Context, request openai.ResponsesWebSocketTurnRequest, result openai.ResponsesWebSocketTurnResult, dialErr *openai.ResponsesWebSocketDialError) (openai.ResponsesWebSocketTurnConfig, error) {
			if dialErr != nil && dialErr.StatusCode == http.StatusUnauthorized && turnAccess.Credential.APIKeyID != "" {
				if s.refreshRejectedGatewayAccess(ctx, &turnAccess, dialErr.StatusCode, nil, authRefreshed) {
					return setTurnAccess(turnAccess, request)
				}
				recordedAt := result.StartedAt
				if recordedAt.IsZero() {
					recordedAt = time.Now()
				}
				requestID := connectionRequestID + "-ws-" + strconv.Itoa(request.Turn) + "-attempt-" + strconv.Itoa(turnAttempt)
				metric := gatewayErrorMetric(requestID, r.URL.Path, request.Billing.Model, result.Billing, http.StatusUnauthorized, domain.GatewayErrorSourceUpstream, "websocket_handshake_error", dialErr.Error(), time.Since(recordedAt))
				metric.IsStream = true
				s.recordGatewayMetric(ctx, turnAccess, metric, recordedAt)
				return openai.ResponsesWebSocketTurnConfig{}, openai.NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "upstream Responses WebSocket authentication failed", dialErr)
			}
			if dialErr == nil || dialErr.StatusCode != http.StatusTooManyRequests {
				return openai.ResponsesWebSocketTurnConfig{}, openai.NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket handshake failed", dialErr)
			}
			return retryFirstAccount(ctx, request, result, dialErr.ResponseHeaders, "rate_limit_error", dialErr.Error(), dialErr)
		},
		OnUpstreamError: func(ctx context.Context, request openai.ResponsesWebSocketTurnRequest, result openai.ResponsesWebSocketTurnResult, upstreamErr *openai.ResponsesWebSocketUpstreamEventError) (openai.ResponsesWebSocketTurnConfig, error) {
			if upstreamErr == nil {
				return openai.ResponsesWebSocketTurnConfig{}, openai.NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket error is unavailable", nil)
			}
			return retryFirstAccount(ctx, request, result, result.ResponseHeaders, upstreamErr.Code, upstreamErr.Message, upstreamErr)
		},
		AfterTurn: func(ctx context.Context, turn int, result openai.ResponsesWebSocketTurnResult, turnErr error) {
			access := turnAccess
			var shutdownCause responsesWebSocketShutdownCause
			serverShuttingDown := errors.As(context.Cause(sessionCtx), &shutdownCause)
			if result.HandshakeSucceeded && ((turn == 1 && firstTurn) || (replacePinnedAfterTurn && result.TerminalEvent != "")) {
				pinned = access
				pinned.Release = nil
				firstTurn = false
			}
			replacePinnedAfterTurn = false
			releaseGatewayAccess(&turnAccess)
			if access.Credential.APIKeyID == "" {
				return
			}
			recordedAt := result.StartedAt
			if recordedAt.IsZero() {
				recordedAt = time.Now()
			}
			requestID := strings.TrimSpace(result.RequestID)
			if requestID == "" {
				requestID = connectionRequestID + "-ws-" + strconv.Itoa(turn)
			}
			status := http.StatusOK
			if result.Metrics.ErrorStatusCode != 0 {
				status = result.Metrics.ErrorStatusCode
			}
			if turnErr != nil && result.TerminalEvent == "" && status == http.StatusOK && !result.Metrics.ClientDisconnected {
				status = http.StatusBadGateway
			}
			metric := gatewayMetric(requestID, result.Billing.Model, r.URL.Path, result.Billing, status, result.Metrics)
			metric.IsStream = true
			for _, segment := range result.BillingSegments {
				metric.BillingSegments = append(metric.BillingSegments, domain.GatewayBillingSegment{
					TokenUsage: gatewayTokenUsage(segment), WebSearchCalls: segment.WebSearchCalls, ImageSize: segment.ImageSize,
				})
			}
			if serverShuttingDown {
				metric.StatusCode = http.StatusServiceUnavailable
				metric.ErrorSource = domain.GatewayErrorSourceGateway
				metric.ErrorCode = "server_shutting_down"
				metric.ErrorMessage = responsesWebSocketShutdownReason
			} else if result.Metrics.ClientDisconnected {
				metric.StatusCode = clientClosedRequestStatus
				metric.ErrorSource = domain.GatewayErrorSourceRequest
				metric.ErrorCode = "client_disconnected"
				metric.ErrorMessage = "client disconnected before response completed"
			} else if turnErr != nil && metric.ErrorMessage == "" {
				var dialErr *openai.ResponsesWebSocketDialError
				var closeErr *openai.ResponsesWebSocketCloseError
				if errors.As(turnErr, &dialErr) {
					metric.StatusCode = dialErr.StatusCode
					if metric.StatusCode == 0 {
						metric.StatusCode = http.StatusBadGateway
					}
					metric.ErrorSource = domain.GatewayErrorSourceUpstream
					metric.ErrorCode = "websocket_handshake_error"
				} else if errors.As(turnErr, &closeErr) && closeErr.StatusCode() == websocket.StatusPolicyViolation {
					metric.StatusCode = http.StatusBadRequest
					metric.ErrorSource = domain.GatewayErrorSourceRequest
					metric.ErrorCode = "invalid_request_error"
				} else {
					metric.ErrorSource = domain.GatewayErrorSourceUpstream
					metric.ErrorCode = "websocket_error"
				}
				metric.ErrorMessage = turnErr.Error()
			}
			if s.recordGatewayMetric(ctx, access, metric, recordedAt) == nil && result.HandshakeSucceeded && result.TerminalEvent != "" {
				s.recordGatewayUsage(ctx, access, result.ResponseHeaders, requestID, time.Now())
			}
		},
	}
	err = s.responsesWebSocket.Run(sessionCtx, client, firstFrame, hooks)
	if err == nil {
		_ = client.Close(websocket.StatusNormalClosure, "done")
		return
	}
	var shutdownCause responsesWebSocketShutdownCause
	if errors.As(context.Cause(sessionCtx), &shutdownCause) {
		_ = client.Close(websocket.StatusGoingAway, responsesWebSocketShutdownReason)
		return
	}
	var closeErr *openai.ResponsesWebSocketCloseError
	if errors.As(err, &closeErr) {
		_ = client.Close(closeErr.StatusCode(), closeErr.Reason())
		return
	}
	if websocket.CloseStatus(err) != -1 {
		return
	}
	s.logger.Warn("proxy Responses WebSocket", "error", err)
	_ = client.Close(websocket.StatusInternalError, "Responses WebSocket proxy failed")
}

func readResponsesWebSocketFirstFrame(
	controlCtx context.Context,
	client openai.ResponsesWebSocketConn,
	timeout time.Duration,
) (websocket.MessageType, []byte, error) {
	readDone := make(chan responsesWebSocketFirstRead, 1)
	go func() {
		messageType, frame, err := client.Read(context.Background())
		readDone <- responsesWebSocketFirstRead{messageType: messageType, frame: frame, err: err}
	}()

	var timer *time.Timer
	var timeoutCh <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timeoutCh = timer.C
		defer timer.Stop()
	}
	closeAndJoin := func(status websocket.StatusCode, reason string, cause error) (websocket.MessageType, []byte, error) {
		_ = client.Close(status, reason)
		_ = client.CloseNow()
		<-readDone
		return 0, nil, openai.NewResponsesWebSocketCloseError(status, reason, cause)
	}

	select {
	case result := <-readDone:
		return result.messageType, result.frame, result.err
	case <-timeoutCh:
		return closeAndJoin(websocket.StatusPolicyViolation, "missing first response.create message", context.DeadlineExceeded)
	case <-controlCtx.Done():
		reason := "Responses WebSocket session canceled"
		var shutdownCause responsesWebSocketShutdownCause
		if errors.As(context.Cause(controlCtx), &shutdownCause) {
			reason = responsesWebSocketShutdownReason
		}
		return closeAndJoin(websocket.StatusGoingAway, reason, context.Cause(controlCtx))
	}
}

func writeResponsesWebSocketError(client openai.ResponsesWebSocketConn, timeout time.Duration, source, code, message string) {
	if client == nil {
		return
	}
	errorType := "api_error"
	if source == domain.GatewayErrorSourceRequest {
		errorType = "invalid_request_error"
	}
	payload := []byte(`{"type":"error","error":{"type":` + strconv.Quote(errorType) + `,"code":` + strconv.Quote(code) + `,"message":` + strconv.Quote(message) + `}}`)
	ctx, cancel := contextWithPositiveTimeout(context.Background(), timeout)
	defer cancel()
	_ = client.Write(ctx, websocket.MessageText, payload)
}

func responsesWebSocketTurnConfig(access application.GatewayAccess, frame []byte, inboundHeader http.Header) openai.ResponsesWebSocketTurnConfig {
	metadata, _ := openai.ParseRequestBilling(frame)
	return openai.ResponsesWebSocketTurnConfig{
		Frame: frame,
		Dial: &openai.ResponsesWebSocketDialConfig{
			AccessToken: access.AccessToken, ChatGPTAccountID: access.Credential.Account.ChatGPTAccountID,
			APIKeyID: access.Credential.APIKeyID, InternalAccountID: access.Credential.Account.ID,
			FingerprintMode: access.Credential.Account.CodexFingerprintMode, ProxyURL: access.ProxyURL, InboundHeader: inboundHeader,
			Model: metadata.Model, ServiceTier: metadata.ServiceTier,
		},
	}
}

func contextWithPositiveTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func isWebSocketUpgrade(r *http.Request) bool {
	return headerHasToken(r.Header, "Connection", "upgrade") && headerHasToken(r.Header, "Upgrade", "websocket")
}

func headerHasToken(headers http.Header, name, token string) bool {
	for _, value := range headers.Values(name) {
		for _, candidate := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(candidate), token) {
				return true
			}
		}
	}
	return false
}

func responsesWebSocketAccessError(err error) error {
	if errors.Is(err, domain.ErrAccountConcurrency) || errors.Is(err, domain.ErrAccountRateLimited) ||
		errors.Is(err, domain.ErrQuotaExhausted) || errors.Is(err, domain.ErrAccountUnavailable) ||
		errors.Is(err, domain.ErrNoRouteAvailable) {
		return openai.NewResponsesWebSocketCloseError(websocket.StatusTryAgainLater, "account is unavailable for this turn; please reconnect", err)
	}
	if errors.Is(err, domain.ErrUnauthorized) {
		return openai.NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "API key is no longer authorized", err)
	}
	return err
}
