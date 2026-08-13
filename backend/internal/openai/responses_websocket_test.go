package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

type responsesWebSocketTestMessage struct {
	typ     websocket.MessageType
	payload []byte
	err     error
}

type responsesWebSocketTestConn struct {
	reads chan responsesWebSocketTestMessage
	done  chan struct{}

	mu           sync.Mutex
	writeTypes   []websocket.MessageType
	writes       [][]byte
	writeErr     error
	writeStarted chan struct{}
	writeRelease chan struct{}
	closed       bool
	activeReads  int
	maxReads     int
	closeOnce    sync.Once
}

type responsesWebSocketCloseOrderConn struct {
	*responsesWebSocketTestConn
	closeCalled chan struct{}
}

func (c *responsesWebSocketCloseOrderConn) Close(websocket.StatusCode, string) error {
	select {
	case <-c.closeCalled:
	default:
		close(c.closeCalled)
	}
	<-c.done
	return nil
}

func newResponsesWebSocketTestConn(messages ...string) *responsesWebSocketTestConn {
	conn := &responsesWebSocketTestConn{reads: make(chan responsesWebSocketTestMessage, len(messages)+4), done: make(chan struct{})}
	for _, message := range messages {
		conn.reads <- responsesWebSocketTestMessage{typ: websocket.MessageText, payload: []byte(message)}
	}
	return conn
}

func (c *responsesWebSocketTestConn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	c.mu.Lock()
	c.activeReads++
	if c.activeReads > c.maxReads {
		c.maxReads = c.activeReads
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.activeReads--
		c.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-c.done:
		return 0, nil, io.ErrClosedPipe
	case message := <-c.reads:
		return message.typ, append([]byte(nil), message.payload...), message.err
	}
}

func (c *responsesWebSocketTestConn) Write(_ context.Context, messageType websocket.MessageType, payload []byte) error {
	c.mu.Lock()
	if c.writeErr != nil {
		defer c.mu.Unlock()
		return c.writeErr
	}
	writeStarted := c.writeStarted
	writeRelease := c.writeRelease
	c.mu.Unlock()
	if writeStarted != nil {
		select {
		case writeStarted <- struct{}{}:
		default:
		}
	}
	if writeRelease != nil {
		<-writeRelease
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writeErr != nil {
		return c.writeErr
	}
	c.writeTypes = append(c.writeTypes, messageType)
	c.writes = append(c.writes, append([]byte(nil), payload...))
	return nil
}

func (c *responsesWebSocketTestConn) Close(websocket.StatusCode, string) error { return c.CloseNow() }
func (c *responsesWebSocketTestConn) CloseNow() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		close(c.done)
	})
	return nil
}

func (c *responsesWebSocketTestConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *responsesWebSocketTestConn) maxConcurrentReads() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxReads
}

func (c *responsesWebSocketTestConn) written() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([][]byte, len(c.writes))
	for index := range c.writes {
		result[index] = append([]byte(nil), c.writes[index]...)
	}
	return result
}

func (c *responsesWebSocketTestConn) writtenTypes() []websocket.MessageType {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]websocket.MessageType(nil), c.writeTypes...)
}

func (c *responsesWebSocketTestConn) send(message string) {
	c.reads <- responsesWebSocketTestMessage{typ: websocket.MessageText, payload: []byte(message)}
}

func (c *responsesWebSocketTestConn) sendFrame(messageType websocket.MessageType, payload []byte) {
	c.reads <- responsesWebSocketTestMessage{typ: messageType, payload: append([]byte(nil), payload...)}
}

func (c *responsesWebSocketTestConn) sendError(err error) {
	c.reads <- responsesWebSocketTestMessage{err: err}
}

func (c *responsesWebSocketTestConn) failWrites(err error) {
	c.mu.Lock()
	c.writeErr = err
	c.mu.Unlock()
}

func (c *responsesWebSocketTestConn) blockWrites() (<-chan struct{}, func()) {
	c.mu.Lock()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	c.writeStarted = started
	c.writeRelease = release
	c.mu.Unlock()
	var once sync.Once
	return started, func() { once.Do(func() { close(release) }) }
}

func waitForResponsesWebSocketWrites(t *testing.T, conn *responsesWebSocketTestConn, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(conn.written()) >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d WebSocket writes; got %d", count, len(conn.written()))
}

type responsesWebSocketTestDialer struct {
	conn            ResponsesWebSocketConn
	headers         http.Header
	count           int
	status          int
	responseHeaders http.Header
	err             error
}

type responsesWebSocketSequenceDialer struct {
	mu      sync.Mutex
	conns   []ResponsesWebSocketConn
	headers []http.Header
}

func (d *responsesWebSocketSequenceDialer) Dial(_ context.Context, _ string, headers http.Header, _ string) (ResponsesWebSocketConn, int, http.Header, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.headers = append(d.headers, headers.Clone())
	index := len(d.headers) - 1
	if index >= len(d.conns) {
		return nil, http.StatusBadGateway, nil, errors.New("unexpected dial")
	}
	return d.conns[index], http.StatusSwitchingProtocols, http.Header{"X-Request-Id": []string{fmt.Sprintf("handshake-%d", index+1)}}, nil
}

func (d *responsesWebSocketSequenceDialer) dialHeaders() []http.Header {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]http.Header, len(d.headers))
	for index := range d.headers {
		result[index] = d.headers[index].Clone()
	}
	return result
}

func (d *responsesWebSocketTestDialer) Dial(_ context.Context, _ string, headers http.Header, _ string) (ResponsesWebSocketConn, int, http.Header, error) {
	d.count++
	d.headers = headers.Clone()
	if d.err != nil {
		return nil, d.status, d.responseHeaders, d.err
	}
	return d.conn, http.StatusSwitchingProtocols, http.Header{"X-Request-Id": []string{"handshake-request"}}, nil
}

func TestResponsesWebSocketProxyClientPriority(t *testing.T) {
	for _, test := range []struct {
		name          string
		accountProxy  string
		outboundProxy string
		wantProxy     string
	}{
		{
			name:          "account proxy overrides global proxy",
			accountProxy:  "socks5://account-proxy.example:1080",
			outboundProxy: "http://global-proxy.example:8080",
			wantProxy:     "socks5://account-proxy.example:1080",
		},
		{
			name:          "global proxy is used without account proxy",
			outboundProxy: "http://global-proxy.example:8080",
			wantProxy:     "http://global-proxy.example:8080",
		},
		{name: "no explicit proxy keeps the default client"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dialer := &coderResponsesWebSocketDialer{outboundProxyURL: test.outboundProxy}
			client, err := dialer.proxyHTTPClient(test.accountProxy)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantProxy == "" {
				if client != nil {
					t.Fatalf("client = %#v, want nil", client)
				}
				return
			}
			transport, ok := client.Transport.(*http.Transport)
			if !ok || transport.Proxy == nil {
				t.Fatalf("transport = %#v", client.Transport)
			}
			request, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/backend-api/codex/responses", nil)
			if err != nil {
				t.Fatal(err)
			}
			proxy, err := transport.Proxy(request)
			if err != nil {
				t.Fatal(err)
			}
			if proxy == nil || proxy.String() != test.wantProxy {
				t.Fatalf("proxy = %v, want %q", proxy, test.wantProxy)
			}
		})
	}
}

