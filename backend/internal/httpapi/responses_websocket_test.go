package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/openai"
	"github.com/sharesub/sharesub/backend/internal/security"
)

const responsesWebSocketHTTPAPIKey = "sk-sharesub-websocket-test"

type responsesWebSocketHTTPStore struct {
	application.Store

	mu          sync.Mutex
	credential  domain.GatewayCredential
	credentials []domain.GatewayCredential
	metrics     []domain.GatewayMetric
	quotas      []responsesWebSocketHTTPQuotaRecord
}

type responsesWebSocketHTTPQuotaRecord struct {
	planID, accountID string
	generation        int64
	signals           []domain.QuotaSignal
}

func (s *responsesWebSocketHTTPStore) ResolveGatewayRoutes(context.Context, []byte, time.Time) (domain.GatewayRouteSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidates := s.credentials
	if len(candidates) == 0 {
		candidates = []domain.GatewayCredential{s.credential}
	}
	return domain.GatewayRouteSet{
		APIKey:     domain.APIKey{ID: candidates[0].APIKeyID, Strategy: domain.RoutePriority},
		Candidates: append([]domain.GatewayCredential(nil), candidates...),
	}, nil
}

func (*responsesWebSocketHTTPStore) AccountQuotaExhausted(context.Context, string, time.Time) (bool, error) {
	return false, nil
}

func (*responsesWebSocketHTTPStore) MemberQuotaExhausted(context.Context, string, string, string, int64, int, time.Time) (bool, error) {
	return false, nil
}

func (*responsesWebSocketHTTPStore) TouchAPIKey(context.Context, string, time.Time) error {
	return nil
}

func (s *responsesWebSocketHTTPStore) RecordGatewayMetric(_ context.Context, metric domain.GatewayMetric) error {
	s.mu.Lock()
	s.metrics = append(s.metrics, metric)
	s.mu.Unlock()
	return nil
}

func (s *responsesWebSocketHTTPStore) RecordAccountQuotaSignals(_ context.Context, planID, accountID string, generation int64, signals []domain.QuotaSignal, _ time.Time) error {
	s.mu.Lock()
	s.quotas = append(s.quotas, responsesWebSocketHTTPQuotaRecord{
		planID: planID, accountID: accountID, generation: generation, signals: append([]domain.QuotaSignal(nil), signals...),
	})
	s.mu.Unlock()
	return nil
}

func (s *responsesWebSocketHTTPStore) recordedMetrics() []domain.GatewayMetric {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.GatewayMetric(nil), s.metrics...)
}

func (s *responsesWebSocketHTTPStore) recordedQuotas() []responsesWebSocketHTTPQuotaRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]responsesWebSocketHTTPQuotaRecord, len(s.quotas))
	copy(result, s.quotas)
	return result
}

func (s *responsesWebSocketHTTPStore) changeBindingGeneration(generation int64) {
	s.mu.Lock()
	s.credential.AccountBindingGeneration = generation
	s.mu.Unlock()
}

type responsesWebSocketHTTPMessage struct {
	typ     websocket.MessageType
	payload []byte
	err     error
}

type responsesWebSocketHTTPUpstream struct {
	reads  chan responsesWebSocketHTTPMessage
	writes chan []byte
	done   chan struct{}
	once   sync.Once
}

func newResponsesWebSocketHTTPUpstream() *responsesWebSocketHTTPUpstream {
	return &responsesWebSocketHTTPUpstream{
		reads: make(chan responsesWebSocketHTTPMessage, 8), writes: make(chan []byte, 8), done: make(chan struct{}),
	}
}

func (c *responsesWebSocketHTTPUpstream) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-c.done:
		return 0, nil, io.ErrClosedPipe
	case message := <-c.reads:
		return message.typ, append([]byte(nil), message.payload...), message.err
	}
}

func (c *responsesWebSocketHTTPUpstream) Write(ctx context.Context, _ websocket.MessageType, payload []byte) error {
	copyPayload := append([]byte(nil), payload...)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return io.ErrClosedPipe
	case c.writes <- copyPayload:
		return nil
	}
}

func (c *responsesWebSocketHTTPUpstream) Close(websocket.StatusCode, string) error {
	return c.CloseNow()
}

func (c *responsesWebSocketHTTPUpstream) CloseNow() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func (c *responsesWebSocketHTTPUpstream) send(payload string) {
	c.reads <- responsesWebSocketHTTPMessage{typ: websocket.MessageText, payload: []byte(payload)}
}

type responsesWebSocketHTTPDialer struct {
	mu               sync.Mutex
	conn             openai.ResponsesWebSocketConn
	count            int
	headers          http.Header
	headersByAttempt []http.Header
	attempts         []responsesWebSocketHTTPDialAttempt
}

type responsesWebSocketHTTPDialAttempt struct {
	conn            openai.ResponsesWebSocketConn
	status          int
	responseHeaders http.Header
	err             error
}

func (d *responsesWebSocketHTTPDialer) Dial(_ context.Context, _ string, headers http.Header, _ string) (openai.ResponsesWebSocketConn, int, http.Header, error) {
	d.mu.Lock()
	attemptIndex := d.count
	d.count++
	d.headers = headers.Clone()
	d.headersByAttempt = append(d.headersByAttempt, headers.Clone())
	if attemptIndex < len(d.attempts) {
		attempt := d.attempts[attemptIndex]
		d.mu.Unlock()
		return attempt.conn, attempt.status, attempt.responseHeaders.Clone(), attempt.err
	}
	conn := d.conn
	d.mu.Unlock()
	return conn, http.StatusSwitchingProtocols, http.Header{"X-Request-Id": []string{"handshake-request"}}, nil
}

