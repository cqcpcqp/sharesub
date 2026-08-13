package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/openai"
)

type responsesWebSocketShutdownTestDialer struct {
	mu     sync.Mutex
	closed int
}

func (*responsesWebSocketShutdownTestDialer) Dial(context.Context, string, http.Header, string) (openai.ResponsesWebSocketConn, int, http.Header, error) {
	return newResponsesWebSocketHTTPUpstream(), http.StatusSwitchingProtocols, nil, nil
}

func (d *responsesWebSocketShutdownTestDialer) Close() {
	d.mu.Lock()
	d.closed++
	d.mu.Unlock()
}

func (d *responsesWebSocketShutdownTestDialer) closeCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

func TestResponsesWebSocketShutdownClosesActiveUpgradeAndRejectsNewSessions(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	config.FirstMessageTimeout = time.Minute
	service, _ := newResponsesWebSocketHTTPService(t)
	server := New(service, nil, discardTestLogger(), config)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	defer server.Close()

	client, response, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	if response == nil || response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade response = %#v", response)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(shutdownCtx) }()

	closeErr := readResponsesWebSocketHTTPClose(t, client)
	if closeErr.Code != websocket.StatusGoingAway || closeErr.Reason != responsesWebSocketShutdownReason {
		t.Fatalf("shutdown close = %+v", closeErr)
	}
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not wait for and reap the WebSocket handler")
	}

	reconnected, reconnectResponse, reconnectErr := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if reconnected != nil {
		reconnected.CloseNow()
	}
	if reconnectErr == nil || reconnectResponse == nil || reconnectResponse.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("post-shutdown handshake response=%#v error=%v", reconnectResponse, reconnectErr)
	}
}

func TestResponsesWebSocketShutdownDuringActiveTurnDoesNotRecordUpstreamFailureAndReleasesCapacity(t *testing.T) {
	config := responsesWebSocketHTTPConfig()
	config.MaxConnectionsPerAPIKey = 1
	service, store := newResponsesWebSocketHTTPService(t)
	upstream := newResponsesWebSocketHTTPUpstream()
	dialer := &responsesWebSocketHTTPDialer{conn: upstream}
	server := New(service, nil, discardTestLogger(), config)
	server.responsesWebSocket = openai.NewResponsesWebSocketSession(openai.ResponsesWebSocketOptions{
		Dialer: dialer, DialTimeout: config.DialTimeout, ReadTimeout: config.ReadTimeout,
		WriteTimeout: config.WriteTimeout, InterTurnIdleTimeout: config.InterTurnIdleTimeout,
		UpstreamDrainTimeout: config.UpstreamDrainTimeout,
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	defer server.Close()

	client, response, err := dialResponsesWebSocketHTTP(t, httpServer.URL)
	if err != nil {
		t.Fatalf("dial Responses WebSocket: %v", err)
	}
	defer client.CloseNow()
	if response == nil || response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade response = %#v", response)
	}
	writeResponsesWebSocketHTTP(t, client, `{"type":"response.create","model":"gpt-5.6-sol"}`)
	waitResponsesWebSocketHTTPWrite(t, upstream)

	if probe, probeErr := service.ResolveGatewayAccess(context.Background(), responsesWebSocketHTTPAPIKey); !errors.Is(probeErr, domain.ErrAccountConcurrency) {
		if probe.Release != nil {
			probe.Release()
		}
		t.Fatalf("active turn account admission error = %v, want concurrency limit", probeErr)
	}
	if release, ok := server.webSocketIngress.acquire(responsesWebSocketHTTPAPIKey); ok {
		release()
		t.Fatal("active WebSocket did not hold the API key ingress slot")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(shutdownCtx) }()

	closeErr := readResponsesWebSocketHTTPClose(t, client)
	if closeErr.Code != websocket.StatusGoingAway || closeErr.Reason != responsesWebSocketShutdownReason {
		t.Fatalf("shutdown close = %+v", closeErr)
	}
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not wait for and reap the active turn")
	}

	probe, probeErr := service.ResolveGatewayAccess(context.Background(), responsesWebSocketHTTPAPIKey)
	if probeErr != nil {
		t.Fatalf("account slot remained occupied after shutdown: %v", probeErr)
	}
	if probe.Release != nil {
		probe.Release()
	}
	releaseIngress, ok := server.webSocketIngress.acquire(responsesWebSocketHTTPAPIKey)
	if !ok {
		t.Fatal("API key ingress slot remained occupied after shutdown")
	}
	releaseIngress()

	metrics := store.recordedMetrics()
	if len(metrics) != 1 || metrics[0].StatusCode != http.StatusServiceUnavailable ||
		metrics[0].ErrorSource != domain.GatewayErrorSourceGateway ||
		metrics[0].ErrorCode != "server_shutting_down" ||
		metrics[0].ErrorMessage != responsesWebSocketShutdownReason {
		t.Fatalf("planned shutdown metric = %+v", metrics)
	}
}