func TestResponsesWebSocketSessionPassesGlobalProxyToDefaultDialer(t *testing.T) {
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{OutboundProxyURL: "http://global-proxy.example:8080"})
	dialer, ok := session.dialer.(*coderResponsesWebSocketDialer)
	if !ok {
		t.Fatalf("dialer = %T", session.dialer)
	}
	if dialer.outboundProxyURL != "http://global-proxy.example:8080" {
		t.Fatalf("outbound proxy = %q", dialer.outboundProxyURL)
	}
}

func TestResponsesWebSocketProxyClientIsReused(t *testing.T) {
	dialer := &coderResponsesWebSocketDialer{}
	first, err := dialer.proxyHTTPClient("http://proxy.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	second, err := dialer.proxyHTTPClient("http://proxy.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(dialer.proxyClients) != 1 {
		t.Fatalf("proxy clients were not reused: first=%p second=%p size=%d", first, second, len(dialer.proxyClients))
	}
	transport, ok := first.Transport.(*http.Transport)
	if !ok || transport.IdleConnTimeout != wsProxyIdleConnTimeout || transport.MaxIdleConns != wsProxyMaxIdleConns ||
		transport.MaxIdleConnsPerHost != wsProxyMaxIdlePerHost || transport.TLSHandshakeTimeout != defaultWSDialTimeout {
		t.Fatalf("proxy transport = %#v", first.Transport)
	}
}

func TestResponsesWebSocketProxyClientCacheIsBounded(t *testing.T) {
	dialer := &coderResponsesWebSocketDialer{}
	for index := 0; index < wsProxyClientCacheLimit+10; index++ {
		proxyURL := fmt.Sprintf("http://proxy-%d.example:8080", index)
		if _, err := dialer.proxyHTTPClient(proxyURL); err != nil {
			t.Fatal(err)
		}
	}
	if len(dialer.proxyClients) != wsProxyClientCacheLimit {
		t.Fatalf("proxy client cache size = %d, want %d", len(dialer.proxyClients), wsProxyClientCacheLimit)
	}
}

func TestResponsesWebSocketProxyClientCacheCloses(t *testing.T) {
	dialer := &coderResponsesWebSocketDialer{}
	client, err := dialer.proxyHTTPClient("http://proxy.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	if client == nil || len(dialer.proxyClients) != 1 {
		t.Fatalf("proxy cache was not populated: client=%#v size=%d", client, len(dialer.proxyClients))
	}
	dialer.Close()
	if len(dialer.proxyClients) != 0 {
		t.Fatalf("proxy client cache size after Close = %d", len(dialer.proxyClients))
	}
	if _, err := dialer.proxyHTTPClient("http://proxy.example:8080"); err == nil {
		t.Fatal("proxy cache was reusable after Close")
	}
	dialer.Close()
}

func TestPrepareResponsesWebSocketFrame(t *testing.T) {
	frame, billing, _, err := prepareResponsesWebSocketFrame([]byte(`{
		"model":"gpt-5.6-sol","input":"hello","background":true,"store":true,
		"reasoning":{"effort":"high"},"temperature":0.1
	}`), 1, "")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(frame, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["type"] != "response.create" || payload["store"] != false || payload["stream"] != true {
		t.Fatalf("normalized payload = %s", frame)
	}
	if _, exists := payload["background"]; exists {
		t.Fatalf("background was retained: %s", frame)
	}
	if _, exists := payload["temperature"]; exists {
		t.Fatalf("unsupported field was retained: %s", frame)
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input was not normalized: %#v", payload["input"])
	}
	if billing.Model != "gpt-5.6-sol" || !billing.Stream {
		t.Fatalf("billing = %+v", billing)
	}
	include, ok := payload["include"].([]any)
	if !ok || len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v", payload["include"])
	}
}