func (d *responsesWebSocketHTTPDialer) dialCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.count
}

func (d *responsesWebSocketHTTPDialer) dialHeaders() []http.Header {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]http.Header, len(d.headersByAttempt))
	for index := range d.headersByAttempt {
		result[index] = d.headersByAttempt[index].Clone()
	}
	return result
}

func newResponsesWebSocketHTTPService(t *testing.T, mutateCredential ...func(*domain.GatewayCredential)) (*application.Service, *responsesWebSocketHTTPStore) {
	t.Helper()
	manager, err := security.New(make([]byte, 32), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	credential := domain.GatewayCredential{
		APIKeyID: responsesWebSocketHTTPAPIKey,
		Member: domain.Member{
			ID: "member", UserID: "user", ShareBasisPoints: 10_000,
		},
		Plan: domain.Plan{
			ID: "plan", AccountID: "account", AllocationMode: domain.AllocationFixed,
		},
		Account: domain.Account{
			ID: "account", OwnerUserID: "owner", ChatGPTAccountID: "chatgpt-account", MaxConcurrency: 1,
		},
		TokenExpiresAt:           time.Now().Add(time.Hour),
		AccountBindingGeneration: 7,
	}
	for _, mutate := range mutateCredential {
		mutate(&credential)
	}
	credential.AccessTokenCiphertext, err = manager.Encrypt("access-token", []byte("owner:chatgpt-account:access"))
	if err != nil {
		t.Fatal(err)
	}
	store := &responsesWebSocketHTTPStore{credential: credential}
	return application.NewService(store, manager, nil, 0, "", ""), store
}

func addResponsesWebSocketHTTPCredential(t *testing.T, store *responsesWebSocketHTTPStore, accountID string) domain.GatewayCredential {
	t.Helper()
	credential := store.credential
	credential.Plan.ID = "plan-" + accountID
	credential.Plan.AccountID = accountID
	credential.Account.ID = accountID
	credential.Account.ChatGPTAccountID = "chatgpt-" + accountID
	credential.AccountBindingGeneration++
	manager, err := security.New(make([]byte, 32), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	credential.AccessTokenCiphertext, err = manager.Encrypt(
		"access-token-"+accountID,
		[]byte(credential.Account.OwnerUserID+":"+credential.Account.ChatGPTAccountID+":access"),
	)
	if err != nil {
		t.Fatal(err)
	}
	store.credentials = append(store.credentials, credential)
	return credential
}

func newResponsesWebSocketHTTPServer(
	t *testing.T,
	config ResponsesWebSocketConfig,
	mutateCredential ...func(*domain.GatewayCredential),
) (*httptest.Server, *application.Service, *responsesWebSocketHTTPStore, *responsesWebSocketHTTPUpstream, *responsesWebSocketHTTPDialer) {
	return newResponsesWebSocketHTTPServerWithRequestDone(t, config, nil, mutateCredential...)
}

func newResponsesWebSocketHTTPServerWithRequestDone(
	t *testing.T,
	config ResponsesWebSocketConfig,
	requestDone chan<- struct{},
	mutateCredential ...func(*domain.GatewayCredential),
) (*httptest.Server, *application.Service, *responsesWebSocketHTTPStore, *responsesWebSocketHTTPUpstream, *responsesWebSocketHTTPDialer) {
	t.Helper()
	service, store := newResponsesWebSocketHTTPService(t, mutateCredential...)
	upstream := newResponsesWebSocketHTTPUpstream()
	dialer := &responsesWebSocketHTTPDialer{conn: upstream}
	server := New(service, nil, discardTestLogger(), config)
	server.responsesWebSocket = openai.NewResponsesWebSocketSession(openai.ResponsesWebSocketOptions{
		Dialer: dialer, DialTimeout: config.DialTimeout, ReadTimeout: config.ReadTimeout,
		WriteTimeout: config.WriteTimeout, InterTurnIdleTimeout: config.InterTurnIdleTimeout,
		UpstreamDrainTimeout: config.UpstreamDrainTimeout,
	})
	handler := server.Handler()
	if requestDone != nil {
		baseHandler := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			baseHandler.ServeHTTP(w, r)
			requestDone <- struct{}{}
		})
	}
	return httptest.NewServer(handler), service, store, upstream, dialer
}

func responsesWebSocketHTTPConfig() ResponsesWebSocketConfig {
	config := DefaultResponsesWebSocketConfig()
	config.FirstMessageTimeout = time.Second
	config.InterTurnIdleTimeout = time.Second
	config.MaxSessionDuration = 2 * time.Second
	config.DialTimeout = time.Second
	config.ReadTimeout = time.Second
	config.WriteTimeout = time.Second
	config.UpstreamDrainTimeout = 25 * time.Millisecond
	return config
}

func dialResponsesWebSocketHTTP(t *testing.T, serverURL string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+responsesWebSocketHTTPAPIKey)
	return websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(serverURL, "http")+"/v1/responses", &websocket.DialOptions{HTTPHeader: headers})
}

func writeResponsesWebSocketHTTP(t *testing.T, conn *websocket.Conn, payload string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		t.Fatalf("write WebSocket frame: %v", err)
	}
}

func readResponsesWebSocketHTTP(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	messageType, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read WebSocket frame: %v", err)
	}
	if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
		t.Fatalf("WebSocket message type = %d", messageType)
	}
	return payload
}

