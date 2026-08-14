package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
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
	AccessToken       string
	ChatGPTAccountID  string
	APIKeyID          string
	InternalAccountID string
	FingerprintMode   string
	Fingerprint       *CodexFingerprint
	ProxyURL          string
	InboundHeader     http.Header
	Model             string
	ServiceTier       string
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
	FirstOutputTimeout   time.Duration
}

type ResponsesWebSocketSession struct {
	targetURL            string
	dialer               ResponsesWebSocketDialer
	dialTimeout          time.Duration
	readTimeout          time.Duration
	writeTimeout         time.Duration
	interTurnIdleTimeout time.Duration
	upstreamDrainTimeout time.Duration
	firstOutputTimeout   time.Duration
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
	readTimeout := positiveDuration(options.ReadTimeout, defaultWSReadTimeout)
	firstOutputTimeout := options.FirstOutputTimeout
	if firstOutputTimeout <= 0 {
		firstOutputTimeout = readTimeout
	}
	return &ResponsesWebSocketSession{
		targetURL: options.TargetURL, dialer: options.Dialer,
		dialTimeout:          positiveDuration(options.DialTimeout, defaultWSDialTimeout),
		readTimeout:          readTimeout,
		writeTimeout:         positiveDuration(options.WriteTimeout, defaultWSWriteTimeout),
		interTurnIdleTimeout: positiveDuration(options.InterTurnIdleTimeout, defaultWSInterTurnTimeout),
		upstreamDrainTimeout: positiveDuration(options.UpstreamDrainTimeout, defaultWSDrainTimeout),
		firstOutputTimeout:   firstOutputTimeout,
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