func TestPrepareResponsesWebSocketFramePreservesExplicitStream(t *testing.T) {
	frame, billing, err := PrepareResponsesWebSocketFrame([]byte(`{
		"model":"gpt-5.6-sol","stream":false,"unknown":{"kept":true},"temperature":0.1
	}`), "")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(frame, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["stream"] != false || billing.Stream {
		t.Fatalf("explicit stream=false was not preserved: %s / %+v", frame, billing)
	}
	if _, exists := payload["unknown"]; !exists {
		t.Fatalf("unknown field was removed: %s", frame)
	}
	if _, exists := payload["temperature"]; exists {
		t.Fatalf("known unsupported field was retained: %s", frame)
	}
}

func TestPrepareResponsesWebSocketFrameInheritsModel(t *testing.T) {
	frame, billing, previous, err := prepareResponsesWebSocketFrame(
		[]byte(`{"type":"response.create","previous_response_id":"resp_1"}`),
		2,
		"gpt-5.6-sol",
	)
	if err != nil {
		t.Fatal(err)
	}
	if billing.Model != "gpt-5.6-sol" || previous != "resp_1" {
		t.Fatalf("billing = %+v, previous = %q", billing, previous)
	}
	var payload map[string]any
	if err := json.Unmarshal(frame, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "gpt-5.6-sol" {
		t.Fatalf("model = %#v", payload["model"])
	}
}

func TestResponsesWebSocketResponseCreateClassificationMatchesUpstreamProtocol(t *testing.T) {
	for _, test := range []struct {
		name        string
		messageType websocket.MessageType
		frame       string
		want        bool
	}{
		{name: "text create", messageType: websocket.MessageText, frame: `{"type":"response.create"}`, want: true},
		{name: "binary create passes upstream", messageType: websocket.MessageBinary, frame: `{"type":"response.create"}`},
		{name: "missing type passes upstream", messageType: websocket.MessageText, frame: `{"model":"gpt-5.6-sol"}`},
		{name: "cancel control", messageType: websocket.MessageText, frame: `{"type":"response.cancel"}`},
		{name: "invalid JSON passes upstream", messageType: websocket.MessageText, frame: `{`},
		{name: "empty type passes upstream", messageType: websocket.MessageText, frame: `{"type":""}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isResponsesWebSocketResponseCreate(test.messageType, []byte(test.frame)); got != test.want {
				t.Fatalf("classification = %v, want %v", got, test.want)
			}
		})
	}
}

func TestResponsesWebSocketHeaders(t *testing.T) {
	inbound := make(http.Header)
	inbound.Set("session_id", "session")
	inbound.Set("conversation_id", "conversation")
	inbound.Set("Accept-Language", "zh-CN")
	headers, err := responsesWebSocketHeaders(ResponsesWebSocketDialConfig{
		AccessToken: "access", ChatGPTAccountID: "account", APIKeyID: "key", InboundHeader: inbound,
	}, "cache")
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get("Authorization") != "Bearer access" || headers.Get("ChatGPT-Account-ID") != "account" ||
		headers.Get("OpenAI-Beta") != responsesWebSocketBetaV2 || headers.Get("Version") != codexProbeVersion ||
		headers.Get("Originator") != codexDefaultOriginator || headers.Get("User-Agent") != codexProbeUserAgent || headers.Get("Accept-Language") != "zh-CN" {
		t.Fatalf("headers = %#v", headers)
	}
	if headers.Get("session_id") != isolateSession("key", "session") || headers.Get("conversation_id") != isolateSession("key", "conversation") {
		t.Fatalf("session headers = %#v", headers)
	}
}

func TestResponsesWebSocketSessionKeepsUpstreamAcrossTurnsAndReportsUsage(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	dialer := &responsesWebSocketTestDialer{conn: upstream}
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: dialer, DialTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, InterTurnIdleTimeout: 20 * time.Millisecond,
	})
	var results []ResponsesWebSocketTurnResult
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{
					Frame: request.Frame,
					Dial:  &ResponsesWebSocketDialConfig{AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key"},
				}, nil
			},
			AfterTurn: func(_ context.Context, _ int, result ResponsesWebSocketTurnResult, turnErr error) {
				if turnErr != nil {
					t.Errorf("turn error: %v", turnErr)
				}
				results = append(results, result)
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.output_text.delta","response_id":"resp_1","delta":"one"}`)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.6-sol","usage":{"input_tokens":5,"output_tokens":2,"input_tokens_details":{"cached_tokens":3}}}}`)
	waitForResponsesWebSocketWrites(t, client, 2)
	client.send(`{"type":"response.create","previous_response_id":"resp_1"}`)
	waitForResponsesWebSocketWrites(t, upstream, 2)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_2","model":"gpt-5.6-sol","usage":{"input_tokens":7,"output_tokens":4}}}`)
	waitForResponsesWebSocketWrites(t, client, 3)
	err := <-runErr
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusNormalClosure {
		t.Fatalf("Run() error = %v", err)
	}
	if dialer.count != 1 {
		t.Fatalf("dial count = %d", dialer.count)
	}
	if len(upstream.written()) != 2 || len(results) != 2 || len(client.written()) != 3 {
		t.Fatalf("upstream writes=%d results=%d client writes=%d", len(upstream.written()), len(results), len(client.written()))
	}
	if results[0].RequestID != "resp_1" || results[0].Metrics.InputTokens != 5 || results[0].Metrics.CachedTokens != 3 || results[0].Metrics.OutputTokens != 2 {
		t.Fatalf("first result = %+v", results[0])
	}
	if results[0].Metrics.TTFT <= 0 {
		t.Fatalf("first turn TTFT = %v", results[0].Metrics.TTFT)
	}
	if results[1].RequestID != "resp_2" || results[1].Metrics.InputTokens != 7 || results[1].Metrics.OutputTokens != 4 {
		t.Fatalf("second result = %+v", results[1])
	}
	if results[0].ResponseHeaders.Get("X-Request-Id") != "handshake-request" || results[1].ResponseHeaders != nil {
		t.Fatalf("response headers = %#v / %#v", results[0].ResponseHeaders, results[1].ResponseHeaders)
	}
}

func TestResponsesWebSocketSessionRetriesFirstRateLimitErrorBeforeDownstream(t *testing.T) {
	firstUpstream := newResponsesWebSocketTestConn()
	secondUpstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	dialer := &responsesWebSocketSequenceDialer{conns: []ResponsesWebSocketConn{firstUpstream, secondUpstream}}
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: dialer, DialTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, InterTurnIdleTimeout: 20 * time.Millisecond,
	})
	var hookCalls int
	var afterResults []ResponsesWebSocketTurnResult
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":"one"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token-a", ChatGPTAccountID: "account-a", APIKeyID: "key",
				}}, nil
			},
			OnFirstUpstreamError: func(_ context.Context, request ResponsesWebSocketTurnRequest, result ResponsesWebSocketTurnResult, upstreamErr *ResponsesWebSocketUpstreamEventError) (ResponsesWebSocketTurnConfig, error) {
				hookCalls++
				if request.Turn != 1 || upstreamErr.Code != "insufficient_quota" || string(upstreamErr.Frame) == "" ||
					result.Metrics.ErrorStatusCode != http.StatusTooManyRequests || result.ResponseHeaders.Get("X-Request-Id") != "handshake-1" {
					t.Errorf("retry hook request=%+v result=%+v upstreamErr=%+v", request, result, upstreamErr)
				}
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token-b", ChatGPTAccountID: "account-b", APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, _ int, result ResponsesWebSocketTurnResult, _ error) {
				afterResults = append(afterResults, result)
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, firstUpstream, 1)
	// This frame is already queued behind the initial create. The relay must
	// not consume or send it to the account that is about to fail over.
	client.send(`{"type":"session.update","session":{"model":"gpt-5.6-sol"}}`)
	firstUpstream.send(`{"type":"error","error":{"type":"usage_limit_error","code":"insufficient_quota","message":"usage limit reached"}}`)
	waitForResponsesWebSocketWrites(t, secondUpstream, 1)
	if len(client.written()) != 0 {
		t.Fatalf("rate-limit error leaked downstream: %q", client.written())
	}
	if !firstUpstream.isClosed() {
		t.Fatal("failed upstream was not closed before retry")
	}
	if len(firstUpstream.written()) != 1 || len(secondUpstream.written()) != 1 {
		t.Fatalf("first writes=%d second writes=%d", len(firstUpstream.written()), len(secondUpstream.written()))
	}
	if string(firstUpstream.written()[0]) != string(secondUpstream.written()[0]) {
		t.Fatalf("first frame was not replayed exactly once:\nfirst=%s\nsecond=%s", firstUpstream.written()[0], secondUpstream.written()[0])
	}
	secondUpstream.send(`{"type":"response.created","response":{"id":"resp_retry"}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	waitForResponsesWebSocketWrites(t, secondUpstream, 2)
	if got := string(secondUpstream.written()[1]); got != `{"type":"session.update","session":{"model":"gpt-5.6-sol"}}` {
		t.Fatalf("queued control frame = %s", got)
	}
	secondUpstream.send(`{"type":"response.completed","response":{"id":"resp_retry","usage":{"input_tokens":2}}}`)
	waitForResponsesWebSocketWrites(t, client, 2)
	if err := <-runErr; err == nil {
		t.Fatal("Run() unexpectedly returned nil")
	}
	if hookCalls != 1 || len(afterResults) != 1 || afterResults[0].RequestID != "resp_retry" {
		t.Fatalf("hook calls=%d results=%+v", hookCalls, afterResults)
	}
	if len(dialer.dialHeaders()) != 2 || firstUpstream.maxConcurrentReads() != 1 || secondUpstream.maxConcurrentReads() != 1 || client.maxConcurrentReads() != 1 {
		t.Fatalf("dials=%d read concurrency first=%d second=%d client=%d", len(dialer.dialHeaders()), firstUpstream.maxConcurrentReads(), secondUpstream.maxConcurrentReads(), client.maxConcurrentReads())
	}
}

func TestResponsesWebSocketCancelBeforeFirstDownstreamPinsAttemptAndPreservesControlFIFO(t *testing.T) {
	firstUpstream := newResponsesWebSocketTestConn()
	secondUpstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	dialer := &responsesWebSocketSequenceDialer{conns: []ResponsesWebSocketConn{firstUpstream, secondUpstream}}
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: dialer, DialTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, InterTurnIdleTimeout: 20 * time.Millisecond,
	})
	var hookCalls int
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token-a", ChatGPTAccountID: "account-a", APIKeyID: "key",
				}}, nil
			},
			OnFirstUpstreamError: func(_ context.Context, request ResponsesWebSocketTurnRequest, _ ResponsesWebSocketTurnResult, _ *ResponsesWebSocketUpstreamEventError) (ResponsesWebSocketTurnConfig, error) {
				hookCalls++
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token-b", ChatGPTAccountID: "account-b", APIKeyID: "key",
				}}, nil
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, firstUpstream, 1)
	client.send(`{"type":"session.update","session":{"model":"gpt-5.6-sol"}}`)
	client.send(`{"type":"response.cancel"}`)
	waitForResponsesWebSocketWrites(t, firstUpstream, 3)
	if got := string(firstUpstream.written()[1]); got != `{"type":"session.update","session":{"model":"gpt-5.6-sol"}}` {
		t.Fatalf("first queued control = %s", got)
	}
	if got := string(firstUpstream.written()[2]); got != `{"type":"response.cancel"}` {
		t.Fatalf("cancel control = %s", got)
	}
	firstUpstream.send(`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"rate limit exceeded"}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	firstUpstream.send(`{"type":"response.cancelled","response":{"id":"resp_cancel","usage":{"input_tokens":1}}}`)
	waitForResponsesWebSocketWrites(t, client, 2)
	if err := <-runErr; err == nil {
		t.Fatal("Run() unexpectedly returned nil")
	}
	if hookCalls != 0 || len(dialer.dialHeaders()) != 1 || len(secondUpstream.written()) != 0 {
		t.Fatalf("retry hook calls=%d dials=%d second writes=%d", hookCalls, len(dialer.dialHeaders()), len(secondUpstream.written()))
	}
}