func readResponsesWebSocketHTTPClose(t *testing.T, conn *websocket.Conn) websocket.CloseError {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := conn.Read(ctx)
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("WebSocket read error = %v, want close frame", err)
	}
	return closeErr
}

func waitResponsesWebSocketHTTPWrite(t *testing.T, upstream *responsesWebSocketHTTPUpstream) []byte {
	t.Helper()
	select {
	case payload := <-upstream.writes:
		return payload
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream WebSocket write")
		return nil
	}
}

func waitResponsesWebSocketHTTPMetrics(t *testing.T, store *responsesWebSocketHTTPStore, count int) []domain.GatewayMetric {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		metrics := store.recordedMetrics()
		if len(metrics) >= count {
			return metrics
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d metrics; got %d", count, len(store.recordedMetrics()))
	return nil
}

func discardTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestResponsesWebSocketIngressLimiter(t *testing.T) {
	limiter := newResponsesWebSocketIngressLimiter(1)
	release, ok := limiter.acquire("key-a")
	if !ok {
		t.Fatal("first connection was rejected")
	}
	if _, ok := limiter.acquire("key-a"); ok {
		t.Fatal("second connection for the same key was accepted")
	}
	otherRelease, ok := limiter.acquire("key-b")
	if !ok {
		t.Fatal("independent API key was rejected")
	}
	otherRelease()
	release()
	release()
	if _, ok := limiter.acquire("key-a"); !ok {
		t.Fatal("released slot was not reusable")
	}
}

func TestResponsesWebSocketUpgradeDetection(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	request.Header.Set("Connection", "keep-alive, Upgrade")
	request.Header.Set("Upgrade", "WebSocket")
	if !isWebSocketUpgrade(request) {
		t.Fatal("valid WebSocket Upgrade was not detected")
	}
	request.Header.Set("Upgrade", "h2c")
	if isWebSocketUpgrade(request) {
		t.Fatal("non-WebSocket Upgrade was accepted")
	}
}

func TestResponsesWebSocketRoutesRequireUpgrade(t *testing.T) {
	server := New(nil, nil, discardTestLogger())
	for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUpgradeRequired {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUpgradeRequired)
			}
			if recorder.Header().Get("Upgrade") != "websocket" {
				t.Fatalf("Upgrade = %q", recorder.Header().Get("Upgrade"))
			}
		})
	}
}

func TestContextWithPositiveTimeout(t *testing.T) {
	ctx, cancel := contextWithPositiveTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("error = %v", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("context did not time out")
	}
}

func TestResponsesWebSocketHTTPUpgradeAndTwoTurnsReuseUpstream(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	httpServer, service, store, upstream, dialer := newResponsesWebSocketHTTPServer(t, config)
	defer httpServer.Close()

	client, response, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	if response == nil || response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade response = %#v", response)
	}

	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","model":"gpt-5.6-sol","input":"one"}`)
	firstRequest := waitResponsesWebSocketHTTPWrite(t, upstream)
	if !strings.Contains(string(firstRequest), `"model":"gpt-5.6-sol"`) {
		t.Fatalf("first upstream request = %s", firstRequest)
	}
	upstream.send(`{"type":"response.output_text.delta","response_id":"resp_1","delta":"one"}`)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.6-sol","usage":{"input_tokens":5,"output_tokens":2,"input_tokens_details":{"cached_tokens":3}}}}`)
	readResponsesWebSocketHTTP(t, client)
	readResponsesWebSocketHTTP(t, client)
	firstMetrics := waitResponsesWebSocketHTTPMetrics(t, store, 1)
	if firstMetrics[0].StatusCode != http.StatusOK || firstMetrics[0].RequestID != "resp_1" ||
		firstMetrics[0].TokenUsage.InputTokens != 5 || firstMetrics[0].TokenUsage.OutputTokens != 2 ||
		firstMetrics[0].TokenUsage.CachedTokens != 3 {
		t.Fatalf("first metric = %+v", firstMetrics[0])
	}

	// MaxConcurrency=1 makes a direct resolve fail while the turn holds its
	// account slot. A successful probe proves the slot was released at the
	// terminal boundary before this connection starts its next turn.
	probe, err := service.ResolveGatewayAccess(context.Background(), responsesWebSocketHTTPAPIKey)
	if err != nil {
		t.Fatalf("account slot remained occupied after first turn: %v", err)
	}
	if probe.Release != nil {
		probe.Release()
	}

	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","previous_response_id":"resp_1","input":"two"}`)
	secondRequest := waitResponsesWebSocketHTTPWrite(t, upstream)
	if !strings.Contains(string(secondRequest), `"previous_response_id":"resp_1"`) || !strings.Contains(string(secondRequest), `"model":"gpt-5.6-sol"`) {
		t.Fatalf("second upstream request = %s", secondRequest)
	}
	upstream.send(`{"type":"response.completed","response":{"id":"resp_2","model":"gpt-5.6-sol","usage":{"input_tokens":7,"output_tokens":4}}}`)
	readResponsesWebSocketHTTP(t, client)
	metrics := waitResponsesWebSocketHTTPMetrics(t, store, 2)
	if dialer.dialCount() != 1 {
		t.Fatalf("upstream dial count = %d, want 1", dialer.dialCount())
	}
	if metrics[1].StatusCode != http.StatusOK || metrics[1].RequestID != "resp_2" ||
		metrics[1].TokenUsage.InputTokens != 7 || metrics[1].TokenUsage.OutputTokens != 4 {
		t.Fatalf("second metric = %+v", metrics[1])
	}

	if err := client.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("close client WebSocket: %v", err)
	}
}

