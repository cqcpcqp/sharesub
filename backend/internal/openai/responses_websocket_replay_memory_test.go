package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func waitForResponsesWebSocketReplayBudget(t *testing.T, budget *responsesWebSocketReplayBudget, want int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := budget.usedBytes(); got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("replay budget used = %d, want %d", budget.usedBytes(), want)
}

func waitForResponsesWebSocketRun(t *testing.T, runErr <-chan error) error {
	t.Helper()
	select {
	case err := <-runErr:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Responses WebSocket Run to exit")
		return nil
	}
}

func TestResponsesWebSocketReplayBudgetIsSharedAndReusableAcrossRuns(t *testing.T) {
	initialFrame := []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":"one"}`)
	normalized, _, _, err := parseResponsesWebSocketFrame(initialFrame, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	currentInput, _, _, err := responsesWebSocketInputItems(normalized)
	if err != nil {
		t.Fatal(err)
	}
	outputItem := []byte(`{"id":"msg_budget","type":"message","role":"assistant","content":[{"type":"output_text","text":"` + strings.Repeat("x", 512) + `"}]}`)
	budgetLimit := responsesWebSocketRawMessagesBytes(currentInput) + int64(len(outputItem))
	if budgetLimit < int64(len(normalized)) {
		t.Fatalf("test replay budget %d is smaller than normalized request %d", budgetLimit, len(normalized))
	}

	firstUpstream := newResponsesWebSocketTestConn()
	secondUpstream := newResponsesWebSocketTestConn()
	thirdUpstream := newResponsesWebSocketTestConn()
	fourthUpstream := newResponsesWebSocketTestConn()
	dialer := &responsesWebSocketUnauthorizedDialer{steps: []responsesWebSocketUnauthorizedDialStep{
		{conn: firstUpstream, status: http.StatusSwitchingProtocols},
		{conn: secondUpstream, status: http.StatusSwitchingProtocols},
		{conn: thirdUpstream, status: http.StatusSwitchingProtocols},
		{conn: fourthUpstream, status: http.StatusSwitchingProtocols},
	}}
	session := NewResponsesWebSocketSession(ResponsesWebSocketOptions{
		Dialer: dialer, DialTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, InterTurnIdleTimeout: 5 * time.Second,
		ReplayMemoryLimitBytes: budgetLimit,
	})
	terminalFrame := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_budget","output":[%s]}}`, outputItem)

	firstClient := newResponsesWebSocketTestConn()
	defer firstClient.CloseNow()
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	firstRunErr := make(chan error, 1)
	go func() {
		firstRunErr <- session.Run(firstCtx, firstClient, initialFrame, ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return responsesWebSocketUnauthorizedConfig(request.Frame, "token-a", "account-a"), nil
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, firstUpstream, 1)
	firstUpstream.send(terminalFrame)
	waitForResponsesWebSocketWrites(t, firstClient, 1)
	waitForResponsesWebSocketReplayBudget(t, session.replayBudget, budgetLimit)

	secondClient := newResponsesWebSocketTestConn()
	defer secondClient.CloseNow()
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	secondRunErr := make(chan error, 1)
	var retryCalls int
	go func() {
		secondRunErr <- session.Run(secondCtx, secondClient, []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":"two"}`), ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return responsesWebSocketUnauthorizedConfig(request.Frame, "token-b", "account-b"), nil
			},
			OnUpstreamError: func(_ context.Context, _ ResponsesWebSocketTurnRequest, _ ResponsesWebSocketTurnResult, _ *ResponsesWebSocketUpstreamEventError) (ResponsesWebSocketTurnConfig, error) {
				retryCalls++
				return ResponsesWebSocketTurnConfig{}, errors.New("budget-exhausted replay must not switch accounts")
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, secondUpstream, 1)
	secondUpstream.send(`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"rate limit exceeded"}}`)
	secondUpstream.send(`{"type":"response.failed","response":{"id":"resp_budget_failed","output":[]}}`)
	waitForResponsesWebSocketWrites(t, secondClient, 2)
	cancelSecond()
	_ = waitForResponsesWebSocketRun(t, secondRunErr)
	if retryCalls != 0 || len(dialer.dialCalls()) != 2 {
		t.Fatalf("budget-exhausted run retried: hook calls=%d dial calls=%d", retryCalls, len(dialer.dialCalls()))
	}
	waitForResponsesWebSocketReplayBudget(t, session.replayBudget, budgetLimit)

	cancelFirst()
	_ = waitForResponsesWebSocketRun(t, firstRunErr)
	waitForResponsesWebSocketReplayBudget(t, session.replayBudget, 0)

	thirdClient := newResponsesWebSocketTestConn()
	defer thirdClient.CloseNow()
	thirdCtx, cancelThird := context.WithCancel(context.Background())
	defer cancelThird()
	thirdRunErr := make(chan error, 1)
	go func() {
		thirdRunErr <- session.Run(thirdCtx, thirdClient, initialFrame, ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return responsesWebSocketUnauthorizedConfig(request.Frame, "token-c", "account-c"), nil
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, thirdUpstream, 1)
	thirdUpstream.send(terminalFrame)
	waitForResponsesWebSocketWrites(t, thirdClient, 1)
	waitForResponsesWebSocketReplayBudget(t, session.replayBudget, budgetLimit)
	cancelThird()
	thirdErr := waitForResponsesWebSocketRun(t, thirdRunErr)
	var closeErr *ResponsesWebSocketCloseError
	if !errors.As(thirdErr, &closeErr) || closeErr.StatusCode() != websocket.StatusGoingAway {
		t.Fatalf("third Run() error = %v", thirdErr)
	}
	waitForResponsesWebSocketReplayBudget(t, session.replayBudget, 0)

	fourthClient := newResponsesWebSocketTestConn()
	defer fourthClient.CloseNow()
	fourthCtx, cancelFourth := context.WithCancel(context.Background())
	defer cancelFourth()
	fourthRunErr := make(chan error, 1)
	go func() {
		fourthRunErr <- session.Run(fourthCtx, fourthClient, initialFrame, ResponsesWebSocketHooks{
			BeforeTurn: func(_ context.Context, request ResponsesWebSocketTurnRequest) (ResponsesWebSocketTurnConfig, error) {
				return responsesWebSocketUnauthorizedConfig(request.Frame, "token-d", "account-d"), nil
			},
		})
	}()
	waitForResponsesWebSocketWrites(t, fourthUpstream, 1)
	fourthUpstream.send(fmt.Sprintf(`{"type":"response.output_item.done","item":%s}`, outputItem))
	waitForResponsesWebSocketWrites(t, fourthClient, 1)
	waitForResponsesWebSocketReplayBudget(t, session.replayBudget, budgetLimit)
	fourthClient.send(`{"type":"session.update","session":{"instructions":"unsafe replay state"}}`)
	waitForResponsesWebSocketWrites(t, fourthUpstream, 2)
	waitForResponsesWebSocketReplayBudget(t, session.replayBudget, 0)
	cancelFourth()
	fourthErr := waitForResponsesWebSocketRun(t, fourthRunErr)
	closeErr = nil
	if !errors.As(fourthErr, &closeErr) || closeErr.StatusCode() != websocket.StatusGoingAway {
		t.Fatalf("fourth Run() error = %v", fourthErr)
	}
	waitForResponsesWebSocketReplayBudget(t, session.replayBudget, 0)
}