func TestResponsesWebSocketFirstFailoverUsesCloseNowBeforeReaderJoin(t *testing.T) {
	firstUpstream := &responsesWebSocketCloseOrderConn{
		responsesWebSocketTestConn: newResponsesWebSocketTestConn(), closeCalled: make(chan struct{}),
	}
	secondUpstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer:      &responsesWebSocketSequenceDialer{conns: []ResponsesWebSocketConn{firstUpstream, secondUpstream}},
		DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
		InterTurnIdleTimeout: 20 * time.Millisecond,
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{AccessToken: "a", ChatGPTAccountID: "a", APIKeyID: "key"}}, nil
			},
			OnFirstUpstreamError: func(_ context.Context, request ResponsesWebSocketTurnRequest, _ ResponsesWebSocketTurnResult, _ *ResponsesWebSocketUpstreamEventError) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{AccessToken: "b", ChatGPTAccountID: "b", APIKeyID: "key"}}, nil
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, firstUpstream.responsesWebSocketTestConn, 1)
	firstUpstream.send(`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"rate limit exceeded"}}`)
	waitForResponsesWebSocketWrites(t, secondUpstream, 1)
	select {
	case <-firstUpstream.closeCalled:
		t.Fatal("graceful Close was called during failover")
	default:
	}
	secondUpstream.send(`{"type":"response.completed","response":{"id":"resp_ok"}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	if err := <-runErr; err == nil {
		t.Fatal("Run() unexpectedly returned nil")
	}
}

func TestResponsesWebSocketSessionObservesClientDisconnectBeforeFirstDownstream(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Minute, WriteTimeout: time.Second, UpstreamDrainTimeout: 20 * time.Millisecond,
	})
	afterResult := make(chan ResponsesWebSocketTurnResult, 1)
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			OnFirstUpstreamError: func(context.Context, ResponsesWebSocketTurnRequest, ResponsesWebSocketTurnResult, *ResponsesWebSocketUpstreamEventError) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{}, errors.New("must not retry after client disconnect")
			},
			AfterTurn: func(_ context.Context, _ int, result ResponsesWebSocketTurnResult, _ error) {
				afterResult <- result
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	client.sendError(io.EOF)
	select {
	case err := <-runErr:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("client disconnect before first downstream waited for upstream read timeout")
	}
	result := <-afterResult
	if !result.Metrics.ClientDisconnected || result.Metrics.ErrorStatusCode != 499 {
		t.Fatalf("turn result = %+v", result)
	}
}

func TestResponsesWebSocketPreDownstreamControlQueueIsBounded(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Minute, WriteTimeout: time.Second,
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			OnFirstUpstreamError: func(context.Context, ResponsesWebSocketTurnRequest, ResponsesWebSocketTurnResult, *ResponsesWebSocketUpstreamEventError) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{}, errors.New("unused")
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	for index := 0; index <= wsPreDownstreamQueueMaxFrames; index++ {
		client.send(`{"type":"session.update","session":{"model":"gpt-5.6-sol"}}`)
	}
	err := <-runErr
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusPolicyViolation ||
		closeErr.Reason() != "too many client frames before the first upstream response" {
		t.Fatalf("Run() error = %v", err)
	}
	if len(upstream.written()) != 1 {
		t.Fatalf("queued pre-downstream controls reached upstream: %d writes", len(upstream.written()))
	}
}

func TestResponsesWebSocketPreDownstreamControlQueueEnforcesByteLimit(t *testing.T) {
	var pending responsesWebSocketPendingClientFrames
	first := responsesWebSocketRead{messageType: websocket.MessageBinary, frame: make([]byte, wsPreDownstreamQueueMaxBytes)}
	if err := pending.append(first); err != nil {
		t.Fatalf("append at byte limit: %v", err)
	}
	if pending.bytes != wsPreDownstreamQueueMaxBytes || len(pending.frames) != 1 {
		t.Fatalf("pending queue = %d bytes / %d frames", pending.bytes, len(pending.frames))
	}
	err := pending.append(responsesWebSocketRead{messageType: websocket.MessageBinary, frame: []byte{1}})
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusPolicyViolation {
		t.Fatalf("append past byte limit error = %v", err)
	}
}

func TestResponsesWebSocketFirstDownstreamWriteFailureDoesNotFlushQueuedControls(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	client.failWrites(errors.New("client write failed"))
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, UpstreamDrainTimeout: 20 * time.Millisecond,
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			OnFirstUpstreamError: func(context.Context, ResponsesWebSocketTurnRequest, ResponsesWebSocketTurnResult, *ResponsesWebSocketUpstreamEventError) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{}, errors.New("unused")
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	client.send(`{"type":"session.update","session":{"model":"gpt-5.6-sol"}}`)
	upstream.send(`{"type":"response.created","response":{"id":"resp_write_fail"}}`)
	if err := <-runErr; err == nil || err.Error() != "client write failed" {
		t.Fatalf("Run() error = %v", err)
	}
	if len(upstream.written()) != 1 {
		t.Fatalf("queued controls flushed after failed first downstream write: %d writes", len(upstream.written()))
	}
}

func TestResponsesWebSocketSessionDoesNotRetryRateLimitAfterDownstream(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: 20 * time.Millisecond,
	})
	var hookCalls int
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key"}}, nil
			},
			OnFirstUpstreamError: func(context.Context, ResponsesWebSocketTurnRequest, ResponsesWebSocketTurnResult, *ResponsesWebSocketUpstreamEventError) (ResponsesWebSocketTurnConfig, error) {
				hookCalls++
				return ResponsesWebSocketTurnConfig{}, errors.New("must not retry")
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.created","response":{"id":"resp_1"}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	upstream.send(`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"rate limit exceeded"}}`)
	waitForResponsesWebSocketWrites(t, client, 2)
	upstream.send(`{"type":"response.failed","response":{"id":"resp_1"}}`)
	waitForResponsesWebSocketWrites(t, client, 3)
	if err := <-runErr; err == nil {
		t.Fatal("Run() unexpectedly returned nil")
	}
	if hookCalls != 0 {
		t.Fatalf("retry hook calls = %d", hookCalls)
	}
}

func TestResponsesWebSocketSessionDoesNotRetryFirstErrorOfLaterTurn(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	dialer := &responsesWebSocketTestDialer{conn: upstream}
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: dialer, DialTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, InterTurnIdleTimeout: 20 * time.Millisecond,
	})
	var hookCalls int
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key"}}, nil
			},
			OnFirstUpstreamError: func(context.Context, ResponsesWebSocketTurnRequest, ResponsesWebSocketTurnResult, *ResponsesWebSocketUpstreamEventError) (ResponsesWebSocketTurnConfig, error) {
				hookCalls++
				return ResponsesWebSocketTurnConfig{}, errors.New("must not retry")
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_1"}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	client.send(`{"type":"response.create","previous_response_id":"resp_1"}`)
	waitForResponsesWebSocketWrites(t, upstream, 2)
	upstream.send(`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"rate limit exceeded"}}`)
	waitForResponsesWebSocketWrites(t, client, 2)
	upstream.send(`{"type":"response.failed","response":{"id":"resp_2"}}`)
	waitForResponsesWebSocketWrites(t, client, 3)
	if err := <-runErr; err == nil {
		t.Fatal("Run() unexpectedly returned nil")
	}
	if hookCalls != 0 || dialer.count != 1 {
		t.Fatalf("retry hook calls=%d dials=%d", hookCalls, dialer.count)
	}
}

func TestResponsesWebSocketRateLimitClassifierMatchesSub2API(t *testing.T) {
	for _, test := range []struct {
		name string
		err  ResponsesWebSocketUpstreamEventError
		want bool
	}{
		{name: "rate type", err: ResponsesWebSocketUpstreamEventError{Type: "rate_limit_error"}, want: true},
		{name: "usage type", err: ResponsesWebSocketUpstreamEventError{Type: "usage_limit_error"}, want: true},
		{name: "quota code", err: ResponsesWebSocketUpstreamEventError{Code: "insufficient_quota"}, want: true},
		{name: "usage message", err: ResponsesWebSocketUpstreamEventError{Message: "Usage limit reached"}, want: true},
		{name: "rate message", err: ResponsesWebSocketUpstreamEventError{Message: "Rate limit exceeded"}, want: true},
		{name: "policy", err: ResponsesWebSocketUpstreamEventError{Type: "invalid_request_error", Message: "policy violation"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isResponsesWebSocketRateLimitError(&test.err); got != test.want {
				t.Fatalf("classification = %v, want %v", got, test.want)
			}
		})
	}
}

func TestResponsesWebSocketTerminalWithoutOutputDoesNotSetTTFT(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: 10 * time.Millisecond,
	})
	var result ResponsesWebSocketTurnResult
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, _ int, got ResponsesWebSocketTurnResult, _ error) { result = got },
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.created","response":{"id":"resp_no_output"}}`)
	upstream.send(`{"type":"response.in_progress","response":{"id":"resp_no_output"}}`)
	upstream.send(`{"type":"response.output_item.added","response_id":"resp_no_output"}`)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_no_output","usage":{"input_tokens":1}}}`)
	if err := <-runErr; err == nil {
		t.Fatal("Run() unexpectedly returned nil")
	}
	if result.Metrics.TTFT != 0 {
		t.Fatalf("terminal-only turn TTFT = %v, want zero", result.Metrics.TTFT)
	}
}