func TestResponsesWebSocketHTTPFirstHandshake429SwitchesAndPinsSuccessfulAccount(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	httpServer, service, store, firstUpstream, dialer := newResponsesWebSocketHTTPServer(t, config, func(credential *domain.GatewayCredential) {
		credential.Account.RPMLimit = 1
	})
	defer httpServer.Close()
	first := addResponsesWebSocketHTTPCredential(t, store, "account-a")
	second := addResponsesWebSocketHTTPCredential(t, store, "account-b")
	second.Account.RPMLimit = 2
	store.credentials[1] = second
	secondUpstream := newResponsesWebSocketHTTPUpstream()
	quotaHeaders := http.Header{
		"X-Codex-Primary-Used-Percent":          []string{"100"},
		"X-Codex-Primary-Reset-After-Seconds":   []string{"3600"},
		"X-Codex-Primary-Window-Minutes":        []string{"300"},
		"X-Codex-Secondary-Used-Percent":        []string{"25"},
		"X-Codex-Secondary-Reset-After-Seconds": []string{"7200"},
		"X-Codex-Secondary-Window-Minutes":      []string{"10080"},
	}
	dialer.attempts = []responsesWebSocketHTTPDialAttempt{
		{status: http.StatusTooManyRequests, responseHeaders: quotaHeaders, err: errors.New("upgrade rejected")},
		{conn: secondUpstream, status: http.StatusSwitchingProtocols, responseHeaders: http.Header{"X-Request-Id": []string{"handshake-b"}}},
	}

	client, _, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	firstFrame := `{"type":"response.create","model":"gpt-5.6-sol","input":"one"}`
	writeResponsesWebSocketHTTP(t, client, firstFrame)
	gotFirst := waitResponsesWebSocketHTTPWrite(t, secondUpstream)
	if !strings.Contains(string(gotFirst), `"content":"one"`) {
		t.Fatalf("successful account first request = %s", gotFirst)
	}
	select {
	case payload := <-firstUpstream.writes:
		t.Fatalf("failed account received first frame: %s", payload)
	default:
	}
	if dialer.dialCount() != 2 {
		t.Fatalf("upstream dial count = %d, want 2", dialer.dialCount())
	}
	dialHeaders := dialer.dialHeaders()
	if got := dialHeaders[0].Get("ChatGPT-Account-ID"); got != first.Account.ChatGPTAccountID {
		t.Fatalf("first dial account = %q, want %q", got, first.Account.ChatGPTAccountID)
	}
	if got := dialHeaders[1].Get("ChatGPT-Account-ID"); got != second.Account.ChatGPTAccountID {
		t.Fatalf("second dial account = %q, want %q", got, second.Account.ChatGPTAccountID)
	}

	secondUpstream.send(`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1}}}`)
	readResponsesWebSocketHTTP(t, client)
	metrics := waitResponsesWebSocketHTTPMetrics(t, store, 2)
	if metrics[0].AccountID != first.Account.ID || metrics[0].StatusCode != http.StatusTooManyRequests ||
		metrics[0].ErrorCode != "rate_limit_error" || !metrics[0].IsStream {
		t.Fatalf("failed handshake metric = %+v", metrics[0])
	}
	if metrics[1].AccountID != second.Account.ID || metrics[1].StatusCode != http.StatusOK || !metrics[1].IsStream {
		t.Fatalf("successful turn metric = %+v", metrics[1])
	}
	quotas := store.recordedQuotas()
	if len(quotas) != 1 || quotas[0].accountID != first.Account.ID || quotas[0].planID != first.Plan.ID || len(quotas[0].signals) != 2 {
		t.Fatalf("failed handshake quota records = %+v", quotas)
	}

	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","previous_response_id":"resp_1","input":"two"}`)
	gotSecond := waitResponsesWebSocketHTTPWrite(t, secondUpstream)
	if !strings.Contains(string(gotSecond), `"content":"two"`) {
		t.Fatalf("pinned account second request = %s", gotSecond)
	}
	if dialer.dialCount() != 2 {
		t.Fatalf("second turn redialed upstream: %d", dialer.dialCount())
	}
	secondUpstream.send(`{"type":"response.completed","response":{"id":"resp_2","usage":{"input_tokens":1}}}`)
	readResponsesWebSocketHTTP(t, client)
	metrics = waitResponsesWebSocketHTTPMetrics(t, store, 3)
	if metrics[2].AccountID != second.Account.ID || metrics[2].StatusCode != http.StatusOK {
		t.Fatalf("second turn metric = %+v", metrics[2])
	}

	if _, err := service.ResolveGatewayAccess(context.Background(), responsesWebSocketHTTPAPIKey, second.Account.ID); !errors.Is(err, domain.ErrAccountRateLimited) {
		t.Fatalf("failed account RPM was not consumed: %v", err)
	}
}

