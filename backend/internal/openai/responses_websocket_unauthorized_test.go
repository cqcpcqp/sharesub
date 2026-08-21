package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type responsesWebSocketUnauthorizedDialStep struct {
	conn            ResponsesWebSocketConn
	status          int
	responseHeaders http.Header
	err             error
}

type responsesWebSocketUnauthorizedDialCall struct {
	headers http.Header
	proxy   string
}

type responsesWebSocketUnauthorizedDialer struct {
	mu    sync.Mutex
	steps []responsesWebSocketUnauthorizedDialStep
	calls []responsesWebSocketUnauthorizedDialCall
}

func (d *responsesWebSocketUnauthorizedDialer) Dial(_ context.Context, _ string, headers http.Header, proxy string) (ResponsesWebSocketConn, int, http.Header, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, responsesWebSocketUnauthorizedDialCall{headers: headers.Clone(), proxy: proxy})
	index := len(d.calls) - 1
	if index >= len(d.steps) {
		return nil, http.StatusBadGateway, nil, errors.New("unexpected scripted Responses WebSocket dial")
	}
	step := d.steps[index]
	return step.conn, step.status, step.responseHeaders.Clone(), step.err
}

func (d *responsesWebSocketUnauthorizedDialer) dialCalls() []responsesWebSocketUnauthorizedDialCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	calls := make([]responsesWebSocketUnauthorizedDialCall, len(d.calls))
	for index := range d.calls {
		calls[index] = responsesWebSocketUnauthorizedDialCall{headers: d.calls[index].headers.Clone(), proxy: d.calls[index].proxy}
	}
	return calls
}

func newResponsesWebSocketUnauthorizedSession(dialer ResponsesWebSocketDialer) *ResponsesWebSocketSession {
	return NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: dialer, DialTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, InterTurnIdleTimeout: 40 * time.Millisecond,
	})
}

func responsesWebSocketUnauthorizedConfig(frame []byte, token, account string) ResponsesWebSocketTurnConfig {
	return ResponsesWebSocketTurnConfig{
		Frame: frame,
		Dial: &ResponsesWebSocketDialConfig{
			AccessToken: token, ChatGPTAccountID: account, APIKeyID: "key",
		},
	}
}

func responsesWebSocketUnauthorizedRequestCopy(request ResponsesWebSocketTurnRequest) ResponsesWebSocketTurnRequest {
	request.Frame = append([]byte(nil), request.Frame...)
	return request
}