func TestResponsesWebSocketSessionRejectsOverlappingResponseCreate(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: time.Second,
	})
	var afterCalls int
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, _ int, _ ResponsesWebSocketTurnResult, _ error) { afterCalls++ },
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.created","response":{"id":"resp_active"}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	client.send(`{"type":"response.create","model":"gpt-5.6-sol"}`)
	err := <-runErr
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusPolicyViolation ||
		closeErr.Reason() != "overlapping response.create is not supported" {
		t.Fatalf("Run() error = %v", err)
	}
	if afterCalls != 1 {
		t.Fatalf("AfterTurn calls = %d, want 1", afterCalls)
	}
	if len(upstream.written()) != 1 {
		t.Fatalf("overlapping turn reached upstream: %d writes", len(upstream.written()))
	}
}

func TestResponsesWebSocketSessionForwardsControlFramesDuringActiveTurn(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: 20 * time.Millisecond,
	})
	var result ResponsesWebSocketTurnResult
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, _ int, got ResponsesWebSocketTurnResult, _ error) { result = got },
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.created","response":{"id":"resp_cancel"}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	client.send(`{"type":"response.cancel","response_id":"resp_cancel"}`)
	waitForResponsesWebSocketWrites(t, upstream, 2)
	if got := string(upstream.written()[1]); got != `{"type":"response.cancel","response_id":"resp_cancel"}` {
		t.Fatalf("forwarded control frame = %s", got)
	}
	upstream.send(`{"type":"response.cancelled","response":{"id":"resp_cancel","usage":{"input_tokens":2,"output_tokens":1}}}`)
	waitForResponsesWebSocketWrites(t, client, 2)
	if err := <-runErr; err == nil {
		t.Fatal("Run() unexpectedly returned nil")
	}
	if result.TerminalEvent != "response.cancelled" || result.Metrics.InputTokens != 2 || result.Metrics.OutputTokens != 1 {
		t.Fatalf("turn result = %+v", result)
	}
}

func TestResponsesWebSocketSessionForwardsControlFramesBetweenTurns(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: 20 * time.Millisecond,
	})
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1}}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	client.send(`{"type":"session.update","session":{"model":"gpt-5.6-sol"}}`)
	waitForResponsesWebSocketWrites(t, upstream, 2)
	if got := string(upstream.written()[1]); got != `{"type":"session.update","session":{"model":"gpt-5.6-sol"}}` {
		t.Fatalf("forwarded control frame = %s", got)
	}
	if err := <-runErr; err == nil {
		t.Fatal("Run() unexpectedly returned nil")
	}
}

func TestResponsesWebSocketSessionInheritsSessionUpdateModelFromActiveTurn(t *testing.T) {
	testResponsesWebSocketSessionInheritsSessionUpdateModel(t, true)
}

func TestResponsesWebSocketSessionInheritsSessionUpdateModelBetweenTurns(t *testing.T) {
	testResponsesWebSocketSessionInheritsSessionUpdateModel(t, false)
}