func TestResponsesWebSocketHTTPFirstUpstreamRateLimitErrorSwitchesBeforeDownstreamAndPinsSuccessfulAccount(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	httpServer, service, store, firstUpstream, dialer := newResponsesWebSocketHTTPServer(t, config, func(credential *domain.GatewayCredential) {
		credential.Account.RPMLimit = 1
		credential.APIKeyFastPolicy = []domain.FastPolicyRule{{ServiceTier: "priority", Action: "force_priority", FallbackAction: "pass"}}
	})
	defer httpServer.Close()
	first := addResponsesWebSocketHTTPCredential(t, store, "account-a")
	second := addResponsesWebSocketHTTPCredential(t, store, "account-b")
	second.Account.RPMLimit = 2
	store.credentials[1] = second
	secondUpstream := newResponsesWebSocketHTTPUpstream()
	quotaHeaders := http.Header{
		"X-Codex-Primary-Used-Percent":          []string{"100"},
		"X-Codex-Primary-Reset-After-Seconds":   []string{"3600"},
		"X-Codex-Primary-Window-Minutes":        []string{"300"},
		"X-Codex-Secondary-Used-Percent":        []string{"25"},
		"X-Codex-Secondary-Reset-After-Seconds": []string{"7200"},
		"X-Codex-Secondary-Window-Minutes":      []string{"10080"},
	}
	dialer.attempts = []responsesWebSocketHTTPDialAttempt{
		{conn: firstUpstream, status: http.StatusSwitchingProtocols, responseHeaders: quotaHeaders},
		{conn: secondUpstream, status: http.StatusSwitchingProtocols, responseHeaders: http.Header{"X-Request-Id": []string{"handshake-b"}}},
	}

	client, _, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	firstFrame := `{"type":"response.create","model":"gpt-5.6-sol","input":"one"}`
	writeResponsesWebSocketHTTP(t, client, firstFrame)
	gotFailedRequest := waitResponsesWebSocketHTTPWrite(t, firstUpstream)
	firstUpstream.send(`{"type":"error","error":{"type":"usage_limit_error","code":"insufficient_quota","message":"usage limit reached"}}`)
	gotSuccessfulRequest := waitResponsesWebSocketHTTPWrite(t, secondUpstream)
	var failedPayload, successfulPayload map[string]any
	if err := json.Unmarshal(gotFailedRequest, &failedPayload); err != nil {
		t.Fatalf("decode failed account first frame: %v", err)
	}
	if err := json.Unmarshal(gotSuccessfulRequest, &successfulPayload); err != nil {
		t.Fatalf("decode successful account first frame: %v", err)
	}
	failedMetadata, failedMetadataOK := failedPayload["client_metadata"].(map[string]any)
	successfulMetadata, successfulMetadataOK := successfulPayload["client_metadata"].(map[string]any)
	delete(failedPayload, "client_metadata")
	delete(successfulPayload, "client_metadata")
	if !reflect.DeepEqual(failedPayload, successfulPayload) || !strings.Contains(string(gotSuccessfulRequest), `"content":"one"`) {
		t.Fatalf("first frame replay mismatch:\nfailed=%s\nsuccess=%s", gotFailedRequest, gotSuccessfulRequest)
	}
	if !failedMetadataOK || !successfulMetadataOK ||
		failedMetadata["session_id"] == "" || successfulMetadata["session_id"] == "" ||
		failedMetadata["session_id"] == successfulMetadata["session_id"] ||
		failedMetadata["x-codex-installation-id"] == successfulMetadata["x-codex-installation-id"] {
		t.Fatalf("account-specific fingerprints were not switched:\nfailed=%v\nsuccess=%v", failedMetadata, successfulMetadata)
	}
	if dialer.dialCount() != 2 {
		t.Fatalf("upstream dial count = %d, want 2", dialer.dialCount())
	}
	select {
	case payload := <-firstUpstream.writes:
		t.Fatalf("failed upstream received duplicate first frame: %s", payload)
	default:
	}
	// If the suppressed error leaked, it would be the first client frame.
	secondUpstream.send(`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1}}}`)
	if payload := readResponsesWebSocketHTTP(t, client); !strings.Contains(string(payload), `"type":"response.completed"`) {
		t.Fatalf("first downstream frame = %s", payload)
	}
	metrics := waitResponsesWebSocketHTTPMetrics(t, store, 2)
	if metrics[0].AccountID != first.Account.ID || metrics[0].StatusCode != http.StatusTooManyRequests ||
		metrics[0].ErrorCode != "insufficient_quota" || metrics[0].ErrorMessage != "usage limit reached" ||
		metrics[0].ServiceTier != "priority" || !metrics[0].IsStream {
		t.Fatalf("failed first-output metric = %+v", metrics[0])
	}
	if metrics[1].AccountID != second.Account.ID || metrics[1].StatusCode != http.StatusOK || !metrics[1].IsStream {
		t.Fatalf("successful turn metric = %+v", metrics[1])
	}
	quotas := store.recordedQuotas()
	if len(quotas) != 1 || quotas[0].accountID != first.Account.ID || quotas[0].planID != first.Plan.ID || len(quotas[0].signals) != 2 {
		t.Fatalf("failed first-output quota records = %+v", quotas)
	}

	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","previous_response_id":"resp_1","input":"two"}`)
	gotSecond := waitResponsesWebSocketHTTPWrite(t, secondUpstream)
	if !strings.Contains(string(gotSecond), `"content":"two"`) || dialer.dialCount() != 2 {
		t.Fatalf("pinned account second request = %s dials=%d", gotSecond, dialer.dialCount())
	}
	secondUpstream.send(`{"type":"response.completed","response":{"id":"resp_2","usage":{"input_tokens":1}}}`)
	readResponsesWebSocketHTTP(t, client)
	metrics = waitResponsesWebSocketHTTPMetrics(t, store, 3)
	if metrics[2].AccountID != second.Account.ID || metrics[2].StatusCode != http.StatusOK {
		t.Fatalf("second turn metric = %+v", metrics[2])
	}

	if _, err := service.ResolveGatewayAccess(context.Background(), responsesWebSocketHTTPAPIKey, second.Account.ID); !errors.Is(err, domain.ErrAccountRateLimited) {
		t.Fatalf("failed account RPM was not consumed: %v", err)
	}
}

