package openai

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

func (s *ResponsesWebSocketSession) Run(ctx context.Context, client ResponsesWebSocketConn, initialFrame []byte, hooks ResponsesWebSocketHooks) error {
	if s == nil || client == nil {
		return NewResponsesWebSocketCloseError(websocket.StatusInternalError, "Responses WebSocket session is unavailable", errors.New("nil session or client connection"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sessionDone := make(chan struct{})
	defer close(sessionDone)
	turnLifecycle := newResponsesWebSocketTurnLifecycle(true)
	clientReads := make(chan responsesWebSocketRead, 1)
	go readResponsesWebSocketFrames(client, clientReads, sessionDone, turnLifecycle)

	frame := append([]byte(nil), initialFrame...)
	var upstream ResponsesWebSocketConn
	var upstreamReads chan responsesWebSocketRead
	var upstreamReaderStop chan struct{}
	var upstreamReaderDone chan struct{}
	var pinnedDial ResponsesWebSocketDialConfig
	var pendingFirstDownstreamControls responsesWebSocketPendingClientFrames
	var replayHistory responsesWebSocketReplayHistory
	sessionReplaySafe := true
	// sessionModel follows the model negotiated by the client. Responses WS v2
	// permits session.update to change session.model between turns, and a later
	// response.create may omit model. A response.create model applies only to
	// that turn; it must not replace the session-level fallback.
	var sessionModel atomic.Pointer[string]
	closeUpstream := func() {
		if upstreamReaderStop != nil {
			close(upstreamReaderStop)
			upstreamReaderStop = nil
		}
		if upstream != nil {
			// A graceful coder/websocket Close can wait several seconds for a
			// peer handshake. A failed account must be torn down before its
			// reader is joined, otherwise first-output failover can stall.
			_ = upstream.CloseNow()
			upstream = nil
		}
		if upstreamReaderDone != nil {
			<-upstreamReaderDone
			upstreamReaderDone = nil
		}
		upstreamReads = nil
	}
	defer closeUpstream()

	for turn := 1; ; turn++ {
		startedAt := time.Now()
		inheritedModel := loadResponsesWebSocketSessionModel(&sessionModel)
		requestFrame, billing, previousResponseID, err := parseResponsesWebSocketFrame(frame, turn, inheritedModel)
		result := ResponsesWebSocketTurnResult{StartedAt: startedAt, Billing: billing}
		if err != nil {
			return err
		}
		turnRequest := ResponsesWebSocketTurnRequest{
			Turn: turn, Frame: requestFrame, Billing: billing, PreviousResponseID: previousResponseID,
		}
		currentInput, currentInputExists, currentInputReplayable, inputErr := responsesWebSocketInputItems(requestFrame)
		if inputErr != nil {
			return NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "invalid response.create input", inputErr)
		}
		replayPlan, retrySafe, replayCommitSafe := replayHistory.plan(previousResponseID, currentInput, currentInputExists, currentInputReplayable)
		retrySafe = retrySafe && sessionReplaySafe
		retryTurnRequest := turnRequest
		retryTurnRequestPrepared := strings.TrimSpace(previousResponseID) == ""
		prepareRetryTurnRequest := func() error {
			if retryTurnRequestPrepared {
				return nil
			}
			fullReplayInput := replayPlan.input(replayHistory.items, currentInput)
			rebuilt, safe, rebuildErr := buildResponsesWebSocketRetryFrame(requestFrame, fullReplayInput)
			if rebuildErr != nil {
				return NewResponsesWebSocketCloseError(websocket.StatusInternalError, "failed to rebuild Responses WebSocket conversation", rebuildErr)
			}
			if !safe {
				return NewResponsesWebSocketCloseError(websocket.StatusInternalError, "Responses WebSocket conversation is not safe to replay", nil)
			}
			retryTurnRequest.Frame = rebuilt
			retryTurnRequest.PreviousResponseID = ""
			retryTurnRequestPrepared = true
			return nil
		}
		turnConfig := ResponsesWebSocketTurnConfig{Frame: requestFrame}
		turnStarted := false
		if hooks.BeforeTurn != nil {
			turnConfig, err = hooks.BeforeTurn(ctx, turnRequest)
			if err != nil {
				return normalizeResponsesWebSocketHookError(err)
			}
			turnStarted = true
		}
		if len(turnConfig.Frame) == 0 {
			turnConfig.Frame = requestFrame
		}
		normalized, normalizedBilling, _, err := prepareResponsesWebSocketFrame(turnConfig.Frame, turn, inheritedModel)
		result.Billing = normalizedBilling
		if err != nil {
			if turnStarted {
				callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, err)
			}
			return err
		}
		if turn == 1 {
			storeResponsesWebSocketSessionModel(&sessionModel, normalizedBilling.Model)
		}
		for {
			if upstream == nil {
				if turnConfig.Dial == nil {
					err = NewResponsesWebSocketCloseError(websocket.StatusInternalError, "Responses WebSocket credentials are unavailable", errors.New("first turn did not provide upstream credentials"))
					if turnStarted {
						callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, err)
					}
					return err
				}
				if err = validateResponsesWebSocketDialConfig(*turnConfig.Dial); err != nil {
					err = NewResponsesWebSocketCloseError(websocket.StatusInternalError, "Responses WebSocket credentials are invalid", err)
					if turnStarted {
						callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, err)
					}
					return err
				}
				for {
					pinnedDial = cloneResponsesWebSocketDialConfig(*turnConfig.Dial)
					headers, headerErr := responsesWebSocketHeaders(*turnConfig.Dial, result.Billing.PromptCacheKey)
					if headerErr != nil {
						err = NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "invalid Codex fingerprint metadata", headerErr)
						if turnStarted {
							callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, err)
						}
						return err
					}
					dialCtx, cancelDial := context.WithTimeout(ctx, s.dialTimeout)
					var handshakeHeaders http.Header
					var dialStatus int
					upstream, dialStatus, handshakeHeaders, err = s.dialer.Dial(dialCtx, s.targetURL, headers, turnConfig.Dial.ProxyURL)
					cancelDial()
					if err == nil {
						result.HandshakeSucceeded = true
						result.ResponseHeaders = cloneWebSocketHeader(handshakeHeaders)
						break
					}

					dialErr := normalizeResponsesWebSocketDialError(err, dialStatus, handshakeHeaders)
					dialHook := hooks.OnDialError
					if dialHook == nil && turn == 1 {
						dialHook = hooks.OnFirstDialError
					}
					if retrySafe && (dialErr.StatusCode == http.StatusTooManyRequests || dialErr.StatusCode == http.StatusUnauthorized) && dialHook != nil {
						if prepareErr := prepareRetryTurnRequest(); prepareErr != nil {
							if turnStarted {
								callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, prepareErr)
							}
							return prepareErr
						}
						nextConfig, retryErr := dialHook(ctx, retryTurnRequest, result, dialErr)
						if retryErr != nil {
							return normalizeResponsesWebSocketHookError(retryErr)
						}
						if len(nextConfig.Frame) == 0 {
							nextConfig.Frame = retryTurnRequest.Frame
						}
						nextNormalized, nextBilling, _, prepareErr := prepareResponsesWebSocketFrame(nextConfig.Frame, turn, "")
						if prepareErr != nil {
							if turnStarted {
								callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, prepareErr)
							}
							return prepareErr
						}
						if nextConfig.Dial == nil {
							retryErr = NewResponsesWebSocketCloseError(websocket.StatusInternalError, "Responses WebSocket credentials are unavailable", errors.New("dial retry did not provide upstream credentials"))
							if turnStarted {
								callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, retryErr)
							}
							return retryErr
						}
						if retryErr = validateResponsesWebSocketDialConfig(*nextConfig.Dial); retryErr != nil {
							retryErr = NewResponsesWebSocketCloseError(websocket.StatusInternalError, "Responses WebSocket credentials are invalid", retryErr)
							if turnStarted {
								callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, retryErr)
							}
							return retryErr
						}
						turnConfig = nextConfig
						normalized = nextNormalized
						result = ResponsesWebSocketTurnResult{StartedAt: time.Now(), Billing: nextBilling}
						inheritedModel = nextBilling.Model
						continue
					}

					err = responsesWebSocketDialCloseError(dialErr)
					if turnStarted {
						callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, err)
					}
					return err
				}
				if upstream == nil {
					err = NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket handshake failed", errors.New("dialer returned a nil connection"))
					if turnStarted {
						callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, err)
					}
					return err
				}
				upstreamReads = make(chan responsesWebSocketRead, 1)
				upstreamReaderStop = make(chan struct{})
				upstreamReaderDone = make(chan struct{})
				go func(conn ResponsesWebSocketConn, output chan<- responsesWebSocketRead, stop <-chan struct{}, done chan<- struct{}) {
					defer close(done)
					readResponsesWebSocketFrames(conn, output, stop, nil)
				}(upstream, upstreamReads, upstreamReaderStop, upstreamReaderDone)
			} else if turnConfig.Dial != nil {
				if err = verifyPinnedResponsesWebSocketDial(pinnedDial, *turnConfig.Dial); err != nil {
					err = NewResponsesWebSocketCloseError(websocket.StatusTryAgainLater, "account binding changed; please reconnect", err)
					if turnStarted {
						callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, err)
					}
					return err
				}
			}

			writeCtx, cancelWrite := context.WithTimeout(ctx, s.writeTimeout)
			err = upstream.Write(writeCtx, websocket.MessageText, normalized)
			cancelWrite()
			if err != nil {
				err = NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket write failed", err)
				if turnStarted {
					callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, err)
				}
				return err
			}

			upstreamErrorHook := hooks.OnUpstreamError
			if upstreamErrorHook == nil && turn == 1 {
				upstreamErrorHook = hooks.OnFirstUpstreamError
			}
			allowUpstreamErrorRetry := retrySafe && upstreamErrorHook != nil
			result, err = s.relayTurn(
				ctx, client, upstream, clientReads, upstreamReads, result, turnLifecycle, &sessionModel,
				&sessionReplaySafe, allowUpstreamErrorRetry, &pendingFirstDownstreamControls,
			)
			var firstUpstreamErr *responsesWebSocketFirstUpstreamError
			if errors.As(err, &firstUpstreamErr) && firstUpstreamErr != nil && upstreamErrorHook != nil {
				closeUpstream()
				if prepareErr := prepareRetryTurnRequest(); prepareErr != nil {
					if turnStarted {
						callResponsesWebSocketAfterTurn(ctx, hooks, turn, firstUpstreamErr.result, prepareErr)
					}
					return prepareErr
				}
				nextConfig, retryErr := upstreamErrorHook(ctx, retryTurnRequest, firstUpstreamErr.result, firstUpstreamErr.upstream)
				if retryErr != nil {
					return normalizeResponsesWebSocketHookError(retryErr)
				}
				retryResult := ResponsesWebSocketTurnResult{StartedAt: time.Now(), Billing: turnRequest.Billing}
				if len(nextConfig.Frame) == 0 {
					nextConfig.Frame = retryTurnRequest.Frame
				}
				nextNormalized, nextBilling, _, prepareErr := prepareResponsesWebSocketFrame(nextConfig.Frame, turn, "")
				if prepareErr != nil {
					if turnStarted {
						callResponsesWebSocketAfterTurn(ctx, hooks, turn, retryResult, prepareErr)
					}
					return prepareErr
				}
				retryResult.Billing = nextBilling
				if nextConfig.Dial == nil {
					retryErr = NewResponsesWebSocketCloseError(websocket.StatusInternalError, "Responses WebSocket credentials are unavailable", errors.New("upstream error retry did not provide upstream credentials"))
					if turnStarted {
						callResponsesWebSocketAfterTurn(ctx, hooks, turn, retryResult, retryErr)
					}
					return retryErr
				}
				if retryErr = validateResponsesWebSocketDialConfig(*nextConfig.Dial); retryErr != nil {
					retryErr = NewResponsesWebSocketCloseError(websocket.StatusInternalError, "Responses WebSocket credentials are invalid", retryErr)
					if turnStarted {
						callResponsesWebSocketAfterTurn(ctx, hooks, turn, retryResult, retryErr)
					}
					return retryErr
				}
				turnConfig = nextConfig
				normalized = nextNormalized
				result = retryResult
				inheritedModel = nextBilling.Model
				continue
			}
			if result.TerminalEvent != "" {
				if err == nil && isSuccessfulResponsesWebSocketTerminalEvent(result.TerminalEvent) {
					replayHistory.commit(result.RequestID, replayPlan, currentInput, result.replayOutput, replayCommitSafe && !result.replayOutputExceedsLimit)
				} else {
					replayHistory.invalidate()
				}
			}
			if turnStarted {
				callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, err)
			}
			if err != nil {
				var terminalErr *ResponsesWebSocketTerminalError
				if !errors.As(err, &terminalErr) {
					return err
				}
			}
			break
		}

		idleTimer := time.NewTimer(s.interTurnIdleTimeout)
		for {
			select {
			case read := <-clientReads:
				if read.err != nil {
					stopResponsesWebSocketTimer(idleTimer)
					return read.err
				}
				if isResponsesWebSocketResponseCreate(read.messageType, read.frame) {
					stopResponsesWebSocketTimer(idleTimer)
					if read.duringTurn {
						return NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "overlapping response.create is not supported", nil)
					}
					frame = append(frame[:0], read.frame...)
					break
				}
				updateResponsesWebSocketSessionModel(&sessionModel, read.messageType, read.frame)
				if !responsesWebSocketControlPreservesReplaySafety(read.messageType, read.frame) {
					sessionReplaySafe = false
				}
				if err := s.writeUpstreamFrame(ctx, upstream, read.messageType, read.frame); err != nil {
					stopResponsesWebSocketTimer(idleTimer)
					return NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket control write failed", err)
				}
				resetResponsesWebSocketTimer(idleTimer, s.interTurnIdleTimeout)
				continue
			case read := <-upstreamReads:
				stopResponsesWebSocketTimer(idleTimer)
				if read.err != nil {
					return NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket closed between turns", read.err)
				}
				return NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket sent an event between turns", nil)
			case <-idleTimer.C:
				return NewResponsesWebSocketCloseError(websocket.StatusNormalClosure, "websocket idle timeout", context.DeadlineExceeded)
			case <-ctx.Done():
				stopResponsesWebSocketTimer(idleTimer)
				return NewResponsesWebSocketCloseError(websocket.StatusGoingAway, "Responses WebSocket session canceled", context.Cause(ctx))
			}
			break
		}
	}
}

