package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

var stateModelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type CodexStateService struct {
	accounts interface {
		AccountByID(context.Context, string) (domain.Account, error)
	}
	traffic  *accountTrafficController
	store    CodexStateStore
	security *security.Manager
	prober   CodexStateProber
	egress   string
	now      func() time.Time
}

func NewCodexStateService(accounts interface {
	AccountByID(context.Context, string) (domain.Account, error)
}, store CodexStateStore, secrets *security.Manager, prober CodexStateProber, egress string) *CodexStateService {
	return &CodexStateService{accounts: accounts, store: store, security: secrets, prober: prober, egress: egress, now: time.Now, traffic: newAccountTrafficController()}
}

func statePlan(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pro", "chatgptpro":
		return "pro"
	case "team", "business", "chatgptteam", "chatgptbusiness":
		return "team"
	}
	return ""
}

// CodexStateAccountScope binds a ticket to the configuration snapshot used to
// resolve the outbound request, rather than a later account read.
func CodexStateAccountScope(a domain.Account) string {
	data, _ := json.Marshal([]any{a.ID, a.ChatGPTAccountID, a.PlanType, a.RefreshTokenCiphertext, a.ProxyURLCiphertext, a.CodexFingerprintMode})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func CodexStateAccessScope(access GatewayAccess) string {
	a := access.Credential.Account
	a.RefreshTokenCiphertext = access.Credential.RefreshTokenCiphertext
	a.ProxyURLCiphertext = access.Credential.ProxyURLCiphertext
	return CodexStateAccountScope(a)
}
func (s *CodexStateService) fingerprint(a domain.Account) string {
	data, _ := json.Marshal([]string{CodexStateAccountScope(a), s.egress})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func stateAAD(j CodexStateJob) []byte {
	return []byte("codex-state:" + j.AccountID + ":" + j.Model + ":" + j.Fingerprint + ":" + j.Version)
}

func (s *CodexStateService) PrepareState(ctx context.Context, accountID, model, scope string) (string, domain.CodexStateReceipt) {
	empty := domain.CodexStateReceipt{}
	if !stateModelPattern.MatchString(model) {
		return "", empty
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	a, err := s.accounts.AccountByID(ctx, accountID)
	if err != nil || !a.StateEnabled || a.Status != domain.StatusActive || statePlan(a.PlanType) == "" || scope != CodexStateAccountScope(a) {
		return "", empty
	}
	jobs, err := s.store.ListStateJobs(ctx, accountID)
	if err != nil {
		return "", empty
	}
	var j CodexStateJob
	for _, candidate := range jobs {
		if candidate.Model == model && candidate.Fingerprint == s.fingerprint(a) {
			j = candidate
			break
		}
	}
	if err != nil || j.Version == "" || j.ExpiresAt == nil || !j.ExpiresAt.After(s.now()) {
		return "", empty
	}
	value, err := s.security.Decrypt(j.Ciphertext, stateAAD(j))
	if err != nil || !domain.ValidCodexState(value, domain.CodexStateLength(statePlan(a.PlanType))) {
		return "", empty
	}
	return value, domain.CodexStateReceipt{AccountID: accountID, Model: model, Version: j.Version}
}

// Only a completed business response with the requested model can enroll a job.
func (s *CodexStateService) ConfirmState(ctx context.Context, accountID, model, scope string) {
	if !stateModelPattern.MatchString(model) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	a, err := s.accounts.AccountByID(ctx, accountID)
	if err != nil || !a.StateEnabled || a.Status != domain.StatusActive || statePlan(a.PlanType) == "" || scope != CodexStateAccountScope(a) {
		return
	}
	_, _ = s.store.EnsureStateJob(ctx, accountID, model, s.fingerprint(a), scope)
}

func (s *CodexStateService) RejectState(ctx context.Context, r domain.CodexStateReceipt, reason string) {
	if r.Version == "" {
		return
	}
	if reason != "state_312" && reason != "model_mismatch" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = s.store.RejectStateTicket(ctx, r, reason)
}

// Run retains only recently used models. Two bounded workers avoid request-time
// probes and respect the database lease across API instances.
func (s *CodexStateService) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		lease, err := security.NewID()
		if err != nil {
			continue
		}
		jobs, err := s.store.ClaimStateJobs(ctx, lease)
		if err != nil {
			continue
		}
		var wg sync.WaitGroup
		for _, j := range jobs {
			wg.Add(1)
			go func(j CodexStateJob) { defer wg.Done(); s.collect(ctx, j) }(j)
		}
		wg.Wait()
	}
}

func (s *CodexStateService) probeIdentity(ctx context.Context, j CodexStateJob) (CodexStateProbe, string, error) {
	a, err := s.accounts.AccountByID(ctx, j.AccountID)
	if err != nil {
		return CodexStateProbe{}, "", err
	}
	plan := statePlan(a.PlanType)
	if !a.StateEnabled || a.Status != domain.StatusActive || plan == "" || s.fingerprint(a) != j.Fingerprint || !a.TokenExpiresAt.After(s.now().Add(2*time.Minute)) {
		return CodexStateProbe{}, "", domain.ErrAccountUnavailable
	}
	scope := a.OwnerUserID + ":" + a.ChatGPTAccountID
	token, err := s.security.Decrypt(a.AccessTokenCiphertext, []byte(scope+":access"))
	if err != nil {
		return CodexStateProbe{}, "", err
	}
	proxy := ""
	if len(a.ProxyURLCiphertext) > 0 {
		proxy, err = s.security.Decrypt(a.ProxyURLCiphertext, []byte(scope+":proxy"))
		if err != nil {
			return CodexStateProbe{}, "", err
		}
	}
	return CodexStateProbe{AccessToken: token, ChatGPTAccountID: a.ChatGPTAccountID, ProxyURL: proxy, Model: j.Model}, plan, nil
}

func (s *CodexStateService) probe(ctx context.Context, j CodexStateJob, p CodexStateProbe) (string, error) {
	a, err := s.accounts.AccountByID(ctx, j.AccountID)
	if err != nil || !a.StateEnabled || a.Status != domain.StatusActive || s.fingerprint(a) != j.Fingerprint {
		return "", &StateProbeError{Reason: "identity_changed"}
	}
	release, err := s.traffic.acquire(a.ID, a.MaxConcurrency, a.RPMLimit, s.now())
	if err != nil {
		return "", &StateProbeError{Reason: "account_busy"}
	}
	defer release()
	return s.prober.ProbeState(ctx, p)
}

func (s *CodexStateService) collect(ctx context.Context, j CodexStateJob) {
	ctx, cancel := context.WithTimeout(ctx, 140*time.Second)
	defer cancel()
	j.Attempts++
	j.Result = "probe_failed"
	j.NextAttemptAt = s.now().Add(5 * time.Minute)
	defer func() {
		if ctx.Err() == nil {
			_ = s.store.FinishStateJob(ctx, j)
		}
	}()
	p, plan, err := s.probeIdentity(ctx, j)
	if err != nil {
		j.Result = "identity_unavailable"
		return
	}
	// With a fixed exit, one acquisition plus one validation per round avoids
	// spending quota on repeated identical probes. Failure starts a cooldown.
	captured := s.now()
	value, err := s.probe(ctx, j, p)
	if err != nil {
		j.Result = stateProbeReason(err)
		return
	}
	if !domain.ValidCodexState(value, domain.CodexStateLength(plan)) {
		j.Result = "unexpected_state"
		return
	}
	p, _, err = s.probeIdentity(ctx, j)
	if err != nil {
		j.Result = "identity_changed"
		return
	}
	p.State = value
	returned, err := s.probe(ctx, j, p)
	if err != nil {
		j.Result = stateProbeReason(err)
		return
	}
	if domain.ValidCodexState(returned, 312) {
		j.Result = "validation_rejected"
		return
	}
	if _, _, err = s.probeIdentity(ctx, j); err != nil {
		j.Result = "identity_changed"
		return
	}
	version, err := security.NewID()
	if err != nil {
		return
	}
	j.Version = version
	cipher, err := s.security.Encrypt(value, stateAAD(j))
	if err != nil {
		return
	}
	expires := captured.Add(time.Hour)
	j.Ciphertext = cipher
	j.ExpiresAt = &expires
	j.NextAttemptAt = expires.Add(-10 * time.Minute)
	j.Result = "ready"
}

// StateProbeError carries a sanitized reason only, never response bodies,
// authorization headers or proxy URLs.
type StateProbeError struct{ Reason string }

func (e *StateProbeError) Error() string { return e.Reason }
func stateProbeReason(err error) string {
	var e *StateProbeError
	if errors.As(err, &e) {
		switch e.Reason {
		case "identity_changed", "account_busy", "unauthorized", "forbidden", "rate_limited", "model_mismatch", "incomplete_response", "transport_failed", "upstream_rejected":
			return e.Reason
		}
	}
	return "probe_failed"
}
