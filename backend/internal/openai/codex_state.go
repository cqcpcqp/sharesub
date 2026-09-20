package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (g *Gateway) SetStateController(c application.CodexStateController) { g.state = c }

func (g *Gateway) prepareState(ctx context.Context, req *http.Request, model string, unsupported bool, options []CodexFingerprintContext) domain.CodexStateReceipt {
	if g.state == nil || unsupported || len(options) == 0 || !options[0].StateEnabled {
		return domain.CodexStateReceipt{}
	}
	value, receipt := g.state.PrepareState(ctx, options[0].AccountID, model, options[0].StateScope)
	// No usable ticket means ordinary forwarding, including the client's header.
	if value != "" {
		req.Header.Set("X-Codex-Turn-State", value)
	}
	return receipt
}

func (g *Gateway) watchState(resp *http.Response, model string, r domain.CodexStateReceipt, unsupported bool, options []CodexFingerprintContext) {
	if g.state == nil || unsupported || len(options) == 0 || !options[0].StateEnabled {
		return
	}
	reject := func(reason string) {
		if r.Version != "" {
			g.state.RejectState(context.Background(), r, reason)
		}
	}
	if domain.ValidCodexState(strings.TrimSpace(resp.Header.Get("X-Codex-Turn-State")), 312) {
		reject("state_312")
	}
	if resp.StatusCode == http.StatusOK {
		resp.Body = &stateObservedBody{ReadCloser: resp.Body, observer: stateCompletion{expected: model}, reject: reject, confirm: func() { g.state.ConfirmState(context.Background(), options[0].AccountID, model, options[0].StateScope) }}
	}
}

type stateObservedBody struct {
	io.ReadCloser
	observer  stateCompletion
	reject    func(string)
	rejected  bool
	confirmed bool
	confirm   func()
}

func (b *stateObservedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.observer.feed(p[:n])
	if err == io.EOF {
		b.observer.finish()
	}
	if b.observer.complete && !b.observer.matches && !b.rejected {
		b.rejected = true
		b.reject("model_mismatch")
	}
	if b.observer.complete && b.observer.matches && !b.confirmed {
		b.confirmed = true
		b.confirm()
	}
	return n, err
}

// Only bounded terminal events are inspected. Business content is neither
// persisted nor replayed; oversized events cannot validate a ticket.
type stateCompletion struct {
	expected      string
	line          []byte
	event         []byte
	overflow      bool
	eventOverflow bool
	complete      bool
	matches       bool
}

func (o *stateCompletion) feed(p []byte) {
	for _, c := range p {
		if c == '\n' {
			o.consumeLine()
			continue
		}
		if !o.overflow {
			if len(o.line) >= 1<<20 {
				o.line = nil
				o.overflow = true
			} else {
				o.line = append(o.line, c)
			}
		}
	}
}
func (o *stateCompletion) consumeLine() {
	line := bytes.TrimSuffix(o.line, []byte{'\r'})
	if o.overflow {
		o.eventOverflow = true
	} else if len(line) == 0 {
		o.inspect()
		o.event = nil
		o.eventOverflow = false
	} else if bytes.HasPrefix(line, []byte("data:")) && !o.eventOverflow {
		data := bytes.TrimSpace(line[5:])
		if len(o.event)+len(data)+1 > 1<<20 {
			o.event = nil
			o.eventOverflow = true
		} else {
			o.event = append(o.event, data...)
			o.event = append(o.event, '\n')
		}
	}
	o.line = o.line[:0]
	o.overflow = false
}
func (o *stateCompletion) finish() {
	if len(o.line) > 0 || o.overflow {
		o.consumeLine()
	}
	o.inspect()
}
func (o *stateCompletion) inspect() {
	if o.eventOverflow {
		return
	}
	var v struct {
		Type     string          `json:"type"`
		Error    json.RawMessage `json:"error"`
		Response *struct {
			Status string          `json:"status"`
			Model  string          `json:"model"`
			Error  json.RawMessage `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(o.event, &v) != nil || v.Type != "response.completed" || v.Response == nil || v.Response.Model == "" {
		return
	}
	if (v.Response.Status != "completed" && v.Response.Status != "") || stateHasError(v.Error) || stateHasError(v.Response.Error) {
		return
	}
	match := v.Response.Model == o.expected
	if o.complete {
		o.matches = o.matches && match
	} else {
		o.matches = match
	}
	o.complete = true
}
func stateHasError(v json.RawMessage) bool {
	return len(v) > 0 && !bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}

func (g *Gateway) ProbeState(ctx context.Context, p application.CodexStateProbe) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	body, _ := json.Marshal(codexProbePayload{Model: p.Model, Instructions: "Reply with exactly: pong", Input: []codexProbeMessage{{Role: "user", Content: []codexProbeContent{{Type: "input_text", Text: "ping"}}}}, Stream: true, Store: false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexResponsesURL, bytes.NewReader(body))
	if err != nil {
		return "", stateProbeError("transport_failed")
	}
	req.Header.Set("Authorization", "Bearer "+p.AccessToken)
	req.Header.Set("Chatgpt-Account-Id", p.ChatGPTAccountID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "identity")
	applyCodexOAuthIdentity(req.Header, "")
	applyCodexRoutingHint(req.Header, p.Model, "")
	seed, err := randomSessionSeed()
	if err != nil {
		return "", stateProbeError("transport_failed")
	}
	req.Header.Set("Session_Id", seed)
	if p.State != "" {
		req.Header.Set("X-Codex-Turn-State", p.State)
	}
	client, err := g.clientForProxy(p.ProxyURL)
	if err != nil {
		return "", stateProbeError("transport_failed")
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", stateProbeError("transport_failed")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 401:
		return "", stateProbeError("unauthorized")
	case 403:
		return "", stateProbeError("forbidden")
	case 429:
		return "", stateProbeError("rate_limited")
	default:
		return "", stateProbeError("upstream_rejected")
	}
	observer := stateCompletion{expected: p.Model}
	reader := bufio.NewReader(io.LimitReader(resp.Body, (4<<20)+1))
	buf := make([]byte, 16<<10)
	total := 0
	for {
		n, e := reader.Read(buf)
		total += n
		if total > 4<<20 {
			return "", stateProbeError("incomplete_response")
		}
		observer.feed(buf[:n])
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", stateProbeError("incomplete_response")
		}
	}
	observer.finish()
	if !observer.complete {
		return "", stateProbeError("incomplete_response")
	}
	if !observer.matches {
		return "", stateProbeError("model_mismatch")
	}
	return strings.TrimSpace(resp.Header.Get("X-Codex-Turn-State")), nil
}
func stateProbeError(reason string) error { return &application.StateProbeError{Reason: reason} }