func (s *ResponsesWebSocketSession) relayTurn(
	ctx context.Context,
	client ResponsesWebSocketConn,
	upstream ResponsesWebSocketConn,
	clientReads, upstreamReads <-chan responsesWebSocketRead,
	result ResponsesWebSocketTurnResult,
	turnLifecycle *responsesWebSocketTurnLifecycle,
	sessionModel *atomic.Pointer[string],
	sessionReplaySafe *bool,
	allowUpstreamErrorRetry bool,
	pendingFirstDownstreamControls *responsesWebSocketPendingClientFrames,
) (ResponsesWebSocketTurnResult, error) {
	activeClientReads := clientReads
	var firstTokenAt time.Time
	var terminal terminalResponse
	var pendingError *responseError
	var replayCollector responsesWebSocketReplayCollector
	var drainTimer *time.Timer
	var drainTimeout <-chan time.Time
	clientDisconnected := false
	wroteDownstream := false
	var clientErr error
	firstOutputDeadline := time.Now().Add(s.firstOutputTimeout)
	readTimer := time.NewTimer(s.firstOutputTimeout)
	defer func() {
		stopResponsesWebSocketTimer(readTimer)
		stopResponsesWebSocketTimer(drainTimer)
	}()

	applyPendingError := func() {
		if terminal.Error == nil && pendingError != nil {
			pendingCopy := *pendingError
			terminal.Error = &pendingCopy
		}
	}
	finishMetrics := func() {
		applyPendingError()
		result.Metrics = proxyMetrics(result.StartedAt, time.Time{}, firstTokenAt, terminal, clientDisconnected)
		if clientDisconnected {
			result.Metrics.ErrorStatusCode = 499
			result.Metrics.ErrorCode = "client_disconnected"
			result.Metrics.ErrorMessage = "client disconnected before response completed"
		}
	}
	markClientDisconnected := func(err error) {
		if clientDisconnected {
			return
		}
		clientDisconnected = true
		clientErr = err
		clientReads = nil
		activeClientReads = nil
		stopResponsesWebSocketTimer(readTimer)
		drainTimer = time.NewTimer(s.upstreamDrainTimeout)
		drainTimeout = drainTimer.C
	}
	for {
		select {
		case read := <-activeClientReads:
			if read.err != nil {
				markClientDisconnected(read.err)
				continue
			}
			if isResponsesWebSocketResponseCreate(read.messageType, read.frame) {
				finishMetrics()
				return result, NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "overlapping response.create is not supported", nil)
			}
			updateResponsesWebSocketSessionModel(sessionModel, read.messageType, read.frame)
			if sessionReplaySafe != nil && !responsesWebSocketControlPreservesReplaySafety(read.messageType, read.frame) {
				*sessionReplaySafe = false
			}
			if allowUpstreamErrorRetry && !wroteDownstream {
				if isResponsesWebSocketResponseCancel(read.messageType, read.frame) {
					// Cancellation targets the current execution and cannot follow a
					// replay to another account. Commit this attempt, preserve FIFO
					// for earlier controls, and deliver the cancel immediately.
					allowUpstreamErrorRetry = false
					if err := pendingFirstDownstreamControls.flush(ctx, upstream, s.writeTimeout); err != nil {
						finishMetrics()
						return result, NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket queued control write failed", err)
					}
					if err := s.writeUpstreamFrame(ctx, upstream, read.messageType, read.frame); err != nil {
						finishMetrics()
						return result, NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket cancel write failed", err)
					}
					continue
				}
				if err := pendingFirstDownstreamControls.append(read); err != nil {
					finishMetrics()
					return result, err
				}
				continue
			}
			if err := s.writeUpstreamFrame(ctx, upstream, read.messageType, read.frame); err != nil {
				finishMetrics()
				return result, NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket control write failed", err)
			}
		case read := <-upstreamReads:
			if read.err != nil {
				finishMetrics()
				if clientDisconnected {
					return result, clientErr
				}
				return result, NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket read failed", read.err)
			}
			if read.messageType != websocket.MessageText && read.messageType != websocket.MessageBinary {
				finishMetrics()
				return result, NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket returned an unsupported message type", nil)
			}
			eventType := ""
			terminalEvent := false
			if read.messageType == websocket.MessageText {
				var responseID string
				var parsedTerminal *terminalResponse
				eventType, responseID, parsedTerminal = observeResponsesWebSocketEvent(read.frame)
				replayCollector.addEvent(eventType, read.frame)
				if result.RequestID == "" && responseID != "" {
					result.RequestID = responseID
				}
				if firstTokenAt.IsZero() && isResponsesWebSocketTokenEvent(read.frame, eventType) {
					firstTokenAt = time.Now()
				}
				if parsedTerminal != nil {
					terminal = *parsedTerminal
				}
				if eventType == "error" {
					if upstreamEventErr := parseResponsesWebSocketErrorEvent(read.frame); upstreamEventErr != nil {
						pendingError = &responseError{
							Type: upstreamEventErr.Type, Code: upstreamEventErr.Code, Message: upstreamEventErr.Message,
						}
						if allowUpstreamErrorRetry && !wroteDownstream && !clientDisconnected && isResponsesWebSocketRateLimitError(upstreamEventErr) {
							finishMetrics()
							result.Metrics.ErrorStatusCode = http.StatusTooManyRequests
							return result, &responsesWebSocketFirstUpstreamError{result: result, upstream: upstreamEventErr}
						}
					}
				}
				terminalEvent = isTerminalResponseEvent(eventType)
			}
			if !clientDisconnected {
				if terminalEvent {
					turnLifecycle.beginTerminalWrite()
				}
				writeCtx, cancelWrite := context.WithTimeout(context.Background(), s.writeTimeout)
				writeErr := client.Write(writeCtx, read.messageType, read.frame)
				cancelWrite()
				if terminalEvent {
					turnLifecycle.finishTerminalWrite(writeErr == nil)
				}
				if writeErr != nil {
					markClientDisconnected(writeErr)
				} else {
					wroteDownstream = true
					if err := pendingFirstDownstreamControls.flush(ctx, upstream, s.writeTimeout); err != nil {
						finishMetrics()
						return result, NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket queued control write failed", err)
					}
				}
			}
			if terminalEvent {
				result.replayOutput = replayCollector.items
				result.replayOutputExceedsLimit = replayCollector.exceedsLimit
				result.TerminalEvent = normalizeResponsesWebSocketTerminalEvent(eventType)
				applyPendingError()
				applyResponsesWebSocketTerminalFailure(eventType, &terminal)
				finishMetrics()
				if !clientDisconnected && !isSuccessfulResponsesWebSocketTerminalEvent(eventType) {
					return result, newResponsesWebSocketTerminalError(eventType, terminal.Error)
				}
				return result, clientErr
			}
			if !clientDisconnected {
				readTimeout := time.Until(firstOutputDeadline)
				if !firstTokenAt.IsZero() {
					readTimeout = s.readTimeout
				} else if readTimeout <= 0 {
					finishMetrics()
					return result, NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket read timeout", context.DeadlineExceeded)
				}
				resetResponsesWebSocketTimer(readTimer, readTimeout)
			}
		case <-readTimer.C:
			finishMetrics()
			return result, NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket read timeout", context.DeadlineExceeded)
		case <-drainTimeout:
			drainTimeout = nil
			finishMetrics()
			return result, clientErr
		case <-ctx.Done():
			finishMetrics()
			if clientDisconnected {
				return result, clientErr
			}
			return result, NewResponsesWebSocketCloseError(websocket.StatusGoingAway, "Responses WebSocket session canceled", context.Cause(ctx))
		}
	}
}