func decodeResponsesWebSocketUnauthorizedFrame(t *testing.T, frame []byte) (string, []json.RawMessage) {
	t.Helper()
	var payload struct {
		PreviousResponseID string            `json:"previous_response_id"`
		Input              []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(frame, &payload); err != nil {
		t.Fatalf("decode Responses WebSocket frame %s: %v", frame, err)
	}
	return payload.PreviousResponseID, payload.Input
}

func TestResponsesWebSocketUnauthorizedRefreshPreservesUnsafeExternalConversation(t *testing.T) {
	upstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	defer client.CloseNow()
	dialer := &responsesWebSocketUnauthorizedDialer{steps: []responsesWebSocketUnauthorizedDialStep{
		{status: http.StatusUnauthorized, responseHeaders: http.Header{"X-Request-Id": []string{"unauthorized"}}, err: errors.New("unauthorized")},
		{conn: upstream, status: http.StatusSwitchingProtocols, responseHeaders: http.Header{"X-Request-Id": []string{"refreshed"}}},
	}}
	session := newResponsesWebSocketUnauthorizedSession(dialer)
	initialFrame := []byte(`{"type":"response.create","model":"gpt-5.6-sol","previous_response_id":"resp_external","input":[{"type":"item_reference","id":"msg_external"}]}`)
	var hookRequest ResponsesWebSocketTurnRequest
	var hookCalls int
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, initialFrame, ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return responsesWebSocketUnauthorizedConfig(request.Frame, "stale-token", "account-a"), nil
			},
			OnDialError: func(_ context.Context, request ResponsesWebSocketTurnRequest, _ ResponsesWebSocketTurnResult, dialErr *ResponsesWebSocketDialError) (ResponsesWebSocketTurnConfig, error) {
				hookCalls++
				hookRequest = responsesWebSocketUnauthorizedRequestCopy(request)
				if dialErr == nil || dialErr.StatusCode != http.StatusUnauthorized {
					return ResponsesWebSocketTurnConfig{}, errors.New("unexpected dial error")
				}
				return responsesWebSocketUnauthorizedConfig(request.Frame, "fresh-token", "account-a"), nil
			},
		})
	}()

	waitForResponsesWebSocketWrites(t, upstream, 1)
	upstream.send(`{"type":"response.completed","response":{"id":"resp_refreshed","output":[]}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	err := <-runErr
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusNormalClosure {
		t.Fatalf("Run() error = %v", err)
	}
	if hookCalls != 1 || hookRequest.PreviousResponseID != "resp_external" {
		t.Fatalf("hook calls=%d request=%+v", hookCalls, hookRequest)
	}
	previousResponseID, hookItems := decodeResponsesWebSocketUnauthorizedFrame(t, hookRequest.Frame)
	if previousResponseID != "resp_external" || len(hookItems) != 1 || !strings.Contains(string(hookItems[0]), `"type":"item_reference"`) {
		t.Fatalf("hook frame lost account-scoped state: %s", hookRequest.Frame)
	}
	previousResponseID, upstreamItems := decodeResponsesWebSocketUnauthorizedFrame(t, upstream.written()[0])
	if previousResponseID != "resp_external" || len(upstreamItems) != 1 || !strings.Contains(string(upstreamItems[0]), `"id":"msg_external"`) {
		t.Fatalf("refreshed frame lost account-scoped state: %s", upstream.written()[0])
	}
	calls := dialer.dialCalls()
	if len(calls) != 2 || calls[0].headers.Get("Authorization") != "Bearer stale-token" || calls[1].headers.Get("Authorization") != "Bearer fresh-token" ||
		calls[0].headers.Get("ChatGPT-Account-ID") != "account-a" || calls[1].headers.Get("ChatGPT-Account-ID") != "account-a" {
		t.Fatalf("dial calls = %#v", calls)
	}
}

func TestResponsesWebSocketUnsafeHandshakeRateLimitDoesNotCallDialHook(t *testing.T) {
	client := newResponsesWebSocketTestConn()
	defer client.CloseNow()
	dialer := &responsesWebSocketUnauthorizedDialer{steps: []responsesWebSocketUnauthorizedDialStep{{
		status: http.StatusTooManyRequests, responseHeaders: http.Header{"Retry-After": []string{"5"}}, err: errors.New("rate limited"),
	}}}
	session := newResponsesWebSocketUnauthorizedSession(dialer)
	initialFrame := []byte(`{"type":"response.create","model":"gpt-5.6-sol","previous_response_id":"resp_external","input":[{"type":"item_reference","id":"msg_external"}]}`)
	var hookCalls int
	err := session.Run(context.Background(), client, initialFrame, ResponsesWebSocketHooks{
		BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
			return responsesWebSocketUnauthorizedConfig(request.Frame, "token", "account-a"), nil
		},
		OnDialError: func(_ context.Context, request ResponsesWebSocketTurnRequest, _ ResponsesWebSocketTurnResult, _ *ResponsesWebSocketDialError) (ResponsesWebSocketTurnConfig, error) {
			hookCalls++
			return ResponsesWebSocketTurnConfig{}, errors.New("replay-unsafe 429 must not invoke the dial hook")
		},
	})
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusTryAgainLater {
		t.Fatalf("Run() error = %v", err)
	}
	if hookCalls != 0 || len(dialer.dialCalls()) != 1 {
		t.Fatalf("hook calls=%d dial calls=%d", hookCalls, len(dialer.dialCalls()))
	}
}

func TestResponsesWebSocketUnauthorizedRefreshUsesRebuiltFailoverRequest(t *testing.T) {
	accountA := newResponsesWebSocketTestConn()
	accountB := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	defer client.CloseNow()
	dialer := &responsesWebSocketUnauthorizedDialer{steps: []responsesWebSocketUnauthorizedDialStep{
		{conn: accountA, status: http.StatusSwitchingProtocols, responseHeaders: http.Header{"X-Request-Id": []string{"account-a"}}},
		{status: http.StatusUnauthorized, responseHeaders: http.Header{"X-Request-Id": []string{"account-b-unauthorized"}}, err: errors.New("unauthorized")},
		{conn: accountB, status: http.StatusSwitchingProtocols, responseHeaders: http.Header{"X-Request-Id": []string{"account-b-refreshed"}}},
	}}
	session := newResponsesWebSocketUnauthorizedSession(dialer)
	var upstreamRetryRequest ResponsesWebSocketTurnRequest
	var dialRetryRequest ResponsesWebSocketTurnRequest
	var upstreamHookCalls int
	var dialHookCalls int
	runErr := make(chan error, 1)
	go func() {
		runErr <- session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":"one"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return responsesWebSocketUnauthorizedConfig(request.Frame, "token-a", "account-a"), nil
			},
			OnUpstreamError: func(_ context.Context, request ResponsesWebSocketTurnRequest, _ ResponsesWebSocketTurnResult, _ *ResponsesWebSocketUpstreamEventError) (ResponsesWebSocketTurnConfig, error) {
				upstreamHookCalls++
				upstreamRetryRequest = responsesWebSocketUnauthorizedRequestCopy(request)
				return responsesWebSocketUnauthorizedConfig(request.Frame, "stale-token-b", "account-b"), nil
			},
			OnDialError: func(_ context.Context, request ResponsesWebSocketTurnRequest, _ ResponsesWebSocketTurnResult, dialErr *ResponsesWebSocketDialError) (ResponsesWebSocketTurnConfig, error) {
				dialHookCalls++
				dialRetryRequest = responsesWebSocketUnauthorizedRequestCopy(request)
				if dialErr == nil || dialErr.StatusCode != http.StatusUnauthorized {
					return ResponsesWebSocketTurnConfig{}, errors.New("unexpected dial error")
				}
				return responsesWebSocketUnauthorizedConfig(request.Frame, "fresh-token-b", "account-b"), nil
			},
		})
	}()

	waitForResponsesWebSocketWrites(t, accountA, 1)
	accountA.send(`{"type":"response.completed","response":{"id":"resp_1","output":[]}}`)
	waitForResponsesWebSocketWrites(t, client, 1)
	client.send(`{"type":"response.create","previous_response_id":"resp_1","input":"two"}`)
	waitForResponsesWebSocketWrites(t, accountA, 2)
	accountA.send(`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"rate limit exceeded"}}`)
	waitForResponsesWebSocketWrites(t, accountB, 1)
	accountB.send(`{"type":"response.completed","response":{"id":"resp_2","output":[]}}`)
	waitForResponsesWebSocketWrites(t, client, 2)
	err := <-runErr
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusNormalClosure {
		t.Fatalf("Run() error = %v", err)
	}
	if upstreamHookCalls != 1 || dialHookCalls != 1 {
		t.Fatalf("upstream hook calls=%d dial hook calls=%d", upstreamHookCalls, dialHookCalls)
	}
	if upstreamRetryRequest.PreviousResponseID != "" || dialRetryRequest.PreviousResponseID != "" || !bytes.Equal(upstreamRetryRequest.Frame, dialRetryRequest.Frame) {
		t.Fatalf("dial hook did not receive rebuilt request:\nupstream=%+v\ndial=%+v", upstreamRetryRequest, dialRetryRequest)
	}
	previousResponseID, rebuiltItems := decodeResponsesWebSocketUnauthorizedFrame(t, dialRetryRequest.Frame)
	if previousResponseID != "" || len(rebuiltItems) != 2 || !strings.Contains(string(rebuiltItems[0]), `"content":"one"`) || !strings.Contains(string(rebuiltItems[1]), `"content":"two"`) {
		t.Fatalf("rebuilt request = %s", dialRetryRequest.Frame)
	}
	previousResponseID, writtenItems := decodeResponsesWebSocketUnauthorizedFrame(t, accountB.written()[0])
	if previousResponseID != "" || len(writtenItems) != 2 {
		t.Fatalf("account B frame = %s", accountB.written()[0])
	}
	calls := dialer.dialCalls()
	if len(calls) != 3 || calls[0].headers.Get("Authorization") != "Bearer token-a" || calls[1].headers.Get("Authorization") != "Bearer stale-token-b" || calls[2].headers.Get("Authorization") != "Bearer fresh-token-b" ||
		calls[1].headers.Get("ChatGPT-Account-ID") != "account-b" || calls[2].headers.Get("ChatGPT-Account-ID") != "account-b" {
		t.Fatalf("dial calls = %#v", calls)
	}
}

func TestResponsesWebSocketUnauthorizedRefreshRejectsChangedAccountBindingBeforeRedial(t *testing.T) {
	unexpectedUpstream := newResponsesWebSocketTestConn()
	client := newResponsesWebSocketTestConn()
	defer client.CloseNow()
	dialer := &responsesWebSocketUnauthorizedDialer{steps: []responsesWebSocketUnauthorizedDialStep{
		{status: http.StatusUnauthorized, err: errors.New("unauthorized")},
		{conn: unexpectedUpstream, status: http.StatusSwitchingProtocols},
	}}
	session := newResponsesWebSocketUnauthorizedSession(dialer)
	var hookCalls int
	err := session.Run(context.Background(), client, []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":"hello"}`), ResponsesWebSocketHooks{
		BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
			return responsesWebSocketUnauthorizedConfig(request.Frame, "stale-token", "account-a"), nil
		},
		OnDialError: func(_ context.Context, request ResponsesWebSocketTurnRequest, _ ResponsesWebSocketTurnResult, _ *ResponsesWebSocketDialError) (ResponsesWebSocketTurnConfig, error) {
			hookCalls++
			return responsesWebSocketUnauthorizedConfig(request.Frame, "fresh-token", "account-b"), nil
		},
	})
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(err, &closeErr) || closeErr.StatusCode() != websocket.StatusInternalError ||
		!strings.Contains(strings.ToLower(closeErr.Reason()), "authentication retry") || !strings.Contains(strings.ToLower(closeErr.Reason()), "binding") {
		t.Fatalf("Run() error = %v", err)
	}
	if hookCalls != 1 || len(dialer.dialCalls()) != 1 || len(unexpectedUpstream.written()) != 0 {
		t.Fatalf("hook calls=%d dial calls=%d unexpected writes=%d", hookCalls, len(dialer.dialCalls()), len(unexpectedUpstream.written()))
	}
}
