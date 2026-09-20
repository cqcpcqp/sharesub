package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

type stateStoreStub struct {
	CodexStateStore
	account  domain.Account
	job      CodexStateJob
	finishes int
	ensures  int
}

func (s *stateStoreStub) AccountByID(context.Context, string) (domain.Account, error) {
	return s.account, nil
}
func (s *stateStoreStub) EnsureStateJob(context.Context, string, string, string, string) (CodexStateJob, error) {
	s.ensures++
	return s.job, nil
}
func (s *stateStoreStub) ListStateJobs(context.Context, string) ([]CodexStateJob, error) {
	return []CodexStateJob{s.job}, nil
}
func (s *stateStoreStub) FinishStateJob(_ context.Context, j CodexStateJob) error {
	s.job = j
	s.finishes++
	return nil
}

type stateProberFunc func(context.Context, CodexStateProbe) (string, error)

func (f stateProberFunc) ProbeState(ctx context.Context, p CodexStateProbe) (string, error) {
	return f(ctx, p)
}
func TestStateCollectEncryptsValidatedCandidate(t *testing.T) {
	secrets, _ := security.New(make([]byte, 32), make([]byte, 32))
	token, _ := secrets.Encrypt("access", []byte("u:c:access"))
	store := &stateStoreStub{account: domain.Account{ID: "a", OwnerUserID: "u", ChatGPTAccountID: "c", PlanType: "pro", StateEnabled: true, Status: domain.StatusActive, AccessTokenCiphertext: token, TokenExpiresAt: time.Now().Add(time.Hour)}}
	value := "gAAAAA" + strings.Repeat("a", 286)
	calls := 0
	service := NewCodexStateService(store, store, secrets, stateProberFunc(func(_ context.Context, p CodexStateProbe) (string, error) {
		calls++
		if calls == 2 && p.State != value {
			t.Fatal("validation did not use candidate")
		}
		return value, nil
	}), "fixed")
	job := CodexStateJob{AccountID: "a", Model: "m", Lease: "lease", Fingerprint: service.fingerprint(store.account)}
	service.collect(context.Background(), job)
	if calls != 2 || store.finishes != 1 || store.job.Result != "ready" || string(store.job.Ciphertext) == value {
		t.Fatal("candidate not encrypted after validation")
	}
	got, receipt := service.PrepareState(context.Background(), "a", "m", CodexStateAccountScope(store.account))
	if got != value || receipt.Version != store.job.Version {
		t.Fatal("stored ticket not usable")
	}
	service.prober = stateProberFunc(func(context.Context, CodexStateProbe) (string, error) {
		return "", &StateProbeError{Reason: "rate_limited"}
	})
	service.collect(context.Background(), store.job)
	if store.job.Result != "rate_limited" || store.job.Version != receipt.Version {
		t.Fatal("renewal discarded old ticket")
	}
	store.account.StateEnabled = false
	got, _ = service.PrepareState(context.Background(), "a", "m", CodexStateAccountScope(store.account))
	if got != "" {
		t.Fatal("disabled account injected")
	}
}
func TestStateValidationRejects312AndIdentityChanges(t *testing.T) {
	for _, changed := range []bool{false, true} {
		secrets, _ := security.New(make([]byte, 32), make([]byte, 32))
		token, _ := secrets.Encrypt("access", []byte("u:c:access"))
		store := &stateStoreStub{account: domain.Account{ID: "a", OwnerUserID: "u", ChatGPTAccountID: "c", PlanType: "team", StateEnabled: true, Status: domain.StatusActive, AccessTokenCiphertext: token, TokenExpiresAt: time.Now().Add(time.Hour)}}
		calls := 0
		service := NewCodexStateService(store, store, secrets, stateProberFunc(func(context.Context, CodexStateProbe) (string, error) {
			calls++
			if changed {
				store.account.StateEnabled = false
			}
			if calls == 1 {
				return "gAAAAA" + strings.Repeat("a", 326), nil
			}
			return "gAAAAA" + strings.Repeat("a", 306), nil
		}), "")
		service.collect(context.Background(), CodexStateJob{AccountID: "a", Model: "m", Fingerprint: service.fingerprint(store.account)})
		if store.job.Version != "" || store.job.Result == "ready" {
			t.Fatal("invalid candidate saved")
		}
	}
}

