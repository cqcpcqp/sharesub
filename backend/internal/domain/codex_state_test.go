package domain

import (
	"strings"
	"testing"
	"time"
)

func TestValidCodexState(t *testing.T) {
	for _, plan := range []string{"pro", "team"} {
		size := CodexStateLength(plan)
		token := "gAAAAA" + strings.Repeat("a", size-6)
		if !ValidCodexState(token, size) {
			t.Fatal("valid token rejected")
		}
		for _, bad := range []string{token + "a", token[:len(token)-1], "x" + token[1:], token[:len(token)-1] + "\n", token[:len(token)-1] + " ", token[:len(token)-1] + "/"} {
			if ValidCodexState(bad, size) {
				t.Fatal("invalid token accepted")
			}
		}
	}
	if ValidCodexState("", CodexStateLength("plus")) {
		t.Fatal("unsupported plan accepted")
	}
}

func TestCodexStateTicketScopeAndExpiry(t *testing.T) {
	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	ticket := CodexStateTicket{AccountID: "a", Model: "m", Plan: "pro", Version: "v2", ConfigFingerprint: "c", IdentityFingerprint: "i", EgressFingerprint: "e", Ciphertext: []byte{1}, CapturedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	valid := func(v CodexStateTicket) bool { return v.Matches("a", "m", "pro", "c", "i", "e", now) }
	if !valid(ticket) {
		t.Fatal("matching ticket rejected")
	}
	mutations := []func(*CodexStateTicket){
		func(v *CodexStateTicket) { v.AccountID = "other" }, func(v *CodexStateTicket) { v.Model = "other" },
		func(v *CodexStateTicket) { v.Plan = "team" }, func(v *CodexStateTicket) { v.ConfigFingerprint = "other" },
		func(v *CodexStateTicket) { v.IdentityFingerprint = "other" }, func(v *CodexStateTicket) { v.EgressFingerprint = "other" },
		func(v *CodexStateTicket) { v.Version = "" }, func(v *CodexStateTicket) { v.Ciphertext = nil },
		func(v *CodexStateTicket) { v.ExpiresAt = now }, func(v *CodexStateTicket) { v.CapturedAt = now.Add(time.Second) },
	}
	for i, mutate := range mutations {
		copy := ticket
		mutate(&copy)
		if valid(copy) {
			t.Fatalf("mutation %d accepted", i)
		}
	}
	if ticket.UsedBy(CodexStateReceipt{"a", "m", "v1"}) {
		t.Fatal("stale receipt matched replacement")
	}
	if !ticket.UsedBy(CodexStateReceipt{"a", "m", "v2"}) {
		t.Fatal("current receipt rejected")
	}
	if ticket.UsedBy(CodexStateReceipt{"other", "m", "v2"}) {
		t.Fatal("cross-account receipt accepted")
	}
}

func TestRejectCodexState(t *testing.T) {
	signal := "gAAAAA" + strings.Repeat("a", 306)
	cases := []struct {
		state, actual      string
		complete, rejected bool
	}{
		{signal, "", false, true}, {strings.Repeat("x", 312), "m", true, false},
		{"", "other", true, true}, {"", "other", false, false}, {"", "m", true, false}, {"", "", true, false},
	}
	for _, tc := range cases {
		if got := RejectCodexState(tc.state, "m", tc.actual, tc.complete); got != tc.rejected {
			t.Fatalf("got %v, want %v", got, tc.rejected)
		}
	}
}