func (s *ResponsesWebSocketSession) writeUpstreamFrame(ctx context.Context, upstream ResponsesWebSocketConn, messageType websocket.MessageType, frame []byte) error {
	if upstream == nil {
		return errors.New("upstream Responses WebSocket connection is unavailable")
	}
	writeCtx, cancelWrite := context.WithTimeout(ctx, s.writeTimeout)
	defer cancelWrite()
	return upstream.Write(writeCtx, messageType, frame)
}

func newResponsesWebSocketTerminalError(eventType string, terminalErr *responseError) error {
	err := &ResponsesWebSocketTerminalError{Event: normalizeResponsesWebSocketTerminalEvent(eventType)}
	if terminalErr != nil {
		err.UpstreamError = &ResponsesWebSocketUpstreamEventError{
			Type: terminalErr.Type, Code: terminalErr.Code, Message: terminalErr.Message,
		}
	}
	return err
}

func applyResponsesWebSocketTerminalFailure(eventType string, terminal *terminalResponse) {
	if terminal == nil || terminal.Error != nil || isSuccessfulResponsesWebSocketTerminalEvent(eventType) {
		return
	}
	normalized := normalizeResponsesWebSocketTerminalEvent(eventType)
	terminal.Error = &responseError{
		Type:    "server_error",
		Code:    strings.ReplaceAll(normalized, ".", "_"),
		Message: "Upstream Responses WebSocket ended with " + normalized,
	}
}

func isSuccessfulResponsesWebSocketTerminalEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "response.completed", "response.done":
		return true
	default:
		return false
	}
}

func callResponsesWebSocketAfterTurn(ctx context.Context, hooks ResponsesWebSocketHooks, turn int, result ResponsesWebSocketTurnResult, err error) {
	if hooks.AfterTurn != nil {
		if result.Metrics.Duration == 0 {
			result.Metrics.Duration = time.Since(result.StartedAt)
		}
		hooks.AfterTurn(context.WithoutCancel(ctx), turn, result, err)
	}
}

func normalizeResponsesWebSocketHookError(err error) error {
	var closeErr *ResponsesWebSocketCloseError
	if errors.As(err, &closeErr) {
		return err
	}
	return NewResponsesWebSocketCloseError(websocket.StatusInternalError, "Responses WebSocket turn setup failed", err)
}

func readResponsesWebSocketFrames(conn ResponsesWebSocketConn, output chan<- responsesWebSocketRead, done <-chan struct{}, turnLifecycle *responsesWebSocketTurnLifecycle) {
	for {
		messageType, frame, err := conn.Read(context.Background())
		read := responsesWebSocketRead{messageType: messageType, frame: frame, err: err}
		if err == nil && turnLifecycle != nil && isResponsesWebSocketResponseCreate(messageType, frame) {
			read.duringTurn = !turnLifecycle.beginResponseCreate()
		}
		select {
		case output <- read:
		case <-done:
			return
		}
		if err != nil {
			return
		}
	}
}

func stopResponsesWebSocketTimer(timer *time.Timer) {
	if timer == nil || timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

func resetResponsesWebSocketTimer(timer *time.Timer, timeout time.Duration) {
	stopResponsesWebSocketTimer(timer)
	timer.Reset(timeout)
}