func TestStateProbeSharesAccountTrafficLimits(t *testing.T) {
	store := &stateStoreStub{account: domain.Account{ID: "a", PlanType: "pro", StateEnabled: true, Status: domain.StatusActive, MaxConcurrency: 1}}
	called := false
	service := NewCodexStateService(store, store, nil, stateProberFunc(func(context.Context, CodexStateProbe) (string, error) { called = true; return "", nil }), "")
	release, err := service.traffic.acquire("a", 1, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, err = service.probe(context.Background(), CodexStateJob{AccountID: "a", Fingerprint: service.fingerprint(store.account)}, CodexStateProbe{})
	if called || stateProbeReason(err) != "account_busy" {
		t.Fatal("probe bypassed account concurrency")
	}
}

type stateManagementStore struct {
	Store
	account domain.Account
}

func (s *stateManagementStore) AccountByID(context.Context, string) (domain.Account, error) {
	return s.account, nil
}
func TestStateStatusRequiresOwnerOrAdmin(t *testing.T) {
	account := domain.Account{ID: "a", OwnerUserID: "owner", PlanType: "pro", StateEnabled: true}
	service := &Service{store: &stateManagementStore{account: account}, now: time.Now}
	for _, tc := range []struct {
		user    domain.User
		allowed bool
	}{
		{domain.User{ID: "owner"}, true}, {domain.User{ID: "other"}, false}, {domain.User{ID: "admin", Role: domain.RoleAdmin}, true},
	} {
		status, err := service.AccountCodexState(context.Background(), tc.user, "a", false)
		if tc.allowed {
			if err != nil || status.Models == nil || !status.Enabled || !status.Supported {
				t.Fatalf("status=%+v err=%v", status, err)
			}
		} else if err != domain.ErrForbidden {
			t.Fatalf("unauthorized read: %v", err)
		}
	}
}

func TestStateRequestSnapshotCannotUseOrEnrollNewConfiguration(t *testing.T) {
	secrets, _ := security.New(make([]byte, 32), make([]byte, 32))
	a := domain.Account{ID: "a", ChatGPTAccountID: "chat", PlanType: "pro", StateEnabled: true, Status: domain.StatusActive, ProxyURLCiphertext: []byte("old-proxy"), RefreshTokenCiphertext: []byte("auth"), CodexFingerprintMode: "session"}
	for _, tc := range []struct {
		name   string
		change func(*domain.Account)
	}{
		{"proxy", func(a *domain.Account) { a.ProxyURLCiphertext = []byte("new-proxy") }},
		{"identity", func(a *domain.Account) { a.ChatGPTAccountID = "new-chat" }},
		{"authorization", func(a *domain.Account) { a.RefreshTokenCiphertext = []byte("new-auth") }},
		{"fingerprint", func(a *domain.Account) { a.CodexFingerprintMode = "full" }},
		{"plan", func(a *domain.Account) { a.PlanType = "team" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &stateStoreStub{account: a}
			oldScope := CodexStateAccountScope(a)
			tc.change(&store.account)
			service := NewCodexStateService(store, store, secrets, nil, "")
			expires := time.Now().Add(time.Hour)
			store.job = CodexStateJob{AccountID: "a", Model: "m", Version: "new-ticket", Fingerprint: service.fingerprint(store.account), ExpiresAt: &expires}
			state := "gAAAAA" + strings.Repeat("a", domain.CodexStateLength(statePlan(store.account.PlanType))-6)
			store.job.Ciphertext, _ = secrets.Encrypt(state, stateAAD(store.job))
			got, r := service.PrepareState(context.Background(), "a", "m", oldScope)
			if got != "" || r.Version != "" {
				t.Fatal("old request used new ticket")
			}
			service.ConfirmState(context.Background(), "a", "m", oldScope)
			if store.ensures != 0 {
				t.Fatal("old response enrolled against new configuration")
			}
			scope := CodexStateAccountScope(store.account)
			got, r = service.PrepareState(context.Background(), "a", "m", scope)
			if got != state || r.Version != "new-ticket" {
				t.Fatal("matching request cannot use ticket")
			}
			service.ConfirmState(context.Background(), "a", "m", scope)
			if store.ensures != 1 {
				t.Fatal("matching completed request was not enrolled")
			}
		})
	}
}

func TestStateLookupDoesNotEnrollUnverifiedModels(t *testing.T) {
	store := &stateStoreStub{account: domain.Account{ID: "a", PlanType: "pro", StateEnabled: true, Status: domain.StatusActive}}
	service := NewCodexStateService(store, store, nil, nil, "")
	for _, model := range []string{"unknown1", "unknown2", "unknown3", "unknown4", "unknown5", "unknown6", "unknown7", "unknown8"} {
		value, _ := service.PrepareState(context.Background(), "a", model, CodexStateAccountScope(store.account))
		if value != "" {
			t.Fatal("unexpected ticket")
		}
	}
	if store.ensures != 0 {
		t.Fatal("unverified model consumed job slot")
	}
}

func TestSetAccountProxyPreservesCiphertextForUnchangedURL(t *testing.T) {
	secrets, _ := security.New(make([]byte, 32), make([]byte, 32))
	service := &Service{security: secrets}
	account := domain.Account{OwnerUserID: "owner", ChatGPTAccountID: "chat"}
	if err := service.setAccountProxy(&account, "http://proxy.example:8080"); err != nil {
		t.Fatal(err)
	}
	original := string(account.ProxyURLCiphertext)
	scope := CodexStateAccountScope(account)
	account.Name = "renamed"
	account.Notes = "new notes"
	if err := service.setAccountProxy(&account, "http://proxy.example:8080"); err != nil {
		t.Fatal(err)
	}
	if string(account.ProxyURLCiphertext) != original || CodexStateAccountScope(account) != scope {
		t.Fatal("same proxy rotated ciphertext/scope")
	}
	if err := service.setAccountProxy(&account, "http://other.example:8080"); err != nil {
		t.Fatal(err)
	}
	if string(account.ProxyURLCiphertext) == original || CodexStateAccountScope(account) == scope {
		t.Fatal("changed proxy did not rotate scope")
	}
	value, err := secrets.Decrypt(account.ProxyURLCiphertext, []byte("owner:chat:proxy"))
	if err != nil || value != "http://other.example:8080" {
		t.Fatal("changed proxy not saved", err)
	}
	if err := service.setAccountProxy(&account, ""); err != nil || len(account.ProxyURLCiphertext) != 0 {
		t.Fatal("removing proxy failed", err)
	}
}
