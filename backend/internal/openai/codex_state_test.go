package openai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

type stateControllerStub struct {
	prepares, rejects, confirms int
	value                       string
}

func (s *stateControllerStub) PrepareState(context.Context, string, string, string) (string, domain.CodexStateReceipt) {
	s.prepares++
	return s.value, domain.CodexStateReceipt{AccountID: "a", Model: "m", Version: "v"}
}
func (s *stateControllerStub) ConfirmState(context.Context, string, string, string) { s.confirms++ }
func (s *stateControllerStub) RejectState(context.Context, domain.CodexStateReceipt, string) {
	s.rejects++
}

type stateRoundTrip func(*http.Request) (*http.Response, error)

func (f stateRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestStateProbeRequiresExactCompletedModel(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"success", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"m\"}}\n\n", 200, true},
		{"wrong model", "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"other\"}}\n\n", 200, false},
		{"incomplete", "data: {\"type\":\"response.created\",\"response\":{\"model\":\"m\"}}\n\n", 200, false},
		{"failed status", "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"m\",\"status\":\"failed\"}}\n\n", 200, false},
		{"429", "", 429, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGateway(&http.Client{Transport: stateRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("X-Codex-Turn-State") != "candidate" {
					t.Fatal("probe identity missing")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"X-Codex-Turn-State": []string{"returned"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})})
			value, err := g.ProbeState(context.Background(), application.CodexStateProbe{AccessToken: "token", Model: "m", State: "candidate"})
			if (err == nil) != tc.ok {
				t.Fatalf("error=%v", err)
			}
			if tc.ok && value != "returned" {
				t.Fatal(value)
			}
		})
	}
}
func TestStateForwardingBoundariesAndObservation(t *testing.T) {
	stub := &stateControllerStub{value: "verified"}
	g := NewGateway(&http.Client{})
	g.SetStateController(stub)
	req, _ := http.NewRequest("POST", "https://example.test", nil)
	req.Header.Set("X-Codex-Turn-State", "client")
	disabled := []CodexFingerprintContext{{AccountID: "a"}}
	g.prepareState(context.Background(), req, "m", false, disabled)
	if stub.prepares != 0 || req.Header.Get("X-Codex-Turn-State") != "client" {
		t.Fatal("disabled path changed")
	}
	enabled := []CodexFingerprintContext{{AccountID: "a", StateEnabled: true}}
	g.prepareState(context.Background(), req, "m", true, enabled)
	if stub.prepares != 0 {
		t.Fatal("compact/images must not enroll")
	}
	receipt := g.prepareState(context.Background(), req, "m", false, enabled)
	if req.Header.Get("X-Codex-Turn-State") != "verified" {
		t.Fatal("verified ticket not injected")
	}
	body := "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"other\"}}\n\n"
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
	g.watchState(resp, "m", receipt, false, enabled)
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != body || stub.rejects != 1 {
		t.Fatal("observation altered body or failed to revoke")
	}
}
func TestStateCompletionChunkingAndOverflow(t *testing.T) {
	text := "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"m\"}}\r\n\r\n"
	var o = stateCompletion{expected: "m"}
	for _, c := range []byte(text) {
		o.feed([]byte{c})
	}
	o.finish()
	if !o.complete || !o.matches {
		t.Fatal("split SSE failed")
	}
	o = stateCompletion{expected: "m"}
	o.feed([]byte("data: " + strings.Repeat("x", (1<<20)+1) + "\n\n"))
	o.finish()
	if o.complete || len(o.line) > 1<<20 || len(o.event) > 1<<20 {
		t.Fatal("unbounded or invalid frame accepted")
	}
}

func TestOnlySuccessfulMatchingBusinessResponsesEnrollState(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		confirmed  int
	}{
		{"missing-model", `{"error":{"code":"model_not_found"}}`, 400, 0},
		{"forbidden", `{"error":{"code":"permission_error"}}`, 403, 0},
		{"interrupted", "data: {\"type\":\"response.created\"}\n\n", 200, 0},
		{"failed", "data: {\"type\":\"response.failed\",\"response\":{\"model\":\"m\"}}\n\n", 200, 0},
		{"wrong-model", "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"other\"}}\n\n", 200, 0},
		{"matched", "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"m\"}}\n\n", 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stateControllerStub{}
			g := NewGateway(&http.Client{})
			g.SetStateController(stub)
			resp := &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}
			g.watchState(resp, "m", domain.CodexStateReceipt{}, false, []CodexFingerprintContext{{AccountID: "a", StateEnabled: true, StateScope: "snapshot"}})
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || string(body) != tc.body || stub.confirms != tc.confirmed {
				t.Fatalf("confirms=%d err=%v", stub.confirms, err)
			}
		})
	}
}