func TestResponsesWebSocketShutdownTimeoutForcesCloseNow(t *testing.T) {
	registry := newResponsesWebSocketSessionRegistry()
	client := &responsesWebSocketBlockingCloseConn{closeStarted: make(chan struct{}), closeNowStarted: make(chan struct{}), closeNowRelease: make(chan struct{})}
	activeSession, ok := registry.register(func(error) {})
	if !ok {
		t.Fatal("register active session")
	}
	if !activeSession.bindClient(client) {
		t.Fatal("bind active session client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	err := registry.shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v", err)
	}
	select {
	case <-client.closeStarted:
	default:
		t.Fatal("graceful close was not attempted")
	}
	client.mu.Lock()
	closeCode, closeReason := client.closeCode, client.closeReason
	client.mu.Unlock()
	if closeCode != websocket.StatusGoingAway || closeReason != responsesWebSocketShutdownReason {
		t.Fatalf("graceful close = %d %q", closeCode, closeReason)
	}
	select {
	case <-client.closeNowStarted:
	default:
		t.Fatal("CloseNow was not called after shutdown deadline")
	}
	close(client.closeNowRelease)
}

func TestResponsesWebSocketShutdownDeadlineDoesNotWaitForBlockingCloseNow(t *testing.T) {
	registry := newResponsesWebSocketSessionRegistry()
	client := &responsesWebSocketBlockingCloseConn{closeStarted: make(chan struct{}), closeNowStarted: make(chan struct{}), closeNowRelease: make(chan struct{})}
	activeSession, ok := registry.register(func(error) {})
	if !ok || !activeSession.bindClient(client) {
		t.Fatal("register and bind active session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- registry.shutdown(ctx) }()
	select {
	case err := <-shutdownDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown error = %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("shutdown waited for blocking CloseNow after its deadline")
	}
	select {
	case <-client.closeNowStarted:
	default:
		t.Fatal("blocking CloseNow was not started")
	}
	close(client.closeNowRelease)
}

func TestResponsesWebSocketShutdownErrorStillClosesTransport(t *testing.T) {
	dialer := &responsesWebSocketShutdownTestDialer{}
	server := New(nil, nil, discardTestLogger())
	server.responsesWebSocket = openai.NewResponsesWebSocketSession(openai.ResponsesWebSocketOptions{Dialer: dialer})
	client := &responsesWebSocketBlockingCloseConn{closeStarted: make(chan struct{}), closeNowStarted: make(chan struct{}), closeNowRelease: make(chan struct{})}
	activeSession, ok := server.webSocketSessions.register(func(error) {})
	if !ok || !activeSession.bindClient(client) {
		t.Fatal("register and bind active session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := server.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v", err)
	}
	if got := dialer.closeCount(); got != 1 {
		t.Fatalf("transport close count = %d, want 1", got)
	}
	close(client.closeNowRelease)
}

func TestResponsesWebSocketCloseIsIdempotentAndClosesTransport(t *testing.T) {
	dialer := &responsesWebSocketShutdownTestDialer{}
	server := New(nil, nil, discardTestLogger())
	server.responsesWebSocket = openai.NewResponsesWebSocketSession(openai.ResponsesWebSocketOptions{Dialer: dialer})
	server.Close()
	server.Close()
	if got := dialer.closeCount(); got != 1 {
		t.Fatalf("transport close count = %d, want 1", got)
	}
}

func TestResponsesWebSocketRegistryRejectsRegistrationAfterShutdown(t *testing.T) {
	registry := newResponsesWebSocketSessionRegistry()
	if err := registry.shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown empty registry: %v", err)
	}
	if _, ok := registry.register(func(error) {}); ok {
		t.Fatal("registration succeeded after shutdown")
	}
}

type responsesWebSocketBlockingCloseConn struct {
	closeStarted    chan struct{}
	closeNowStarted chan struct{}
	closeNowRelease chan struct{}
	closeCode       websocket.StatusCode
	closeReason     string
	mu              sync.Mutex
	startOnce       sync.Once
	nowOnce         sync.Once
}

func (*responsesWebSocketBlockingCloseConn) Read(context.Context) (websocket.MessageType, []byte, error) {
	return 0, nil, io.ErrClosedPipe
}

func (*responsesWebSocketBlockingCloseConn) Write(context.Context, websocket.MessageType, []byte) error {
	return nil
}

func (c *responsesWebSocketBlockingCloseConn) Close(code websocket.StatusCode, reason string) error {
	c.mu.Lock()
	c.closeCode = code
	c.closeReason = reason
	c.mu.Unlock()
	c.startOnce.Do(func() { close(c.closeStarted) })
	<-c.closeNowStarted
	return nil
}

func (c *responsesWebSocketBlockingCloseConn) CloseNow() error {
	c.nowOnce.Do(func() { close(c.closeNowStarted) })
	<-c.closeNowRelease
	return nil
}