func testResponsesWebSocketSessionInheritsSessionUpdateModel(t *testing.T, updateDuringTurn bool) {
	t.Helper()
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: 20 * time.Millisecond,
	})
	var beforeTurnRequests []ResponsesWebSocketTurnRequest
	var results []ResponsesWebSocketTurnResult
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-old","service_tier":"priority"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				beforeTurnRequests = append(beforeTurnRequests, request)
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, _ int, result ResponsesWebSocketTurnResult, turnErr error) {
				if turnErr != nil {
					t.Errorf("turn error: %v", turnErr)
				}
				results = append(results, result)
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	if updateDuringTurn {
		client.send(`{"type":"session.update","session":{"model":"gpt-new"}}`)
		waitForResponsesWebSocketWrites(t, upstream, 2)
	}
	upstream.send(`{"type":"response.completed","response":{"id":"resp_1"}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	if !updateDuringTurn {
		client.send(`{"type":"session.update","session":{"model":"gpt-new"}}`)
		waitForResponsesWebSocketWrites(t, upstream, 2)
	}
	client.send(`{"type":"response.create","previous_response_id":"resp_1","service_tier":"priority"}`)
	wantUpstreamWrites := 3
	waitForResponsesWebSocketWrites(t, upstream, wantUpstreamWrites)
	secondRequest := upstream.written()[wantUpstreamWrites-1]
	var upstreamPayload map[string]any
	if err := json.Unmarshal(secondRequest, &upstreamPayload); err != nil {
		t.Fatal(err)
	}
	if upstreamPayload["model"] != "gpt-new" {
		t.Fatalf("second upstream request = %s, want inherited model gpt-new", secondRequest)
	}
	upstream.send(`{"type":"response.completed","response":{"id":"resp_2"}}`)
	waitForResponsesWebSocketWrites(t, client, 2)
	err := <-runErr
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusNormalClosure {
		t.Fatalf("Run() error = %v", err)
	}
	if len(beforeTurnRequests) != 2 || beforeTurnRequests[1].Billing.Model != "gpt-new" {
		t.Fatalf("BeforeTurn requests = %+v", beforeTurnRequests)
	}
	var beforeTurnPayload map[string]any
	if err := json.Unmarshal(beforeTurnRequests[1].Frame, &beforeTurnPayload); err != nil {
		t.Fatal(err)
	}
	if beforeTurnPayload["model"] != "gpt-new" {
		t.Fatalf("second BeforeTurn frame = %s", beforeTurnRequests[1].Frame)
	}
	if len(results) != 2 || results[1].Billing.Model != "gpt-new" {
		t.Fatalf("turn results = %+v", results)
	}
}

func TestResponsesWebSocketSessionExplicitTurnModelDoesNotReplaceSessionModel(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: 20 * time.Millisecond,
	})
	var beforeTurnModels []string
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-session"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				beforeTurnModels = append(beforeTurnModels, request.Billing.Model)
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_1"}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	client.send(`{"type":"response.create","model":"gpt-one-turn","previous_response_id":"resp_1"}`)
	waitForResponsesWebSocketWrites(t, upstream, 2)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_2"}}`)
	waitForResponsesWebSocketWrites(t, client, 2)
	client.send(`{"type":"response.create","previous_response_id":"resp_2"}`)
	waitForResponsesWebSocketWrites(t, upstream, 3)
	var thirdPayload map[string]any
	if err := json.Unmarshal(upstream.written()[2], &thirdPayload); err != nil {
		t.Fatal(err)
	}
	if thirdPayload["model"] != "gpt-session" {
		t.Fatalf("third upstream request = %s, want session model gpt-session", upstream.written()[2])
	}
	upstream.send(`{"type":"response.completed","response":{"id":"resp_3"}}`)
	waitForResponsesWebSocketWrites(t, client, 3)
	err := <-runErr
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusNormalClosure {
		t.Fatalf("Run() error = %v", err)
	}
	if len(beforeTurnModels) != 3 || beforeTurnModels[0] != "gpt-session" || beforeTurnModels[1] != "gpt-one-turn" || beforeTurnModels[2] != "gpt-session" {
		t.Fatalf("BeforeTurn models = %v", beforeTurnModels)
	}
}

func TestResponsesWebSocketSessionSerializesTerminalCommitAndNextTurn(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	writeStarted, releaseWrite := client.blockWrites()
	defer releaseWrite()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: 20 * time.Millisecond,
	})
	afterTurnStarted := make(chan struct{}, 1)
	releaseAfterTurn := make(chan struct{})
	var releaseAfterTurnOnce sync.Once
	defer func() { releaseAfterTurnOnce.Do(func() { close(releaseAfterTurn) }) }()
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, turn int, _ ResponsesWebSocketTurnResult, _ error) {
				if turn == 1 {
					afterTurnStarted <- struct{}{}
					<-releaseAfterTurn
				}
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_1"}}`)
	select {
	case <-writeStarted:
	case <-time.After(time.Second):
		t.Fatal("terminal client write did not start")
	}
	client.send(`{"type":"response.create","previous_response_id":"resp_1"}`)
	if len(upstream.written()) != 1 {
		t.Fatal("next response.create reached upstream during terminal write")
	}
	releaseWrite()
	select {
	case <-afterTurnStarted:
	case <-time.After(time.Second):
		t.Fatal("first AfterTurn did not start after terminal delivery")
	}
	if len(upstream.written()) != 1 {
		t.Fatal("next response.create reached upstream before AfterTurn committed")
	}
	releaseAfterTurnOnce.Do(func() { close(releaseAfterTurn) })
	waitForResponsesWebSocketWrites(t, upstream, 2)
	if got := string(upstream.written()[1]); !strings.Contains(got, `"previous_response_id":"resp_1"`) {
		t.Fatalf("second upstream request = %s", got)
	}
	upstream.send(`{"type":"response.completed","response":{"id":"resp_2"}}`)
	waitForResponsesWebSocketWrites(t, client, 2)
	err := <-runErr
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusNormalClosure {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestResponsesWebSocketTurnLifecycleFailedTerminalWriteKeepsTurnActive(t *testing.T) {
	lifecycle := newResponsesWebSocketTurnLifecycle(true)
	lifecycle.beginTerminalWrite()

	admitted := make(chan bool, 1)
	go func() {
		admitted <- lifecycle.beginResponseCreate()
	}()
	select {
	case <-admitted:
		t.Fatal("next response.create was decided before terminal write completed")
	case <-time.After(20 * time.Millisecond):
	}

	lifecycle.finishTerminalWrite(false)
	select {
	case ok := <-admitted:
		if ok {
			t.Fatal("failed terminal write admitted a next response.create")
		}
	case <-time.After(time.Second):
		t.Fatal("next response.create remained blocked after terminal write failed")
	}
}

func TestResponsesWebSocketRelayPassesThroughUnobservableUpstreamFrames(t *testing.T) {
	tests := []struct {
		name        string
		messageType websocket.MessageType
		frame       []byte
	}{
		{name: "binary", messageType: websocket.MessageBinary, frame: []byte{0x00, 0x01, 0x02, 0xff}},
		{name: "binary JSON is not observed", messageType: websocket.MessageBinary, frame: []byte(`{"type":"response.completed","response":{"id":"resp_binary","usage":{"input_tokens":99}}}`)},
		{name: "invalid text", messageType: websocket.MessageText, frame: []byte(`{"type":"response.output_text.delta"`)},
		{name: "missing type", messageType: websocket.MessageText, frame: []byte(`{"response_id":"resp_missing","delta":"kept"}`)},
		{name: "unknown event", messageType: websocket.MessageText, frame: []byte(`{"type":"response.future_event","payload":{"kept":true}}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
				ReadTimeout: time.Second, WriteTimeout: time.Second,
			})
			client := newResponsesWebSocketTestConn()
			clientReads := make(chan responsesWebSocketRead)
			upstreamReads := make(chan responsesWebSocketRead, 2)
			upstreamReads <- responsesWebSocketRead{messageType: test.messageType, frame: test.frame}
			upstreamReads <- responsesWebSocketRead{
				messageType: websocket.MessageText,
				frame:       []byte(`{"type":"response.completed","response":{"id":"resp_terminal","usage":{"input_tokens":5}}}`),
			}

			result, err := session.relayTurn(
				context.Background(), client, newResponsesWebSocketTestConn(), clientReads, upstreamReads,
				ResponsesWebSocketTurnResult{StartedAt: time.Now()}, newResponsesWebSocketTurnLifecycle(true), nil, false, nil,
			)
			if err != nil {
				t.Fatalf("relayTurn() error = %v", err)
			}
			if result.RequestID != "resp_terminal" || result.TerminalEvent != "response.completed" || result.Metrics.InputTokens != 5 {
				t.Fatalf("relay result = %+v", result)
			}
			writes := client.written()
			writeTypes := client.writtenTypes()
			if len(writes) != 2 || len(writeTypes) != 2 {
				t.Fatalf("downstream writes/types = %d/%d, want 2/2", len(writes), len(writeTypes))
			}
			if writeTypes[0] != test.messageType || string(writes[0]) != string(test.frame) {
				t.Fatalf("first downstream frame = type %d payload %q", writeTypes[0], writes[0])
			}
			if writeTypes[1] != websocket.MessageText {
				t.Fatalf("terminal downstream type = %d, want text", writeTypes[1])
			}
		})
	}
}

func TestResponsesWebSocketSessionDrainsTerminalUsageAfterClientDisconnect(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	client.failWrites(errors.New("client disconnected"))
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: time.Second,
	})
	var result ResponsesWebSocketTurnResult
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, _ int, got ResponsesWebSocketTurnResult, _ error) { result = got },
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.output_text.delta","response_id":"resp_drain","delta":"one"}`)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_drain","model":"gpt-5.6-sol","usage":{"input_tokens":11,"output_tokens":6}}}`)
	if err := <-runErr; err == nil || err.Error() != "client disconnected" {
		t.Fatalf("Run() error = %v", err)
	}
	if result.TerminalEvent != "response.completed" || !result.Metrics.ClientDisconnected ||
		result.Metrics.InputTokens != 11 || result.Metrics.OutputTokens != 6 ||
		result.Metrics.ErrorStatusCode != 499 || result.Metrics.ErrorCode != "client_disconnected" {
		t.Fatalf("turn result = %+v", result)
	}
}

func TestResponsesWebSocketSessionStopsDrainAtConfiguredTimeout(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	client.failWrites(errors.New("client disconnected"))
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: time.Second,
		UpstreamDrainTimeout: 25 * time.Millisecond,
	})
	var result ResponsesWebSocketTurnResult
	runErr := make(chan error, 1)
	startedAt := time.Now()
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, _ int, got ResponsesWebSocketTurnResult, _ error) { result = got },
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.output_text.delta","response_id":"resp_drain_timeout","delta":"one"}`)
	if err := <-runErr; err == nil || err.Error() != "client disconnected" {
		t.Fatalf("Run() error = %v", err)
	}
	elapsed := time.Since(startedAt)
	if elapsed >= 500*time.Millisecond {
		t.Fatalf("client drain took %v, want configured short timeout", elapsed)
	}
	if !result.Metrics.ClientDisconnected || result.Metrics.ErrorStatusCode != 499 || result.Metrics.ErrorCode != "client_disconnected" {
		t.Fatalf("turn metrics = %+v", result.Metrics)
	}
}

func TestResponsesWebSocketSessionUsesDefaultDrainTimeout(t *testing.T) {
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{})
	if session.upstreamDrainTimeout != 1200*time.Millisecond {
		t.Fatalf("upstream drain timeout = %v", session.upstreamDrainTimeout)
	}
}