func TestResponsesWebSocketHTTPHandshakeFailureDoesNotSwitchAccount(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		wantCode   websocket.StatusCode
		wantReason string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, wantCode: websocket.StatusPolicyViolation, wantReason: "upstream Responses WebSocket authentication failed"},
		{name: "bad gateway", status: http.StatusBadGateway, wantCode: websocket.StatusInternalError, wantReason: "upstream Responses WebSocket handshake failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := responsesWebSocketHTTPConfig()
			httpServer, _, store, _, dialer := newResponsesWebSocketHTTPServer(t, config)
			defer httpServer.Close()
			first := addResponsesWebSocketHTTPCredential(t, store, "account-a")
			addResponsesWebSocketHTTPCredential(t, store, "account-b")
			dialer.attempts = []responsesWebSocketHTTPDialAttempt{{status: test.status, err: errors.New("upgrade rejected")}}

			client, _, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
			if err != nil {
				t.Fatalf("dial Responses WebSocket: %v", err)
			}
			defer client.CloseNow()
			writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","model":"gpt-5.6-sol"}`)
			closeErr := readResponsesWebSocketHTTPClose(t, client)
			if closeErr.Code != test.wantCode || closeErr.Reason != test.wantReason {
				t.Fatalf("close = %+v", closeErr)
			}
			if dialer.dialCount() != 1 {
				t.Fatalf("upstream dial count = %d, want 1", dialer.dialCount())
			}
			metrics := waitResponsesWebSocketHTTPMetrics(t, store, 1)
			wantMetricStatus := test.status
			wantMetricSource := domain.GatewayErrorSourceUpstream
			wantMetricCode := "websocket_handshake_error"
			if metrics[0].AccountID != first.Account.ID || metrics[0].StatusCode != wantMetricStatus ||
				metrics[0].ErrorSource != wantMetricSource || metrics[0].ErrorCode != wantMetricCode {
				t.Fatalf("handshake failure metric = %+v", metrics[0])
			}
		})
	}
}

func TestResponsesWebSocketHTTPHandshake429StopsAfterThreeAccountSwitches(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	httpServer, _, store, _, dialer := newResponsesWebSocketHTTPServer(t, config)
	defer httpServer.Close()
	for _, accountID := range []string{"account-a", "account-b", "account-c", "account-d", "account-e"} {
		addResponsesWebSocketHTTPCredential(t, store, accountID)
	}
	dialer.attempts = make([]responsesWebSocketHTTPDialAttempt, maxUpstreamAccountSwitches+1)
	for index := range dialer.attempts {
		dialer.attempts[index] = responsesWebSocketHTTPDialAttempt{status: http.StatusTooManyRequests, err: errors.New("upgrade rejected")}
	}

	client, _, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","model":"gpt-5.6-sol"}`)
	closeErr := readResponsesWebSocketHTTPClose(t, client)
	if closeErr.Code != websocket.StatusTryAgainLater || closeErr.Reason != "upstream rate limit exceeded, please retry later" {
		t.Fatalf("close = %+v", closeErr)
	}
	if dialer.dialCount() != maxUpstreamAccountSwitches+1 {
		t.Fatalf("upstream dial count = %d, want %d", dialer.dialCount(), maxUpstreamAccountSwitches+1)
	}
	if metrics := waitResponsesWebSocketHTTPMetrics(t, store, maxUpstreamAccountSwitches+1); len(metrics) != maxUpstreamAccountSwitches+1 {
		t.Fatalf("handshake metrics = %d", len(metrics))
	}
}

func TestResponsesWebSocketHTTPInvalidFirstFrameClosesWithPolicyViolation(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	httpServer, _, store, upstream, dialer := newResponsesWebSocketHTTPServer(t, config)
	defer httpServer.Close()

	client, response, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	if response == nil || response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade response = %#v", response)
	}
	writeResponsesWebSocketHTTP(t, client, `{"type":"response.append","model":"gpt-5.6-sol"}`)
	closeErr := readResponsesWebSocketHTTPClose(t, client)
	if closeErr.Code != websocket.StatusPolicyViolation ||
		closeErr.Reason != "response.append is not supported in WebSocket v2; use response.create with previous_response_id" {
		t.Fatalf("close = %+v", closeErr)
	}
	if dialer.dialCount() != 0 || len(store.recordedMetrics()) != 0 {
		t.Fatalf("invalid first frame reached turn setup: dials=%d metrics=%d", dialer.dialCount(), len(store.recordedMetrics()))
	}
	select {
	case payload := <-upstream.writes:
		t.Fatalf("invalid first frame reached upstream: %s", payload)
	default:
	}
}

func TestResponsesWebSocketHTTPFirstMessageTimeoutSendsPolicyCloseAndReleasesIngress(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	config.FirstMessageTimeout = 25 * time.Millisecond
	config.MaxSessionDuration = time.Second
	config.MaxConnectionsPerAPIKey = 1
	requestDone := make(chan struct{}, 1)
	httpServer, _, store, _, dialer := newResponsesWebSocketHTTPServerWithRequestDone(t, config, requestDone)
	defer httpServer.Close()

	client, _, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	closeErr := readResponsesWebSocketHTTPClose(t, client)
	client.CloseNow()
	if closeErr.Code != websocket.StatusPolicyViolation || closeErr.Reason != "missing first response.create message" {
		t.Fatalf("close = %+v", closeErr)
	}
	if dialer.dialCount() != 0 || len(store.recordedMetrics()) != 0 {
		t.Fatalf("first-message timeout reached turn setup: dials=%d metrics=%d", dialer.dialCount(), len(store.recordedMetrics()))
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("first-message timeout handler did not finish")
	}

	reconnected, response, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial after first-message timeout: response=%#v error=%v", response, err)
	}
	reconnected.CloseNow()
}

