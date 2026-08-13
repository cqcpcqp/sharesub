package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	responsesWebSocketURL     = "wss://chatgpt.com/backend-api/codex/responses"
	responsesWebSocketBetaV2  = "responses_websockets=2026-02-06"
	defaultWSDialTimeout      = 10 * time.Second
	defaultWSReadTimeout      = 15 * time.Minute
	defaultWSWriteTimeout     = 2 * time.Minute
	defaultWSInterTurnTimeout = 5 * time.Minute
	defaultWSDrainTimeout     = 1200 * time.Millisecond
	defaultWSUpstreamLimit    = 16 << 20
	maxWebSocketCloseReason   = 123
	wsProxyMaxIdleConns       = 128
	wsProxyMaxIdlePerHost     = 64
	wsProxyIdleConnTimeout    = 90 * time.Second
	wsProxyClientCacheLimit   = 256
	wsProxyClientCacheTTL     = 15 * time.Minute
	// Before the first downstream frame, controls must stay off an account
	// that may still be replaced. Keep that temporary queue bounded while the
	// single client reader remains active so disconnects are observed promptly.
	wsPreDownstreamQueueMaxFrames = 64
	wsPreDownstreamQueueMaxBytes  = 4 << 20
)

var responsesWebSocketMessageIDPattern = regexp.MustCompile(`(?i)^(msg|message|item|chatcmpl)_[A-Za-z0-9_-]{1,256}$`)

type ResponsesWebSocketConn interface {
	Read(context.Context) (websocket.MessageType, []byte, error)
	Write(context.Context, websocket.MessageType, []byte) error
	Close(websocket.StatusCode, string) error
	CloseNow() error
}

type ResponsesWebSocketDialer interface {
	Dial(context.Context, string, http.Header, string) (ResponsesWebSocketConn, int, http.Header, error)
}

type ResponsesWebSocketDialConfig struct {
	AccessToken      string
	ChatGPTAccountID string
	APIKeyID         string
	ProxyURL         string
	InboundHeader    http.Header
}

type ResponsesWebSocketTurnRequest struct {
	Turn               int
	Frame              []byte
	Billing            RequestBilling
	PreviousResponseID string
}

type ResponsesWebSocketTurnConfig struct {
	Frame []byte
	Dial  *ResponsesWebSocketDialConfig
}

type ResponsesWebSocketTurnResult struct {
	RequestID          string
	TerminalEvent      string
	StartedAt          time.Time
	Billing            RequestBilling
	Metrics            ProxyMetrics
	HandshakeSucceeded bool
	ResponseHeaders    http.Header
}

type ResponsesWebSocketHooks struct {
	BeforeTurn           func(context.Context, ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error)
	OnFirstDialError     func(context.Context, ResponsesWebSocketTurnRequest, ResponsesWebSocketTurnResult, *ResponsesWebSocketDialError) (ResponsesWebSocketTurnConfig, error)
	OnFirstUpstreamError func(context.Context, ResponsesWebSocketTurnRequest, ResponsesWebSocketTurnResult, *ResponsesWebSocketUpstreamEventError) (ResponsesWebSocketTurnConfig, error)
	AfterTurn            func(context.Context, int, ResponsesWebSocketTurnResult, error)
}

type ResponsesWebSocketOptions struct {
	TargetURL            string
	Dialer               ResponsesWebSocketDialer
	OutboundProxyURL     string
	DialTimeout          time.Duration
	ReadTimeout          time.Duration
	WriteTimeout         time.Duration
	InterTurnIdleTimeout time.Duration
	UpstreamDrainTimeout time.Duration
	UpstreamReadLimit    int64
}

type ResponsesWebSocketSession struct {
	targetURL            string
	dialer               ResponsesWebSocketDialer
	dialTimeout          time.Duration
	readTimeout          time.Duration
	writeTimeout         time.Duration
	interTurnIdleTimeout time.Duration
	upstreamDrainTimeout time.Duration
}

// PrepareResponsesWebSocketFrame is the public frame normalizer used by
// protocol-level callers and tests. Later turns may inherit the first model.
func PrepareResponsesWebSocketFrame(frame []byte, inheritedModel string) ([]byte, RequestBilling, error) {
	turn := 1
	if strings.TrimSpace(inheritedModel) != "" {
		turn = 2
	}
	normalized, billing, _, err := prepareResponsesWebSocketFrame(frame, turn, inheritedModel)
	return normalized, billing, err
}