func TestResponsesWebSocketSessionRejectsChangedBindingOnLaterTurn(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: time.Second,
	})
	var afterCalls int
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				account := "account"
				if request.Turn == 2 {
					account = "changed-account"
				}
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: account, APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, _ int, _ ResponsesWebSocketTurnResult, _ error) { afterCalls++ },
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1}}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	client.send(`{"type":"response.create","previous_response_id":"resp_1"}`)
	err := <-runErr
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusTryAgainLater {
		t.Fatalf("Run() error = %v", err)
	}
	if afterCalls != 2 {
		t.Fatalf("AfterTurn calls = %d, want 2", afterCalls)
	}
	if len(upstream.written()) != 1 {
		t.Fatalf("changed binding reached upstream: %d writes", len(upstream.written()))
	}
}

func TestResponsesWebSocketSessionPreservesDialFailureMetadata(t *testing.T) {
	dialFailure := errors.New("upgrade rejected")
	dialer := &responsesWebSocketTestDialer{
		status: http.StatusTooManyRequests, responseHeaders: http.Header{"Retry-After": []string{"5"}}, err: dialFailure,
	}
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{Dialer: dialer})
	err := session.Run(context.Background(), newResponsesWebSocketTestConn(), []byte(`{"model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
		BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
			return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
				AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
			}}, nil
		},
	})
	var closeErr *ResponsesWebSocketCloseError
	var dialErr *ResponsesWebSocketDialError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusTryAgainLater || !errors.As(err, &dialErr) {
		t.Fatalf("Run() error = %v", err)
	}
	if dialErr.StatusCode != http.StatusTooManyRequests || dialErr.ResponseHeaders.Get("Retry-After") != "5" || !errors.Is(err, dialFailure) {
		t.Fatalf("dial error = %+v", dialErr)
	}
}

func TestResponsesWebSocketSessionForwardsErrorAndInheritsItAtTerminal(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: time.Second,
	})
	var result ResponsesWebSocketTurnResult
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, _ int, got ResponsesWebSocketTurnResult, _ error) { result = got },
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"slow down"}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	if got := string(client.written()[0]); !strings.Contains(got, `"type":"error"`) {
		t.Fatalf("first downstream frame = %s", got)
	}
	upstream.send(`{"type":"response.failed","response":{"id":"resp_failed","usage":{"input_tokens":3,"output_tokens":1}}}`)
	err := <-runErr
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusNormalClosure ||
		closeErr.Reason() != "websocket idle timeout" {
		t.Fatalf("Run() error = %v", err)
	}
	if len(client.written()) != 2 {
		t.Fatalf("downstream writes = %d, want error then terminal", len(client.written()))
	}
	if result.TerminalEvent != "response.failed" || result.RequestID != "resp_failed" ||
		result.Metrics.InputTokens != 3 || result.Metrics.OutputTokens != 1 ||
		result.Metrics.ErrorCode != "rate_limit_exceeded" || result.Metrics.ErrorMessage != "slow down" ||
		result.Metrics.ErrorStatusCode != http.StatusTooManyRequests {
		t.Fatalf("turn result = %+v", result)
	}
}

func TestResponsesWebSocketSessionTerminalErrorOverridesPendingError(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: 10 * time.Millisecond,
	})
	var result ResponsesWebSocketTurnResult
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, _ int, got ResponsesWebSocketTurnResult, _ error) { result = got },
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"pending"}}`)
	upstream.send(`{"type":"response.failed","response":{"id":"resp_failed","error":{"type":"invalid_request_error","code":"invalid_request","message":"terminal"}}}`)
	if err := <-runErr; err == nil {
		t.Fatal("Run() unexpectedly returned nil after inter-turn idle timeout")
	}
	if result.Metrics.ErrorCode != "invalid_request" || result.Metrics.ErrorMessage != "terminal" || result.Metrics.ErrorStatusCode != http.StatusBadRequest {
		t.Fatalf("turn metrics = %+v", result.Metrics)
	}
}