func TestResponsesWebSocketHTTPSessionDeadlineBeforeFirstMessageSendsGoingAway(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	config.FirstMessageTimeout = time.Second
	config.MaxSessionDuration = 25 * time.Millisecond
	httpServer, _, _, _, dialer := newResponsesWebSocketHTTPServer(t, config)
	defer httpServer.Close()

	client, _, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	closeErr := readResponsesWebSocketHTTPClose(t, client)
	if closeErr.Code != websocket.StatusGoingAway || closeErr.Reason != "Responses WebSocket session canceled" {
		t.Fatalf("close = %+v", closeErr)
	}
	if dialer.dialCount() != 0 {
		t.Fatalf("session deadline reached upstream dial: %d", dialer.dialCount())
	}
}

func TestResponsesWebSocketHTTPMaxSessionDurationReleasesAccountSlot(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	config.MaxSessionDuration = 35 * time.Millisecond
	config.ReadTimeout = time.Second
	httpServer, service, store, upstream, _ := newResponsesWebSocketHTTPServer(t, config)
	defer httpServer.Close()

	client, _, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","model":"gpt-5.6-sol"}`)
	waitResponsesWebSocketHTTPWrite(t, upstream)
	closeErr := readResponsesWebSocketHTTPClose(t, client)
	if closeErr.Code != websocket.StatusGoingAway || closeErr.Reason != "Responses WebSocket session canceled" {
		t.Fatalf("close = %+v", closeErr)
	}
	metrics := waitResponsesWebSocketHTTPMetrics(t, store, 1)
	if metrics[0].StatusCode != http.StatusBadGateway || metrics[0].ErrorCode != "websocket_error" {
		t.Fatalf("timeout metric = %+v", metrics[0])
	}
	probe, err := service.ResolveGatewayAccess(context.Background(), responsesWebSocketHTTPAPIKey)
	if err != nil {
		t.Fatalf("account slot remained occupied after session timeout: %v", err)
	}
	if probe.Release != nil {
		probe.Release()
	}
}

func TestResponsesWebSocketHTTPConnectionLimitRejectsSecondHandshake(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	config.MaxConnectionsPerAPIKey = 1
	requestDone := make(chan struct{}, 2)
	httpServer, _, _, _, _ := newResponsesWebSocketHTTPServerWithRequestDone(t, config, requestDone)
	defer httpServer.Close()

	first, _, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial first Responses WebSocket: %v", err)
	}
	defer first.CloseNow()
	second, response, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if second != nil {
		second.CloseNow()
	}
	if err == nil {
		t.Fatal("second Responses WebSocket handshake unexpectedly succeeded")
	}
	if response == nil || response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") != "5" {
		t.Fatalf("second handshake response = %#v, error = %v", response, err)
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("rejected WebSocket handler did not finish")
	}

	if err := first.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("close first Responses WebSocket: %v", err)
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("closed WebSocket handler did not finish")
	}
	third, response, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial after ingress slot release: response=%#v error=%v", response, err)
	}
	third.CloseNow()
}

func TestResponsesWebSocketHTTPChangedBindingClosesSecondTurnWithTryAgainLater(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	httpServer, _, store, upstream, dialer := newResponsesWebSocketHTTPServer(t, config)
	defer httpServer.Close()

	client, _, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","model":"gpt-5.6-sol"}`)
	waitResponsesWebSocketHTTPWrite(t, upstream)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1}}}`)
	readResponsesWebSocketHTTP(t, client)
	waitResponsesWebSocketHTTPMetrics(t, store, 1)

	store.changeBindingGeneration(8)
	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","previous_response_id":"resp_1"}`)
	closeErr := readResponsesWebSocketHTTPClose(t, client)
	if closeErr.Code != websocket.StatusTryAgainLater || closeErr.Reason != "account is unavailable for this turn; please reconnect" {
		t.Fatalf("close = %+v", closeErr)
	}
	if dialer.dialCount() != 1 {
		t.Fatalf("upstream dial count = %d, want 1", dialer.dialCount())
	}
	if len(store.recordedMetrics()) != 1 {
		t.Fatalf("changed binding unexpectedly recorded a turn metric: %+v", store.recordedMetrics())
	}
}

func TestResponsesWebSocketHTTPFastPolicyBlockWritesErrorAndMetric(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	blockedMessage := "priority is disabled for this API key"
	httpServer, _, store, upstream, dialer := newResponsesWebSocketHTTPServer(t, config, func(credential *domain.GatewayCredential) {
		credential.APIKeyFastPolicy = []domain.FastPolicyRule{{
			ServiceTier: "priority", Action: "block", FallbackAction: "pass", ErrorMessage: blockedMessage,
		}}
	})
	defer httpServer.Close()

	client, _, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","model":"gpt-5.6-sol","service_tier":"priority"}`)
	errorFrame := readResponsesWebSocketHTTP(t, client)
	var event struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(errorFrame, &event); err != nil {
		t.Fatalf("decode policy error frame: %v; payload=%s", err, errorFrame)
	}
	if event.Type != "error" || event.Error.Type != "invalid_request_error" || event.Error.Code != "permission_error" || event.Error.Message != blockedMessage {
		t.Fatalf("policy error frame = %+v", event)
	}
	closeErr := readResponsesWebSocketHTTPClose(t, client)
	if closeErr.Code != websocket.StatusPolicyViolation || closeErr.Reason != blockedMessage {
		t.Fatalf("close = %+v", closeErr)
	}
	metrics := waitResponsesWebSocketHTTPMetrics(t, store, 1)
	if metrics[0].StatusCode != http.StatusForbidden || metrics[0].ErrorSource != domain.GatewayErrorSourceRequest ||
		metrics[0].ErrorCode != "permission_error" || metrics[0].ErrorMessage != blockedMessage || !metrics[0].IsStream {
		t.Fatalf("policy metric = %+v", metrics[0])
	}
	if dialer.dialCount() != 0 {
		t.Fatalf("blocked policy dialed upstream %d times", dialer.dialCount())
	}
	select {
	case payload := <-upstream.writes:
		t.Fatalf("blocked policy reached upstream: %s", payload)
	default:
	}
}

