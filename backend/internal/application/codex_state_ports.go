package application

import (
	"context"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

// CodexStateController is the optional gateway capability. Failures of this
// experimental feature must never make the account's ordinary route unavailable.
type CodexStateController interface {
	PrepareState(context.Context, string, string, string) (string, domain.CodexStateReceipt)
	ConfirmState(context.Context, string, string, string)
	RejectState(context.Context, domain.CodexStateReceipt, string)
}

type CodexStateJob struct {
	AccountID     string     `json:"-"`
	Model         string     `json:"model"`
	Fingerprint   string     `json:"-"`
	Lease         string     `json:"-"`
	Version       string     `json:"-"`
	Ciphertext    []byte     `json:"-"`
	ExpiresAt     *time.Time `json:"expires_at"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	Result        string     `json:"result"`
	Attempts      int        `json:"attempts"`
	LastUsedAt    time.Time  `json:"last_used_at"`
}

type CodexStateStore interface {
	EnsureStateJob(context.Context, string, string, string, string) (CodexStateJob, error)
	ClaimStateJobs(context.Context, string) ([]CodexStateJob, error)
	FinishStateJob(context.Context, CodexStateJob) error
	RejectStateTicket(context.Context, domain.CodexStateReceipt, string) error
	ListStateJobs(context.Context, string) ([]CodexStateJob, error)
	RefreshStateJobs(context.Context, string) error
}

type CodexStateProbe struct {
	AccessToken, ChatGPTAccountID, ProxyURL, Model, State string
}

type CodexStateProber interface {
	ProbeState(context.Context, CodexStateProbe) (string, error)
}