type responsesWebSocketRead struct {
	messageType websocket.MessageType
	frame       []byte
	err         error
	duringTurn  bool
}

type responsesWebSocketPendingClientFrames struct {
	frames []responsesWebSocketRead
	bytes  int64
}

func (p *responsesWebSocketPendingClientFrames) append(read responsesWebSocketRead) error {
	if p == nil {
		return errors.New("nil pre-downstream client frame queue")
	}
	frameBytes := int64(len(read.frame))
	if len(p.frames) >= wsPreDownstreamQueueMaxFrames || frameBytes > wsPreDownstreamQueueMaxBytes-p.bytes {
		return NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "too many client frames before the first upstream response", nil)
	}
	read.frame = append([]byte(nil), read.frame...)
	p.frames = append(p.frames, read)
	p.bytes += frameBytes
	return nil
}

func (p *responsesWebSocketPendingClientFrames) flush(ctx context.Context, upstream ResponsesWebSocketConn, timeout time.Duration) error {
	if p == nil || len(p.frames) == 0 {
		return nil
	}
	if upstream == nil {
		return errors.New("upstream Responses WebSocket connection is unavailable")
	}
	for _, read := range p.frames {
		writeCtx, cancelWrite := context.WithTimeout(ctx, timeout)
		if err := upstream.Write(writeCtx, read.messageType, read.frame); err != nil {
			cancelWrite()
			return err
		}
		cancelWrite()
	}
	p.frames = nil
	p.bytes = 0
	return nil
}

// responsesWebSocketTurnLifecycle serializes the terminal downstream write
// with admission of the next response.create. A create received while a turn
// is genuinely active is rejected immediately. A create received while the
// terminal frame is being delivered waits for that delivery to commit and is
// then admitted as the next turn.
type responsesWebSocketTurnLifecycle struct {
	mu       sync.Mutex
	inFlight bool
}

func newResponsesWebSocketTurnLifecycle(inFlight bool) *responsesWebSocketTurnLifecycle {
	return &responsesWebSocketTurnLifecycle{inFlight: inFlight}
}

func (l *responsesWebSocketTurnLifecycle) beginResponseCreate() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight {
		return false
	}
	l.inFlight = true
	return true
}

func (l *responsesWebSocketTurnLifecycle) beginTerminalWrite() {
	if l != nil {
		l.mu.Lock()
	}
}

func (l *responsesWebSocketTurnLifecycle) finishTerminalWrite(succeeded bool) {
	if l == nil {
		return
	}
	if succeeded {
		l.inFlight = false
	}
	l.mu.Unlock()
}

type ResponsesWebSocketCloseError struct {
	status websocket.StatusCode
	reason string
	err    error
}

func NewResponsesWebSocketCloseError(status websocket.StatusCode, reason string, err error) error {
	return &ResponsesWebSocketCloseError{status: status, reason: truncateResponsesWebSocketCloseReason(reason), err: err}
}

func truncateResponsesWebSocketCloseReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) <= maxWebSocketCloseReason {
		return reason
	}
	const suffix = "..."
	limit := maxWebSocketCloseReason - len(suffix)
	for limit > 0 && limit < len(reason) && reason[limit]&0xc0 == 0x80 {
		limit--
	}
	return strings.TrimSpace(reason[:limit]) + suffix
}

func (e *ResponsesWebSocketCloseError) Error() string {
	if e == nil {
		return ""
	}
	if e.err == nil {
		return fmt.Sprintf("Responses WebSocket close %d: %s", e.status, e.reason)
	}
	return fmt.Sprintf("Responses WebSocket close %d: %s: %v", e.status, e.reason, e.err)
}

func (e *ResponsesWebSocketCloseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *ResponsesWebSocketCloseError) StatusCode() websocket.StatusCode { return e.status }
func (e *ResponsesWebSocketCloseError) Reason() string                   { return e.reason }

