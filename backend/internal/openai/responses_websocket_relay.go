package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type responsesWebSocketRelayReplayState struct {
	history            *responsesWebSocketReplayHistory
	currentInput       *[]json.RawMessage
	currentReservation *responsesWebSocketReplayReservation
	outputReservation  *responsesWebSocketReplayReservation
	collect            bool
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
	replayState ...*responsesWebSocketRelayReplayState,
) (ResponsesWebSocketTurnResult, error) {
	var activeReplayState *responsesWebSocketRelayReplayState
	if len(replayState) > 0 {
		activeReplayState = replayState[0]
	}
	activeClientReads := clientReads
	var firstTokenAt time.Time
	var terminal terminalResponse
	var pendingError *responseError
	var replayCollector responsesWebSocketReplayCollector
	if activeReplayState != nil {
		if activeReplayState.collect {
			replayCollector.reservation = activeReplayState.outputReservation
		} else {
			replayCollector.disabled = true
		}
	}
	retainReplayOutput := false
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
		if !retainReplayOutput {
			replayCollector.release()
		}
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
			controlReplaySafe := responsesWebSocketControlPreservesReplaySafety(read.messageType, read.frame)
			if !controlReplaySafe {
				if sessionReplaySafe != nil {
					*sessionReplaySafe = false
				}
				if activeReplayState != nil && activeReplayState.history != nil {
					activeReplayState.history.invalidate()
				}
				if activeReplayState != nil {
					if activeReplayState.currentInput != nil {
						*activeReplayState.currentInput = nil
					}
					if activeReplayState.currentReservation != nil {
						activeReplayState.currentReservation.release()
					}
					activeReplayState.collect = false
				}
				replayCollector.disable()
				if allowUpstreamErrorRetry && !wroteDownstream {
					allowUpstreamErrorRetry = false
					if err := pendingFirstDownstreamControls.flush(ctx, upstream, s.writeTimeout); err != nil {
						finishMetrics()
						return result, NewResponsesWebSocketCloseError(websocket.StatusInternalError, "upstream Responses WebSocket queued control write failed", err)
					}
				}
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
				retainReplayOutput = activeReplayState != nil && activeReplayState.collect && activeReplayState.outputReservation != nil
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
