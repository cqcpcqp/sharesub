package openai

import (
	"context"
	"encoding/json"
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
	initialFrame = nil
	var upstream ResponsesWebSocketConn
	var upstreamReads chan responsesWebSocketRead
	var upstreamReaderStop chan struct{}
	var upstreamReaderDone chan struct{}
	var pinnedDial ResponsesWebSocketDialConfig
	var pendingFirstDownstreamControls responsesWebSocketPendingClientFrames
	replayHistory := newResponsesWebSocketReplayHistory(s.replayBudget)
	currentReplayReservation := newResponsesWebSocketReplayReservation(s.replayBudget)
	outputReplayReservation := newResponsesWebSocketReplayReservation(s.replayBudget)
	defer func() {
		currentReplayReservation.release()
		outputReplayReservation.release()
		replayHistory.invalidate()
	}()
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
		currentReplayReservation.release()
		outputReplayReservation.release()
		startedAt := time.Now()
		inheritedModel := loadResponsesWebSocketSessionModel(&sessionModel)
		requestFrame, billing, previousResponseID, err := parseResponsesWebSocketFrame(frame, turn, inheritedModel)
		// parseResponsesWebSocketFrame returns an independent normalized payload.
		// Drop the client frame immediately so one near the inbound read limit does
		// not pin its large backing array for the lifetime of the session.
		frame = nil
		result := ResponsesWebSocketTurnResult{StartedAt: startedAt, Billing: billing}
		if err != nil {
			return err
		}
		turnRequest := ResponsesWebSocketTurnRequest{
			Turn: turn, Frame: requestFrame, Billing: billing, PreviousResponseID: previousResponseID,
		}
		if strings.TrimSpace(previousResponseID) == "" {
			// An independent response replaces the durable replay chain. Releasing
			// it before reserving the new input prevents obsolete history from
			// denying a replacement that would fit on its own.
			replayHistory.invalidate()
		}
		var currentInput []json.RawMessage
		var replayPlan responsesWebSocketReplayPlan
		retrySafe := false
		replayCommitSafe := false
		requestFrameBytes := int64(len(requestFrame))
		if sessionReplaySafe && requestFrameBytes <= responsesWebSocketReplayHistoryMaxBytes && currentReplayReservation.resize(requestFrameBytes) {
			var currentInputExists, currentInputReplayable bool
			var inputErr error
			currentInput, currentInputExists, currentInputReplayable, inputErr = responsesWebSocketInputItems(requestFrame)
			if inputErr != nil {
				return NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "invalid response.create input", inputErr)
			}
			currentInputBytes := responsesWebSocketRawMessagesBytes(currentInput)
			if !responsesWebSocketReplayLimitExceeded(len(currentInput), currentInputBytes) && currentReplayReservation.resize(currentInputBytes) {
				replayPlan, retrySafe, replayCommitSafe = replayHistory.plan(previousResponseID, currentInput, currentInputExists, currentInputReplayable)
			}
		}
		if !retrySafe && !replayCommitSafe {
			currentReplayReservation.release()
			currentInput = nil
		}
		if !replayCommitSafe {
			// Budget denial and non-reconstructable turns fail closed for future
			// replay while the current request continues on its pinned account.
			replayHistory.invalidate()
		}
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
					retryUnauthorized := dialErr.StatusCode == http.StatusUnauthorized && dialHook != nil
					retryRateLimit := dialErr.StatusCode == http.StatusTooManyRequests && retrySafe && dialHook != nil
					if retryUnauthorized || retryRateLimit {
						hookRequest := turnRequest
						if retryTurnRequestPrepared {
							hookRequest = retryTurnRequest
						}
						if retryRateLimit {
							if prepareErr := prepareRetryTurnRequest(); prepareErr != nil {
								if turnStarted {
									callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, prepareErr)
								}
								return prepareErr
							}
							hookRequest = retryTurnRequest
						}
						nextConfig, retryErr := dialHook(ctx, hookRequest, result, dialErr)
						if retryErr != nil {
							return normalizeResponsesWebSocketHookError(retryErr)
						}
						if len(nextConfig.Frame) == 0 {
							nextConfig.Frame = hookRequest.Frame
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
						if retryUnauthorized {
							if retryErr = verifyPinnedResponsesWebSocketDial(pinnedDial, *nextConfig.Dial); retryErr != nil {
								retryErr = NewResponsesWebSocketCloseError(websocket.StatusInternalError, "Responses WebSocket authentication retry changed account binding", retryErr)
								if turnStarted {
									callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, retryErr)
								}
								return retryErr
							}
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
			outputReplayReservation.release()
			relayReplayState := responsesWebSocketRelayReplayState{
				history:            &replayHistory,
				currentInput:       &currentInput,
				currentReservation: &currentReplayReservation,
				outputReservation:  &outputReplayReservation,
				collect:            replayCommitSafe,
			}
			result, err = s.relayTurn(
				ctx, client, upstream, clientReads, upstreamReads, result, turnLifecycle, &sessionModel,
				&sessionReplaySafe, allowUpstreamErrorRetry, &pendingFirstDownstreamControls,
				&relayReplayState,
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
					replayHistory.commit(
						result.RequestID, replayPlan, currentInput, result.replayOutput,
						replayCommitSafe && sessionReplaySafe && !result.replayOutputExceedsLimit,
						&currentReplayReservation, &outputReplayReservation,
					)
				} else {
					replayHistory.invalidate()
					currentReplayReservation.release()
					outputReplayReservation.release()
				}
			}
			currentReplayReservation.release()
			outputReplayReservation.release()
			currentInput = nil
			result.replayOutput = nil
			if turnStarted {
				callResponsesWebSocketAfterTurn(ctx, hooks, turn, result, err)
			}
			requestFrame = nil
			turnRequest.Frame = nil
			retryTurnRequest.Frame = nil
			turnConfig.Frame = nil
			normalized = nil
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
					frame = read.frame
					read.frame = nil
					break
				}
				updateResponsesWebSocketSessionModel(&sessionModel, read.messageType, read.frame)
				if !responsesWebSocketControlPreservesReplaySafety(read.messageType, read.frame) {
					sessionReplaySafe = false
					replayHistory.invalidate()
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