// ResponsesWebSocketDialError preserves the failed upgrade response so the
// HTTP layer can classify upstream errors and quota signals without guessing.
type ResponsesWebSocketDialError struct {
	StatusCode      int
	ResponseHeaders http.Header
	ResponseBody    []byte
	Err             error
}

// ResponsesWebSocketUpstreamEventError retains the fixed upstream error
// envelope so the caller can record the precise code/message for the turn.
type ResponsesWebSocketUpstreamEventError struct {
	Code    string
	Type    string
	Message string
	Frame   []byte
}

type responsesWebSocketFirstUpstreamError struct {
	result   ResponsesWebSocketTurnResult
	upstream *ResponsesWebSocketUpstreamEventError
}

func (e *responsesWebSocketFirstUpstreamError) Error() string {
	if e == nil || e.upstream == nil {
		return "upstream Responses WebSocket rate limit error"
	}
	return e.upstream.Error()
}

// ResponsesWebSocketTerminalError reports a failed terminal event after the
// event itself has already been forwarded to the client. It is turn-scoped:
// the WebSocket session remains usable for a later response.create.
type ResponsesWebSocketTerminalError struct {
	Event         string
	UpstreamError *ResponsesWebSocketUpstreamEventError
}

func (e *ResponsesWebSocketTerminalError) Error() string {
	if e == nil {
		return ""
	}
	if e.UpstreamError != nil && strings.TrimSpace(e.UpstreamError.Message) != "" {
		return e.UpstreamError.Message
	}
	return "upstream Responses WebSocket ended with " + normalizeResponsesWebSocketTerminalEvent(e.Event)
}

func (e *ResponsesWebSocketTerminalError) Unwrap() error {
	if e == nil || e.UpstreamError == nil {
		return nil
	}
	return e.UpstreamError
}

func (e *ResponsesWebSocketUpstreamEventError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return "upstream Responses WebSocket error"
}

func (e *ResponsesWebSocketDialError) Error() string {
	if e == nil {
		return ""
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("dial Responses WebSocket: status %d: %v", e.StatusCode, e.Err)
	}
	return fmt.Sprintf("dial Responses WebSocket: %v", e.Err)
}

