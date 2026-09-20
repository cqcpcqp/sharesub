package domain

import (
	"strings"
	"time"
)

// CodexStateLength describes the experimentally observed ticket formats. A
// matching length alone does not prove model quality or a successful response.
func CodexStateLength(plan string) int {
	switch plan {
	case "pro":
		return 292
	case "team":
		return 332
	default:
		return 0
	}
}

func ValidCodexState(value string, length int) bool {
	if length <= 0 || len(value) != length || !strings.HasPrefix(value, "gAAAAA") {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '=') {
			return false
		}
	}
	return true
}

// CodexStateTicket is internal only: neither ciphertext nor fingerprints belong
// in account API responses. Ciphertext uses the existing credential key.
type CodexStateTicket struct {
	AccountID           string
	Model               string
	Plan                string
	Version             string
	ConfigFingerprint   string
	IdentityFingerprint string
	EgressFingerprint   string
	Ciphertext          []byte
	CapturedAt          time.Time
	ExpiresAt           time.Time
}

// Matches binds a ticket to the exact account, model, configuration, identity
// and business egress. OAuth access-token refresh alone need not change identity.
func (t CodexStateTicket) Matches(accountID, model, plan, config, identity, egress string, now time.Time) bool {
	return t.AccountID == accountID && t.Model == model && t.Plan == plan &&
		t.ConfigFingerprint == config && t.IdentityFingerprint == identity && t.EgressFingerprint == egress &&
		t.Version != "" && len(t.Ciphertext) > 0 && CodexStateLength(t.Plan) > 0 &&
		!t.CapturedAt.After(now) && t.ExpiresAt.After(now) && t.ExpiresAt.After(t.CapturedAt)
}

// CodexStateReceipt identifies the precise ticket used by an in-flight request.
// A late response must never revoke a newer replacement ticket.
type CodexStateReceipt struct{ AccountID, Model, Version string }

func (t CodexStateTicket) UsedBy(receipt CodexStateReceipt) bool {
	return receipt.Version != "" && t.AccountID == receipt.AccountID && t.Model == receipt.Model && t.Version == receipt.Version
}

// RejectCodexState only uses concrete upstream signals. Incomplete streams do
// not prove a model mismatch and completed business requests must not be replayed.
func RejectCodexState(returnedState, expectedModel, actualModel string, completed bool) bool {
	return ValidCodexState(returnedState, 312) || completed && actualModel != "" && actualModel != expectedModel
}