func TestResponsesWebSocketHTTPSessionUpdateModelAppliesFastPolicyToLaterTurn(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	blockedMessage := "gpt-new priority is disabled"
	httpServer, service, store, upstream, dialer := newResponsesWebSocketHTTPServer(t, config, func(credential *domain.GatewayCredential) {
		credential.APIKeyFastPolicy = []domain.FastPolicyRule{{
			ServiceTier: "priority", Action: "block", ModelWhitelist: []string{"gpt-new"},
			FallbackAction: "pass", ErrorMessage: blockedMessage,
		}}
	})
	defer httpServer.Close()

	client, _, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","model":"gpt-old","service_tier":"priority"}`)
	firstRequest := waitResponsesWebSocketHTTPWrite(t, upstream)
	if !strings.Contains(string(firstRequest), `"model":"gpt-old"`) {
		t.Fatalf("first upstream request = %s", firstRequest)
	}
	writeResponsesWebSocketHTTP(t, client, `{"type":"session.update","session":{"model":"gpt-new"}}`)
	upstream.send(`{"type":"response.created","response":{"id":"resp_1"}}`)
	readResponsesWebSocketHTTP(t, client)
	update := waitResponsesWebSocketHTTPWrite(t, upstream)
	if string(update) != `{"type":"session.update","session":{"model":"gpt-new"}}` {
		t.Fatalf("forwarded session.update = %s", update)
	}
	upstream.send(`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1}}}`)
	readResponsesWebSocketHTTP(t, client)
	waitResponsesWebSocketHTTPMetrics(t, store, 1)

	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","previous_response_id":"resp_1","service_tier":"priority"}`)
	errorFrame := readResponsesWebSocketHTTP(t, client)
	var event struct {
		Type  string `json:"type"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(errorFrame, &event); err != nil {
		t.Fatalf("decode policy error frame: %v; payload=%s", err, errorFrame)
	}
	if event.Type != "error" || event.Error.Code != "permission_error" || event.Error.Message != blockedMessage {
		t.Fatalf("policy error frame = %+v", event)
	}
	closeErr := readResponsesWebSocketHTTPClose(t, client)
	if closeErr.Code != websocket.StatusPolicyViolation || closeErr.Reason != blockedMessage {
		t.Fatalf("close = %+v", closeErr)
	}
	metrics := waitResponsesWebSocketHTTPMetrics(t, store, 2)
	if metrics[1].StatusCode != http.StatusForbidden || metrics[1].Model != "gpt-new" ||
		metrics[1].ErrorCode != "permission_error" || metrics[1].ErrorMessage != blockedMessage {
		t.Fatalf("second-turn policy metric = %+v", metrics[1])
	}
	if dialer.dialCount() != 1 {
		t.Fatalf("upstream dial count = %d, want 1", dialer.dialCount())
	}
	select {
	case payload := <-upstream.writes:
		t.Fatalf("blocked second turn reached upstream: %s", payload)
	default:
	}
	probe, err := service.ResolveGatewayAccess(context.Background(), responsesWebSocketHTTPAPIKey)
	if err != nil {
		t.Fatalf("account slot remained occupied after policy block: %v", err)
	}
	if probe.Release != nil {
		probe.Release()
	}
}

func TestResponsesWebSocketHTTPFastPolicyCloseReasonIsBounded(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	blockedMessage := strings.Repeat("限", 200)
	httpServer, _, store, _, _ := newResponsesWebSocketHTTPServer(t, config, func(credential *domain.GatewayCredential) {
		credential.APIKeyFastPolicy = []domain.FastPolicyRule{{
			ServiceTier: "priority", Action: "block", FallbackAction: "pass", ErrorMessage: blockedMessage,
		}}
	})
	defer httpServer.Close()

	client, _, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","model":"gpt-5.6-sol","service_tier":"priority"}`)
	readResponsesWebSocketHTTP(t, client)
	closeErr := readResponsesWebSocketHTTPClose(t, client)
	if closeErr.Code != websocket.StatusPolicyViolation {
		t.Fatalf("close = %+v", closeErr)
	}
	if len(closeErr.Reason) > 123 || !strings.HasSuffix(closeErr.Reason, "...") {
		t.Fatalf("close reason is not safely truncated: bytes=%d reason=%q", len(closeErr.Reason), closeErr.Reason)
	}
	metrics := waitResponsesWebSocketHTTPMetrics(t, store, 1)
	if metrics[0].ErrorMessage != blockedMessage {
		t.Fatalf("metric error message was truncated: bytes=%d", len(metrics[0].ErrorMessage))
	}
}