func TestResponsesWebSocketSessionPreservesPendingErrorAfterUpstreamClose(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, InterTurnIdleTimeout: time.Second,
	})
	var result ResponsesWebSocketTurnResult
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, _ int, got ResponsesWebSocketTurnResult, _ error) { result = got },
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"slow down"}}`)
	upstream.sendError(errors.New("upstream EOF"))
	if err := <-runErr; err == nil {
		t.Fatal("Run() unexpectedly returned nil")
	}
	if result.Metrics.ErrorCode != "rate_limit_exceeded" || result.Metrics.ErrorMessage != "slow down" || result.Metrics.ErrorStatusCode != http.StatusTooManyRequests {
		t.Fatalf("turn metrics = %+v", result.Metrics)
	}
}

func TestResponsesWebSocketSessionPreservesPendingErrorAfterReadTimeout(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: &responsesWebSocketTestDialer{conn: upstream}, DialTimeout: time.Second,
		ReadTimeout: 25 * time.Millisecond, WriteTimeout: time.Second, InterTurnIdleTimeout: time.Second,
	})
	var result ResponsesWebSocketTurnResult
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return ResponsesWebSocketTurnConfig{Frame: request.Frame, Dial: &ResponsesWebSocketDialConfig{
					AccessToken: "token", ChatGPTAccountID: "account", APIKeyID: "key",
				}}, nil
			},
			AfterTurn: func(_ context.Context, _ int, got ResponsesWebSocketTurnResult, _ error) { result = got },
		})
	}()
	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"slow down"}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	err := <-runErr
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.Reason() != "upstream Responses WebSocket read timeout" {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Metrics.ErrorCode != "rate_limit_exceeded" || result.Metrics.ErrorMessage != "slow down" || result.Metrics.ErrorStatusCode != http.StatusTooManyRequests {
		t.Fatalf("turn metrics = %+v", result.Metrics)
	}
}

func TestResponsesWebSocketSessionBeforeTurnFailureDoesNotCallAfterTurn(t *testing.T) {
	hookErr := NewResponsesWebSocketCloseError(websocket.StatusTryAgainLater, "busy", errors.New("no slot"))
	var afterCalls int
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{})
	err := session.Run(context.Background(), newResponsesWebSocketTestConn(), []byte(`{"model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
		BeforeTurn: func(context.Context, ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
			return ResponsesWebSocketTurnConfig{}, hookErr
		},
		AfterTurn: func(context.Context, int, ResponsesWebSocketTurnResult, error) { afterCalls++ },
	})
	if !errors.Is(err, hookErr) {
		t.Fatalf("Run() error = %v", err)
	}
	if afterCalls != 0 {
		t.Fatalf("AfterTurn calls = %d, want zero", afterCalls)
	}
}

func TestResponsesWebSocketCloseReasonIsUTF8SafeAndBounded(t *testing.T) {
	full := strings.Repeat("策略拒绝", 80)
	err := NewResponsesWebSocketCloseError(websocket.StatusPolicyViolation, full, nil)
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("error = %v", err)
	}
	if len(closeErr.Reason()) > maxWebSocketCloseReason || !strings.HasSuffix(closeErr.Reason(), "...") || !utf8.ValidString(closeErr.Reason()) {
		t.Fatalf("close reason bytes=%d valid=%v reason=%q", len(closeErr.Reason()), utf8.ValidString(closeErr.Reason()), closeErr.Reason())
	}
}

func TestResponsesWebSocketSessionInvalidHookFrameCallsAfterTurn(t *testing.T) {
	var afterCalls int
	var afterErr error
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{})
	err := session.Run(context.Background(), newResponsesWebSocketTestConn(), []byte(`{"model":"gpt-5.6-sol"}`), ResponsesWebSocketHooks{
		BeforeTurn: func(context.Context, ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
			return ResponsesWebSocketTurnConfig{Frame: []byte(`{"type":"response.append"}`)}, nil
		},
		AfterTurn: func(_ context.Context, _ int, _ ResponsesWebSocketTurnResult, err error) {
			afterCalls++
			afterErr = err
		},
	})
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusPolicyViolation {
		t.Fatalf("Run() error = %v", err)
	}
	if afterCalls != 1 || afterErr == nil {
		t.Fatalf("AfterTurn calls/error = %d / %v", afterCalls, afterErr)
	}
}

func TestParseResponsesWebSocketFrameRejectsInvalidProtocol(t *testing.T) {
	for _, test := range []struct {
		name  string
		frame string
	}{
		{name: "invalid json", frame: `{"type":"response.create"`},
		{name: "append", frame: `{"type":"response.append","model":"gpt-5.6-sol"}`},
		{name: "missing model", frame: `{"type":"response.create"}`},
		{name: "message previous id", frame: `{"type":"response.create","model":"gpt-5.6-sol","previous_response_id":"message_1"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := parseResponsesWebSocketFrame([]byte(test.frame), 1, "")
			var closeErr *ResponsesWebSocketCloseError
			if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusPolicyViolation {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestParseResponsesWebSocketTerminalEvent(t *testing.T) {
	for _, eventType := range []string{"response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled"} {
		frame := []byte(`{"type":"` + eventType + `","response":{"id":"resp","usage":{"input_tokens":1}}}`)
		gotType, responseID, terminal, err := parseResponsesWebSocketEvent(frame)
		if err != nil || gotType != eventType || responseID != "resp" || terminal == nil || terminal.Usage.InputTokens != 1 {
			t.Fatalf("event %q = %q, %q, %+v, %v", eventType, gotType, responseID, terminal, err)
		}
	}
}

func TestParseResponsesWebSocketEventResponseIDPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		frame string
		want  string
	}{
		{
			name:  "nested response id wins",
			frame: `{"type":"response.completed","id":"resp_top","response_id":"resp_field","response":{"id":"resp_nested"}}`,
			want:  "resp_nested",
		},
		{
			name:  "response id wins without nested id",
			frame: `{"type":"response.output_text.delta","id":"evt_top","response_id":"resp_field","response":{}}`,
			want:  "resp_field",
		},
		{
			name:  "terminal may use top level id",
			frame: `{"type":"response.done","id":"resp_top","response":{}}`,
			want:  "resp_top",
		},
		{
			name:  "non terminal ignores top level event id",
			frame: `{"type":"response.output_text.delta","id":"evt_top","response":{}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got, _, err := parseResponsesWebSocketEvent([]byte(test.frame))
			if err != nil || got != test.want {
				t.Fatalf("response id = %q, want %q (err=%v)", got, test.want, err)
			}
		})
	}
}

func TestResponsesWebSocketTerminalOutcomeMetrics(t *testing.T) {
	for _, eventType := range []string{"response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled"} {
		t.Run(eventType, func(t *testing.T) {
			session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
				ReadTimeout: time.Second, WriteTimeout: time.Second,
			})
			clientReads := make(chan responsesWebSocketRead)
			upstreamReads := make(chan responsesWebSocketRead, 1)
			upstreamReads <- responsesWebSocketRead{
				messageType: websocket.MessageText,
				frame:       []byte(`{"type":"` + eventType + `","response":{"id":"resp_terminal","usage":{"input_tokens":5}}}`),
			}
			turnLifecycle := newResponsesWebSocketTurnLifecycle(true)
			result, err := session.relayTurn(
				context.Background(), newResponsesWebSocketTestConn(), newResponsesWebSocketTestConn(), clientReads, upstreamReads,
				ResponsesWebSocketTurnResult{StartedAt: time.Now()}, turnLifecycle, nil, false, nil,
			)
			if result.RequestID != "resp_terminal" || result.Metrics.InputTokens != 5 {
				t.Fatalf("relay result = %+v, err=%v", result, err)
			}
			metrics := result.Metrics
			if isSuccessfulResponsesWebSocketTerminalEvent(eventType) {
				if err != nil {
					t.Fatalf("successful terminal error = %v", err)
				}
				if metrics.ErrorStatusCode != 0 || metrics.ErrorCode != "" {
					t.Fatalf("successful terminal metrics = %+v", metrics)
				}
				return
			}
			var terminalErr *ResponsesWebSocketTerminalError
			if !errors.As(err, &terminalErr) || terminalErr.Event != normalizeResponsesWebSocketTerminalEvent(eventType) {
				t.Fatalf("failed terminal error = %v", err)
			}
			wantEvent := normalizeResponsesWebSocketTerminalEvent(eventType)
			if metrics.ErrorStatusCode != http.StatusBadGateway || metrics.ErrorCode != strings.ReplaceAll(wantEvent, ".", "_") {
				t.Fatalf("failed terminal metrics = %+v", metrics)
			}
		})
	}
}