func (e *ResponsesWebSocketDialError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func normalizeResponsesWebSocketDialError(err error, statusCode int, responseHeaders http.Header) *ResponsesWebSocketDialError {
	var dialErr *ResponsesWebSocketDialError
	if errors.As(err, &dialErr) && dialErr != nil {
		return dialErr
	}
	return &ResponsesWebSocketDialError{
		StatusCode: statusCode, ResponseHeaders: cloneWebSocketHeader(responseHeaders), Err: err,
	}
}

func responsesWebSocketDialCloseError(dialErr *ResponsesWebSocketDialError) error {
	status := websocket.StatusInternalError
	reason := "upstream Responses WebSocket handshake failed"
	if dialErr != nil {
		switch {
		case dialErr.StatusCode == http.StatusTooManyRequests:
			status = websocket.StatusTryAgainLater
			reason = "upstream rate limit exceeded, please retry later"
		case dialErr.StatusCode == http.StatusUnauthorized || dialErr.StatusCode == http.StatusForbidden:
			status = websocket.StatusPolicyViolation
			reason = "upstream Responses WebSocket authentication failed"
		case dialErr.StatusCode >= http.StatusBadRequest && dialErr.StatusCode < http.StatusInternalServerError:
			status = websocket.StatusPolicyViolation
			reason = "upstream Responses WebSocket handshake rejected"
		}
	}
	return NewResponsesWebSocketCloseError(status, reason, dialErr)
}

func NewResponsesWebSocketSession(options ResponsesWebSocketOptions) *ResponsesWebSocketSession {
	if options.TargetURL == "" {
		options.TargetURL = responsesWebSocketURL
	}
	options.UpstreamReadLimit = positiveInt64(options.UpstreamReadLimit, defaultWSUpstreamLimit)
	if options.Dialer == nil {
		options.Dialer = &coderResponsesWebSocketDialer{
			readLimit:        options.UpstreamReadLimit,
			outboundProxyURL: options.OutboundProxyURL,
			proxyClients:     make(map[string]*responsesWebSocketProxyClientEntry),
		}
	}
	return &ResponsesWebSocketSession{
		targetURL: options.TargetURL, dialer: options.Dialer,
		dialTimeout:          positiveDuration(options.DialTimeout, defaultWSDialTimeout),
		readTimeout:          positiveDuration(options.ReadTimeout, defaultWSReadTimeout),
		writeTimeout:         positiveDuration(options.WriteTimeout, defaultWSWriteTimeout),
		interTurnIdleTimeout: positiveDuration(options.InterTurnIdleTimeout, defaultWSInterTurnTimeout),
		upstreamDrainTimeout: positiveDuration(options.UpstreamDrainTimeout, defaultWSDrainTimeout),
	}
}

// Close releases idle HTTP transports owned by the default WebSocket dialer.
// Active WebSocket sessions own their upgraded connections independently.
func (s *ResponsesWebSocketSession) Close() {
	if s == nil || s.dialer == nil {
		return
	}
	if closer, ok := s.dialer.(interface{ Close() }); ok {
		closer.Close()
	}
}

func positiveDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func positiveInt64(value, fallback int64) int64 {
	if value > 0 {
		return value
	}
	return fallback
}

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
					headers := responsesWebSocketHeaders(*turnConfig.Dial, result.Billing.PromptCacheKey)
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
					if turn == 1 && dialErr.StatusCode == http.StatusTooManyRequests && hooks.OnFirstDialError != nil {
						nextConfig, retryErr := hooks.OnFirstDialError(ctx, turnRequest, result, dialErr)
						if retryErr != nil {
							return normalizeResponsesWebSocketHookError(retryErr)
						}
						if len(nextConfig.Frame) == 0 {
							nextConfig.Frame = requestFrame
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

			allowFirstUpstreamErrorRetry := turn == 1 && hooks.OnFirstUpstreamError != nil
			result, err = s.relayTurn(
				ctx, client, upstream, clientReads, upstreamReads, result, turnLifecycle, &sessionModel,
				allowFirstUpstreamErrorRetry, &pendingFirstDownstreamControls,
			)
			var firstUpstreamErr *responsesWebSocketFirstUpstreamError
			if errors.As(err, &firstUpstreamErr) && firstUpstreamErr != nil && hooks.OnFirstUpstreamError != nil {
				closeUpstream()
				nextConfig, retryErr := hooks.OnFirstUpstreamError(ctx, turnRequest, firstUpstreamErr.result, firstUpstreamErr.upstream)
				if retryErr != nil {
					return normalizeResponsesWebSocketHookError(retryErr)
				}
				retryResult := ResponsesWebSocketTurnResult{StartedAt: time.Now(), Billing: turnRequest.Billing}
				if len(nextConfig.Frame) == 0 {
					nextConfig.Frame = requestFrame
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
	allowFirstUpstreamErrorRetry bool,
	pendingFirstDownstreamControls *responsesWebSocketPendingClientFrames,
) (ResponsesWebSocketTurnResult, error) {
	activeClientReads := clientReads
	var firstTokenAt time.Time
	var terminal terminalResponse
	var pendingError *responseError
	var drainTimer *time.Timer
	var drainTimeout <-chan time.Time
	clientDisconnected := false
	wroteDownstream := false
	var clientErr error
	readTimer := time.NewTimer(s.readTimeout)
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
			if allowFirstUpstreamErrorRetry && !wroteDownstream {
				if isResponsesWebSocketResponseCancel(read.messageType, read.frame) {
					// Cancellation targets the current execution and cannot follow a
					// replay to another account. Commit this attempt, preserve FIFO
					// for earlier controls, and deliver the cancel immediately.
					allowFirstUpstreamErrorRetry = false
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
				if result.RequestID == "" && responseID != "" {
					result.RequestID = responseID
				}
				if firstTokenAt.IsZero() && isResponsesWebSocketTokenEvent(eventType) {
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
						if allowFirstUpstreamErrorRetry && !wroteDownstream && !clientDisconnected && isResponsesWebSocketRateLimitError(upstreamEventErr) {
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
				resetResponsesWebSocketTimer(readTimer, s.readTimeout)
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

func isResponsesWebSocketResponseCreate(messageType websocket.MessageType, frame []byte) bool {
	return responsesWebSocketEventTypeIs(messageType, frame, "response.create")
}

func isResponsesWebSocketResponseCancel(messageType websocket.MessageType, frame []byte) bool {
	return responsesWebSocketEventTypeIs(messageType, frame, "response.cancel")
}

func responsesWebSocketEventTypeIs(messageType websocket.MessageType, frame []byte, expected string) bool {
	if messageType != websocket.MessageText {
		return false
	}
	var event struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(frame, &event) == nil && strings.TrimSpace(event.Type) == expected
}

func updateResponsesWebSocketSessionModel(model *atomic.Pointer[string], messageType websocket.MessageType, frame []byte) {
	if model == nil || messageType != websocket.MessageText {
		return
	}
	var event struct {
		Type    string `json:"type"`
		Session struct {
			Model string `json:"model"`
		} `json:"session"`
	}
	if json.Unmarshal(frame, &event) != nil || strings.TrimSpace(event.Type) != "session.update" {
		return
	}
	storeResponsesWebSocketSessionModel(model, event.Session.Model)
}

func storeResponsesWebSocketSessionModel(model *atomic.Pointer[string], value string) {
	if model == nil {
		return
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	model.Store(&value)
}

func loadResponsesWebSocketSessionModel(model *atomic.Pointer[string]) string {
	if model == nil {
		return ""
	}
	current := model.Load()
	if current == nil {
		return ""
	}
	return strings.TrimSpace(*current)
}

func parseResponsesWebSocketFrame(frame []byte, turn int, inheritedModel string) ([]byte, RequestBilling, string, error) {
	trimmed := bytes.TrimSpace(frame)
	if len(trimmed) == 0 {
		return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "empty WebSocket request payload", nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "invalid WebSocket request payload", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "WebSocket request must contain one JSON object", err)
	}
	eventType, exists := payload["type"]
	if !exists {
		payload["type"] = "response.create"
	} else if value, ok := eventType.(string); !ok || strings.TrimSpace(value) != "response.create" {
		reason := "unsupported WebSocket request type"
		if value == "response.append" {
			reason = "response.append is not supported in WebSocket v2; use response.create with previous_response_id"
		}
		return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, reason, nil)
	}
	if turn == 1 {
		model, ok := payload["model"].(string)
		if !ok || strings.TrimSpace(model) == "" {
			return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "model is required in the first response.create payload", nil)
		}
	} else if _, exists := payload["model"]; !exists {
		payload["model"] = inheritedModel
	}
	if previous, ok := payload["previous_response_id"].(string); ok && responsesWebSocketMessageIDPattern.MatchString(strings.TrimSpace(previous)) {
		return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, "previous_response_id must be a response.id (resp_*), not a message id", nil)
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, RequestBilling{}, "", err
	}
	metadata, previousResponseID, err := responsesWebSocketBilling(payload, turn)
	if err != nil {
		return nil, RequestBilling{}, "", NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, err.Error(), err)
	}
	return normalized, metadata, previousResponseID, nil
}

func prepareResponsesWebSocketFrame(frame []byte, turn int, inheritedModel string) ([]byte, RequestBilling, string, error) {
	normalized, metadata, previousResponseID, err := parseResponsesWebSocketFrame(frame, turn, inheritedModel)
	if err != nil {
		return nil, RequestBilling{}, "", err
	}
	var payload map[string]any
	if err := json.Unmarshal(normalized, &payload); err != nil {
		return nil, RequestBilling{}, "", err
	}
	for _, field := range chatGPTUnsupportedFields {
		delete(payload, field)
	}
	delete(payload, "background")
	payload["type"] = "response.create"
	payload["store"] = false
	if _, exists := payload["stream"]; !exists {
		payload["stream"] = true
	}
	normalizeCodexInput(payload)
	ensureCodexReasoningInclude(payload)
	normalized, err = json.Marshal(payload)
	return normalized, metadata, previousResponseID, err
}

func responsesWebSocketBilling(payload map[string]any, turn int) (RequestBilling, string, error) {
	metadata := RequestBilling{Stream: true}
	if model, exists := payload["model"]; exists {
		value, ok := model.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return RequestBilling{}, "", errors.New("response.create model must be a non-empty string")
		}
		metadata.Model = strings.TrimSpace(value)
	} else if turn == 1 {
		return RequestBilling{}, "", errors.New("response.create model is required")
	}
	if tier, exists := payload["service_tier"]; exists {
		value, ok := tier.(string)
		if !ok {
			return RequestBilling{}, "", errors.New("response.create service_tier must be a string")
		}
		metadata.ServiceTier = strings.TrimSpace(value)
	}
	if key, exists := payload["prompt_cache_key"]; exists {
		value, ok := key.(string)
		if !ok {
			return RequestBilling{}, "", errors.New("response.create prompt_cache_key must be a string")
		}
		metadata.PromptCacheKey = strings.TrimSpace(value)
	}
	if stream, exists := payload["stream"]; exists {
		value, ok := stream.(bool)
		if !ok {
			return RequestBilling{}, "", errors.New("response.create stream must be a boolean")
		}
		metadata.Stream = value
	}
	previousResponseID := ""
	if previous, exists := payload["previous_response_id"]; exists {
		value, ok := previous.(string)
		if !ok {
			return RequestBilling{}, "", errors.New("response.create previous_response_id must be a string")
		}
		previousResponseID = strings.TrimSpace(value)
	}
	return metadata, previousResponseID, nil
}

func parseResponsesWebSocketEvent(frame []byte) (string, string, *terminalResponse, error) {
	var event struct {
		Type       string           `json:"type"`
		ID         string           `json:"id"`
		ResponseID string           `json:"response_id"`
		Response   terminalResponse `json:"response"`
		Error      *responseError   `json:"error"`
	}
	if err := json.Unmarshal(frame, &event); err != nil {
		return "", "", nil, fmt.Errorf("parse ChatGPT Responses WebSocket event: %w", err)
	}
	eventType := strings.TrimSpace(event.Type)
	if eventType == "" {
		return "", "", nil, errors.New("ChatGPT Responses WebSocket event type is required")
	}
	responseID := event.Response.ID
	if responseID == "" {
		responseID = event.ResponseID
	}
	if responseID == "" && isTerminalResponseEvent(eventType) {
		responseID = event.ID
	}
	if isTerminalResponseEvent(eventType) {
		return eventType, responseID, &event.Response, nil
	}
	return eventType, responseID, nil, nil
}

func observeResponsesWebSocketEvent(frame []byte) (string, string, *terminalResponse) {
	eventType, responseID, terminal, err := parseResponsesWebSocketEvent(frame)
	if err != nil {
		return "", "", nil
	}
	return eventType, responseID, terminal
}

func parseResponsesWebSocketErrorEvent(frame []byte) *ResponsesWebSocketUpstreamEventError {
	var event struct {
		Error *responseError `json:"error"`
	}
	if json.Unmarshal(frame, &event) != nil || event.Error == nil {
		return nil
	}
	return &ResponsesWebSocketUpstreamEventError{
		Code: event.Error.Code, Type: event.Error.Type, Message: event.Error.Message, Frame: append([]byte(nil), frame...),
	}
}

// isResponsesWebSocketRateLimitError intentionally mirrors sub2api's WS v2
// classifier. Only explicit upstream rate, usage, or quota exhaustion signals
// are safe to replay on another account before any downstream frame exists.
func isResponsesWebSocketRateLimitError(upstreamErr *ResponsesWebSocketUpstreamEventError) bool {
	if upstreamErr == nil {
		return false
	}
	code := strings.ToLower(strings.TrimSpace(upstreamErr.Code))
	errorType := strings.ToLower(strings.TrimSpace(upstreamErr.Type))
	message := strings.ToLower(strings.TrimSpace(upstreamErr.Message))
	if strings.Contains(errorType, "rate_limit") || strings.Contains(errorType, "usage_limit") {
		return true
	}
	if strings.Contains(code, "rate_limit") || strings.Contains(code, "usage_limit") || strings.Contains(code, "insufficient_quota") {
		return true
	}
	if strings.Contains(message, "usage limit") && strings.Contains(message, "reached") {
		return true
	}
	return strings.Contains(message, "rate limit") && (strings.Contains(message, "reached") || strings.Contains(message, "exceeded"))
}

func normalizeResponsesWebSocketTerminalEvent(eventType string) string {
	if eventType == "response.canceled" {
		return "response.cancelled"
	}
	return eventType
}

func isResponsesWebSocketTokenEvent(eventType string) bool {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" || isTerminalResponseEvent(eventType) {
		return false
	}
	switch eventType {
	case "response.created", "response.in_progress", "response.output_item.added", "response.output_item.done":
		return false
	}
	if strings.Contains(eventType, ".delta") {
		return true
	}
	return strings.HasPrefix(eventType, "response.output") && !strings.HasSuffix(eventType, ".done")
}

func responsesWebSocketHeaders(config ResponsesWebSocketDialConfig, promptCacheKey string) http.Header {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+config.AccessToken)
	if config.ChatGPTAccountID != "" {
		headers.Set("ChatGPT-Account-ID", config.ChatGPTAccountID)
	}
	headers.Set("OpenAI-Beta", responsesWebSocketBetaV2)
	applyCodexOAuthIdentity(headers, "")
	for _, name := range []string{"Accept-Language", "X-Codex-Beta-Features", "X-Codex-Window-Id", "X-Codex-Installation-Id", "X-Codex-Turn-State", "X-Codex-Turn-Metadata"} {
		for _, value := range config.InboundHeader.Values(name) {
			if strings.TrimSpace(value) != "" {
				headers.Add(name, value)
			}
		}
	}
	sessionID := strings.TrimSpace(config.InboundHeader.Get("session_id"))
	conversationID := strings.TrimSpace(config.InboundHeader.Get("conversation_id"))
	if sessionID == "" && conversationID != "" {
		sessionID = conversationID
	}
	if sessionID == "" {
		sessionID = strings.TrimSpace(promptCacheKey)
	}
	if sessionID != "" {
		headers.Set("session_id", isolateSession(config.APIKeyID, sessionID))
	}
	if conversationID != "" {
		headers.Set("conversation_id", isolateSession(config.APIKeyID, conversationID))
	}
	return headers
}

func validateResponsesWebSocketDialConfig(config ResponsesWebSocketDialConfig) error {
	if strings.TrimSpace(config.AccessToken) == "" {
		return errors.New("Responses WebSocket access token is required")
	}
	if strings.TrimSpace(config.APIKeyID) == "" {
		return errors.New("Responses WebSocket API key ID is required")
	}
	if strings.TrimSpace(config.ChatGPTAccountID) == "" {
		return errors.New("Responses WebSocket ChatGPT account ID is required")
	}
	return nil
}

func cloneResponsesWebSocketDialConfig(config ResponsesWebSocketDialConfig) ResponsesWebSocketDialConfig {
	config.InboundHeader = cloneWebSocketHeader(config.InboundHeader)
	return config
}

func verifyPinnedResponsesWebSocketDial(pinned, current ResponsesWebSocketDialConfig) error {
	if strings.TrimSpace(current.APIKeyID) != strings.TrimSpace(pinned.APIKeyID) {
		return errors.New("Responses WebSocket API key binding changed")
	}
	if strings.TrimSpace(current.ChatGPTAccountID) != strings.TrimSpace(pinned.ChatGPTAccountID) {
		return errors.New("Responses WebSocket ChatGPT account binding changed")
	}
	if strings.TrimSpace(current.ProxyURL) != strings.TrimSpace(pinned.ProxyURL) {
		return errors.New("Responses WebSocket proxy binding changed")
	}
	return nil
}

func cloneWebSocketHeader(source http.Header) http.Header {
	if source == nil {
		return nil
	}
	return source.Clone()
}

type coderResponsesWebSocketDialer struct {
	readLimit        int64
	outboundProxyURL string
	proxyMu          sync.Mutex
	proxyClients     map[string]*responsesWebSocketProxyClientEntry
	closed           bool
}

type responsesWebSocketProxyClientEntry struct {
	client   *http.Client
	lastUsed time.Time
}

func (d *coderResponsesWebSocketDialer) Dial(ctx context.Context, target string, headers http.Header, proxyURL string) (ResponsesWebSocketConn, int, http.Header, error) {
	options := &websocket.DialOptions{HTTPHeader: headers.Clone(), CompressionMode: websocket.CompressionContextTakeover}
	httpClient, err := d.proxyHTTPClient(proxyURL)
	if err != nil {
		return nil, 0, nil, err
	}
	if httpClient != nil {
		options.HTTPClient = httpClient
	}
	connection, response, err := websocket.Dial(ctx, target, options)
	status := 0
	responseHeaders := responseHeader(response)
	if response != nil {
		status = response.StatusCode
	}
	if err != nil {
		var responseBody []byte
		if response != nil && response.Body != nil {
			responseBody, _ = io.ReadAll(io.LimitReader(response.Body, 8<<10))
			_ = response.Body.Close()
		}
		return nil, status, responseHeaders, &ResponsesWebSocketDialError{
			StatusCode: status, ResponseHeaders: responseHeaders, ResponseBody: responseBody, Err: err,
		}
	}
	connection.SetReadLimit(positiveInt64(d.readLimit, defaultWSUpstreamLimit))
	return connection, status, responseHeaders, nil
}

func (d *coderResponsesWebSocketDialer) proxyHTTPClient(accountProxyURL string) (*http.Client, error) {
	proxyURL := strings.TrimSpace(accountProxyURL)
	if proxyURL == "" {
		proxyURL = strings.TrimSpace(d.outboundProxyURL)
	}
	if proxyURL == "" {
		return nil, nil
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse Responses WebSocket proxy: %w", err)
	}
	now := time.Now()
	d.proxyMu.Lock()
	defer d.proxyMu.Unlock()
	if d.closed {
		return nil, errors.New("Responses WebSocket dialer is closed")
	}
	if d.proxyClients == nil {
		d.proxyClients = make(map[string]*responsesWebSocketProxyClientEntry)
	}
	if entry := d.proxyClients[proxyURL]; entry != nil && entry.client != nil {
		entry.lastUsed = now
		return entry.client, nil
	}
	d.pruneProxyClientsLocked(now)
	transport := &http.Transport{
		Proxy:               http.ProxyURL(parsed),
		MaxIdleConns:        wsProxyMaxIdleConns,
		MaxIdleConnsPerHost: wsProxyMaxIdlePerHost,
		IdleConnTimeout:     wsProxyIdleConnTimeout,
		TLSHandshakeTimeout: defaultWSDialTimeout,
		ForceAttemptHTTP2:   true,
	}
	client := &http.Client{Transport: transport}
	d.proxyClients[proxyURL] = &responsesWebSocketProxyClientEntry{client: client, lastUsed: now}
	d.enforceProxyClientCapacityLocked()
	return client, nil
}

func (d *coderResponsesWebSocketDialer) pruneProxyClientsLocked(now time.Time) {
	for proxyURL, entry := range d.proxyClients {
		if entry == nil || entry.client == nil || now.Sub(entry.lastUsed) > wsProxyClientCacheTTL {
			closeResponsesWebSocketProxyClient(entry)
			delete(d.proxyClients, proxyURL)
		}
	}
}

func (d *coderResponsesWebSocketDialer) enforceProxyClientCapacityLocked() {
	for len(d.proxyClients) > wsProxyClientCacheLimit {
		var oldestURL string
		var oldestTime time.Time
		for proxyURL, entry := range d.proxyClients {
			lastUsed := time.Time{}
			if entry != nil {
				lastUsed = entry.lastUsed
			}
			if oldestURL == "" || lastUsed.Before(oldestTime) {
				oldestURL, oldestTime = proxyURL, lastUsed
			}
		}
		if oldestURL == "" {
			return
		}
		closeResponsesWebSocketProxyClient(d.proxyClients[oldestURL])
		delete(d.proxyClients, oldestURL)
	}
}

func (d *coderResponsesWebSocketDialer) Close() {
	if d == nil {
		return
	}
	d.proxyMu.Lock()
	defer d.proxyMu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	for proxyURL, entry := range d.proxyClients {
		closeResponsesWebSocketProxyClient(entry)
		delete(d.proxyClients, proxyURL)
	}
}

func closeResponsesWebSocketProxyClient(entry *responsesWebSocketProxyClientEntry) {
	if entry == nil || entry.client == nil || entry.client.Transport == nil {
		return
	}
	if transport, ok := entry.client.Transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}

func responseHeader(response *http.Response) http.Header {
	if response == nil {
		return nil
	}
	return response.Header.Clone()
}
