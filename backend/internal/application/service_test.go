package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

type createPlanStore struct {
	Store
	account           domain.Account
	detail            domain.PlanDetail
	created           bool
	accountLookups    int
	createdPlan       domain.Plan
	createdMember     domain.Member
	createdSignals    []domain.QuotaSignal
	bindingObservedAt time.Time
}

type rebindPlanStore struct {
	Store
	plan domain.Plan
	err  error
}

func (s *rebindPlanStore) PlanBinding(context.Context, string, string) (domain.Plan, error) {
	return s.plan, s.err
}

type staticQuotaProber struct {
	signals []domain.QuotaSignal
	err     error
	calls   int
}

func completeQuotaSignals(now time.Time) []domain.QuotaSignal {
	return []domain.QuotaSignal{
		{WindowType: domain.Window5H, WindowStart: now.Add(-time.Hour), ResetAt: now.Add(4 * time.Hour)},
		{WindowType: domain.Window7D, WindowStart: now.Add(-24 * time.Hour), ResetAt: now.Add(6 * 24 * time.Hour)},
	}
}

func (p *staticQuotaProber) ProbeQuota(context.Context, string, string, string) ([]domain.QuotaSignal, error) {
	p.calls++
	return p.signals, p.err
}

type dashboardStore struct {
	Store
	userID      string
	todayStart  time.Time
	trendStart  time.Time
	now         time.Time
	result      domain.Dashboard
	dashboarded bool
}

type accountConfigStore struct {
	Store
	account domain.Account
	updated domain.Account
}

type planAccountStore struct {
	Store
	detail               domain.PlanDetail
	performance          domain.PlanPerformance
	performancePlanID    string
	performanceUserID    string
	performanceStartedAt time.Time
	performanceEndedAt   time.Time
	performanceBucket    time.Duration
	requestErrors        domain.PlanRequestErrorList
	errorPlanID          string
	errorUserID          string
	errorStartedAt       time.Time
	errorEndedAt         time.Time
	errorPage            int
	errorPageSize        int
}

type planMetadataStore struct {
	Store
	description string
	event       domain.AuditEvent
}

func (s *planMetadataStore) UpdatePlanDescription(_ context.Context, _, _ string, description string, event domain.AuditEvent) (domain.Plan, error) {
	s.description = description
	s.event = event
	return domain.Plan{ID: "plan-id", Description: description}, nil
}

func (s *planAccountStore) PlanPerformance(_ context.Context, planID, userID string, startedAt, endedAt time.Time, bucketSize time.Duration) (domain.PlanPerformance, error) {
	s.performancePlanID = planID
	s.performanceUserID = userID
	s.performanceStartedAt = startedAt
	s.performanceEndedAt = endedAt
	s.performanceBucket = bucketSize
	return s.performance, nil
}

func (s *planAccountStore) PlanRequestErrors(_ context.Context, planID, userID string, startedAt, endedAt time.Time, page, pageSize int) (domain.PlanRequestErrorList, error) {
	s.errorPlanID = planID
	s.errorUserID = userID
	s.errorStartedAt = startedAt
	s.errorEndedAt = endedAt
	s.errorPage = page
	s.errorPageSize = pageSize
	return s.requestErrors, nil
}

type inviteStore struct {
	Store
	invite domain.Invite
}

type apiKeyStore struct {
	Store
	created domain.APIKey
	listed  []domain.APIKey
}

type avatarStore struct {
	Store
	avatar    domain.UserAvatar
	updatedAt time.Time
	user      domain.User
}

type quotaProbeStore struct {
	Store
	mu                    sync.Mutex
	credential            domain.PlanQuotaCredential
	updatedAt             time.Time
	resetAccountID        string
	resetPlanID           string
	resetGeneration       int64
	resetSignals          []domain.QuotaSignal
	resetRecordedAt       time.Time
	recordedPlanID        string
	recordedAccountID     string
	recordedGeneration    int64
	recordedSignals       []domain.QuotaSignal
	recordedAt            time.Time
	ownerCredentialCalls  int
	memberCredentialCalls int
	memberPlanID          string
	memberUserID          string
}

func (s *quotaProbeStore) PlanQuotaCredential(context.Context, string, string) (domain.PlanQuotaCredential, error) {
	s.ownerCredentialCalls++
	return s.credential, nil
}

func (s *quotaProbeStore) PlanBinding(context.Context, string, string) (domain.Plan, error) {
	return domain.Plan{
		ID:                       s.credential.PlanID,
		AccountID:                s.credential.AccountID,
		AccountBindingGeneration: s.credential.AccountBindingGeneration,
	}, nil
}

func (s *quotaProbeStore) PlanQuotaCredentialForMember(_ context.Context, planID, userID string) (domain.PlanQuotaCredential, error) {
	s.memberCredentialCalls++
	s.memberPlanID = planID
	s.memberUserID = userID
	return s.credential, nil
}

func (s *quotaProbeStore) AccountQuotaUpdatedAt(context.Context, string) (time.Time, error) {
	return s.updatedAt, nil
}

func (s *quotaProbeStore) RecordQuotaResetSignals(_ context.Context, planID, accountID string, generation int64, signals []domain.QuotaSignal, recordedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetPlanID = planID
	s.resetAccountID = accountID
	s.resetGeneration = generation
	s.resetSignals = signals
	s.resetRecordedAt = recordedAt
	return nil
}

func (s *quotaProbeStore) RecordAccountQuotaSignals(_ context.Context, planID, accountID string, generation int64, signals []domain.QuotaSignal, recordedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordedPlanID = planID
	s.recordedAccountID = accountID
	s.recordedGeneration = generation
	s.recordedSignals = signals
	s.recordedAt = recordedAt
	return nil
}

func (s *avatarStore) UpdateUserAvatar(_ context.Context, _ string, avatar domain.UserAvatar, updatedAt time.Time) (domain.User, error) {
	s.avatar = avatar
	s.updatedAt = updatedAt
	return s.user, nil
}

func (s *apiKeyStore) CreateAPIKey(_ context.Context, key domain.APIKey, _ []domain.APIKeyRoute) error {
	s.created = key
	return nil
}

func (s *apiKeyStore) ListAPIKeys(context.Context, string) ([]domain.APIKey, error) {
	return s.listed, nil
}

func (s *inviteStore) CreateInvite(_ context.Context, _, _ string, invite domain.Invite, _ domain.AuditEvent) error {
	s.invite = invite
	return nil
}

func TestInviteReturnsFragmentLinkWithoutBareTokenField(t *testing.T) {
	store := &inviteStore{}
	service := &Service{store: store, security: testSecurityManager(t), publicURL: "https://sharesub.example.com", now: func() time.Time { return time.Unix(0, 0) }}
	created, err := service.Invite(context.Background(), "owner", "plan", 2500)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.InviteURL, "https://sharesub.example.com/#/invite/ss_invite_") {
		t.Fatalf("invite URL = %q", created.InviteURL)
	}
	body, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"token"`) {
		t.Fatalf("created invite exposed bare token field: %s", body)
	}
	if strings.Contains(string(body), `"email"`) {
		t.Fatalf("generic invite exposed an email field: %s", body)
	}
	if len(store.invite.TokenHash) == 0 {
		t.Fatal("invite token hash was not persisted")
	}
}

func TestAPIKeySecretIsEncryptedAndHydratedForOwner(t *testing.T) {
	manager := testSecurityManager(t)
	store := &apiKeyStore{}
	service := &Service{store: store, security: manager, now: func() time.Time { return time.Unix(0, 0) }}

	created, err := service.CreateAPIKey(context.Background(), "owner", "Codex", domain.RouteBalanced, []domain.APIKeyRoute{{PlanID: "plan", Priority: 1, Enabled: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.Key == "" || len(store.created.KeyCiphertext) == 0 || string(store.created.KeyCiphertext) == created.Key {
		t.Fatalf("created key was not encrypted: %+v", store.created)
	}
	plain, err := manager.Decrypt(store.created.KeyCiphertext, apiKeySecretAssociatedData("owner", store.created.ID))
	if err != nil || plain != created.Key {
		t.Fatalf("decrypted key = %q, %v", plain, err)
	}

	persisted := store.created
	persisted.Key = ""
	persisted.KeyAvailable = false
	store.listed = []domain.APIKey{persisted, {ID: "legacy", UserID: "owner", Name: "Legacy"}}
	for attempt := 0; attempt < 2; attempt++ {
		keys, err := service.ListAPIKeys(context.Background(), "owner")
		if err != nil {
			t.Fatal(err)
		}
		if keys[0].Key != created.Key || !keys[0].KeyAvailable {
			t.Fatalf("hydrated key on attempt %d = %+v", attempt+1, keys[0])
		}
		if keys[1].Key != "" || keys[1].KeyAvailable {
			t.Fatalf("legacy key availability = %+v", keys[1])
		}
	}
}

func (s *accountConfigStore) AccountByID(context.Context, string) (domain.Account, error) {
	return s.account, nil
}

func (s *accountConfigStore) UpdateAccountConfig(_ context.Context, _ string, account domain.Account) (domain.Account, error) {
	s.updated = account
	return account, nil
}

func (s *planAccountStore) PlanDetail(context.Context, string, string, time.Time, time.Time) (domain.PlanDetail, error) {
	return s.detail, nil
}

func TestUpdateAccountConfigEncryptsProxyAndChecksOwnership(t *testing.T) {
	manager := testSecurityManager(t)
	store := &accountConfigStore{account: domain.Account{ID: "account", OwnerUserID: "owner", ChatGPTAccountID: "chatgpt"}}
	service := &Service{store: store, security: manager, now: time.Now}
	policy := []domain.FastPolicyRule{{ServiceTier: "priority", Action: "filter", UserIDs: []string{"member"}, ModelWhitelist: []string{"gpt-5.5*"}, FallbackAction: "pass"}}
	config := AccountConfigInput{Name: "团队主账号", Notes: "仅用于 Codex", ProxyURL: "socks5://proxy.example:1080", MaxConcurrency: 8, RPMLimit: 120, FastPolicy: policy, Status: domain.StatusActive}

	account, err := service.UpdateAccountConfig(context.Background(), "owner", "account", config)
	if err != nil {
		t.Fatal(err)
	}
	if account.ProxyURL != config.ProxyURL || string(store.updated.ProxyURLCiphertext) == config.ProxyURL {
		t.Fatalf("updated account = %+v", account)
	}
	if len(store.updated.FastPolicy) != 1 || store.updated.FastPolicy[0].ServiceTier != "priority" {
		t.Fatalf("updated fast policy = %+v", store.updated.FastPolicy)
	}
	plaintext, err := manager.Decrypt(store.updated.ProxyURLCiphertext, []byte("owner:chatgpt:proxy"))
	if err != nil || plaintext != config.ProxyURL {
		t.Fatalf("decrypted proxy = %q, %v", plaintext, err)
	}
	if _, err := service.UpdateAccountConfig(context.Background(), "member", "account", config); err != domain.ErrForbidden {
		t.Fatalf("non-owner update error = %v, want forbidden", err)
	}
}

func TestNormalizeAccountConfigRejectsUnsupportedValues(t *testing.T) {
	valid := AccountConfigInput{Name: "账号", Status: domain.StatusActive}
	tests := []AccountConfigInput{
		{Name: "", Status: domain.StatusActive},
		{Name: "账号", ProxyURL: "ftp://proxy.example", Status: domain.StatusActive},
		{Name: "账号", MaxConcurrency: 101, Status: domain.StatusActive},
		{Name: "账号", RPMLimit: 10_001, Status: domain.StatusActive},
		{Name: "账号", Status: "unknown"},
		{Name: "账号", FastPolicy: []domain.FastPolicyRule{{ServiceTier: "turbo", Action: "pass", FallbackAction: "pass"}}, Status: domain.StatusActive},
		{Name: "账号", FastPolicy: []domain.FastPolicyRule{{ServiceTier: "priority", Action: "filter", ModelWhitelist: []string{"gpt-*-codex"}, FallbackAction: "pass"}}, Status: domain.StatusActive},
	}
	if _, err := normalizeAccountConfig(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	for _, config := range tests {
		if _, err := normalizeAccountConfig(config); err != domain.ErrInvalidInput {
			t.Fatalf("config %+v error = %v, want invalid input", config, err)
		}
	}
}

func TestNormalizeFastPolicyForAPIKeyRejectsMemberScope(t *testing.T) {
	policy := []domain.FastPolicyRule{{ServiceTier: "priority", Action: "force_priority", UserIDs: []string{"other-user"}, FallbackAction: "pass"}}
	if _, err := normalizeFastPolicy(policy, false); err != domain.ErrInvalidInput {
		t.Fatalf("key policy member scope error = %v, want invalid input", err)
	}
	normalized, err := normalizeFastPolicy([]domain.FastPolicyRule{{ServiceTier: " PRIORITY ", Action: " FORCE_PRIORITY ", FallbackAction: " PASS "}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if normalized[0].ServiceTier != "priority" || normalized[0].Action != "force_priority" || normalized[0].UserIDs == nil || normalized[0].ModelWhitelist == nil {
		t.Fatalf("normalized key policy = %+v", normalized)
	}
}

func TestPlanMemberSeesHydratedAccountConfiguration(t *testing.T) {
	manager := testSecurityManager(t)
	proxyURL := "socks5://proxy.example:1080"
	ciphertext, err := manager.Encrypt(proxyURL, []byte("owner:chatgpt:proxy"))
	if err != nil {
		t.Fatal(err)
	}
	store := &planAccountStore{detail: domain.PlanDetail{
		Plan: domain.Plan{ID: "plan", OwnerUserID: "owner"},
		Account: &domain.Account{
			ID: "account", OwnerUserID: "owner", Name: "团队主账号", Notes: "仅用于 Codex",
			Email: "openai@example.com", ChatGPTAccountID: "chatgpt", PlanType: "plus",
			ProxyURLCiphertext: ciphertext, MaxConcurrency: 6, RPMLimit: 90, Status: domain.StatusActive,
		},
	}}
	service := &Service{store: store, security: manager, now: time.Now}

	detail, err := service.PlanDetail(context.Background(), "member", "plan", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Account.ProxyURL != proxyURL || detail.Account.Name != "团队主账号" || detail.Account.Notes != "仅用于 Codex" || detail.Account.Email != "openai@example.com" || detail.Account.ChatGPTAccountID != "chatgpt" || detail.Account.MaxConcurrency != 6 || detail.Account.RPMLimit != 90 {
		t.Fatalf("member-visible account configuration = %+v", detail.Account)
	}
}

func TestPlanPerformanceUsesRequestedFixedPeriod(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	type periodConfig struct{ duration, bucket time.Duration }
	periods := map[string]periodConfig{
		"30m": {duration: 30 * time.Minute, bucket: time.Minute},
		"6h":  {duration: 6 * time.Hour, bucket: 15 * time.Minute},
		"12h": {duration: 12 * time.Hour, bucket: 30 * time.Minute},
		"24h": {duration: 24 * time.Hour, bucket: time.Hour},
	}
	for period, config := range periods {
		t.Run(period, func(t *testing.T) {
			store := &planAccountStore{performance: domain.PlanPerformance{PerformanceSummary: domain.PerformanceSummary{RequestCount: 12}, ModelUsage: []domain.ModelUsage{{Model: "gpt-5.6-sol"}}}}
			service := &Service{store: store, now: func() time.Time { return now }}
			performance, err := service.PlanPerformance(context.Background(), "member", "plan", period, "Asia/Shanghai")
			if err != nil {
				t.Fatal(err)
			}
			if performance.RequestCount != 12 || len(performance.ModelUsage) != 1 || store.performancePlanID != "plan" || store.performanceUserID != "member" || !store.performanceStartedAt.Equal(now.Add(-config.duration)) || !store.performanceEndedAt.Equal(now) || store.performanceBucket != config.bucket {
				t.Fatalf("performance = %+v, plan = %q, user = %q, start = %s", performance, store.performancePlanID, store.performanceUserID, store.performanceStartedAt)
			}
		})
	}
	t.Run("today", func(t *testing.T) {
		store := &planAccountStore{performance: domain.PlanPerformance{}}
		service := &Service{store: store, now: func() time.Time { return now }}
		if _, err := service.PlanPerformance(context.Background(), "member", "plan", "today", "Asia/Shanghai"); err != nil {
			t.Fatal(err)
		}
		wantStart := time.Date(2026, 8, 4, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
		if !store.performanceStartedAt.Equal(wantStart) || store.performanceBucket != time.Hour {
			t.Fatalf("today start = %s, bucket = %s", store.performanceStartedAt, store.performanceBucket)
		}
	})
	service := &Service{store: &planAccountStore{}, now: func() time.Time { return now }}
	if _, err := service.PlanPerformance(context.Background(), "member", "plan", "1h", "Asia/Shanghai"); err != domain.ErrInvalidInput {
		t.Fatalf("invalid period error = %v, want invalid input", err)
	}
	if _, err := service.PlanPerformance(context.Background(), "member", "plan", "today", "invalid/timezone"); err != domain.ErrInvalidInput {
		t.Fatalf("invalid timezone error = %v, want invalid input", err)
	}
}

func TestPlanRequestErrorsUsesPerformanceWindowAndPagination(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	store := &planAccountStore{requestErrors: domain.PlanRequestErrorList{
		Items: []domain.PlanRequestError{{ID: 7, RequestID: "request-7"}}, Total: 1, Page: 2, PageSize: 10,
	}}
	service := &Service{store: store, now: func() time.Time { return now }}

	result, err := service.PlanRequestErrors(context.Background(), "member", "plan", "6h", "Asia/Shanghai", 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 || store.errorPlanID != "plan" || store.errorUserID != "member" || store.errorPage != 2 || store.errorPageSize != 10 {
		t.Fatalf("result = %+v, store = %+v", result, store)
	}
	if !store.errorStartedAt.Equal(now.Add(-6*time.Hour)) || !store.errorEndedAt.Equal(now) {
		t.Fatalf("error window = %s - %s", store.errorStartedAt, store.errorEndedAt)
	}
	if _, err := service.PlanRequestErrors(context.Background(), "member", "plan", "6h", "Asia/Shanghai", 0, 10); err != domain.ErrInvalidInput {
		t.Fatalf("invalid page error = %v", err)
	}
	if _, err := service.PlanRequestErrors(context.Background(), "member", "plan", "6h", "Asia/Shanghai", 1, 101); err != domain.ErrInvalidInput {
		t.Fatalf("invalid page size error = %v", err)
	}
	maxInt := int(^uint(0) >> 1)
	if _, err := service.PlanRequestErrors(context.Background(), "member", "plan", "6h", "Asia/Shanghai", maxInt, 100); err != domain.ErrInvalidInput {
		t.Fatalf("overflowing page error = %v", err)
	}
}

func TestPrepareAutomaticPlanQuotaProbeSkipsFreshSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	store := &quotaProbeStore{
		credential: domain.PlanQuotaCredential{AccountID: "account"},
		updatedAt:  now.Add(-automaticQuotaProbeTTL + time.Second),
	}
	service := &Service{store: store, now: func() time.Time { return now }}

	probe, shouldProbe, release, err := service.PrepareAutomaticPlanQuotaProbe(context.Background(), "owner", "plan")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if shouldProbe || probe.AccountID != "account" {
		t.Fatalf("probe = %+v, shouldProbe = %v, want fresh snapshot skip", probe, shouldProbe)
	}
}

func TestPreparePlanQuotaProbeForMemberUsesMemberCredential(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	credential := domain.PlanQuotaCredential{
		AccountID: "account", AccountOwnerUserID: "owner", ChatGPTAccountID: "chatgpt", TokenExpiresAt: now.Add(time.Hour),
	}
	var err error
	credential.AccessTokenCiphertext, err = manager.Encrypt("access-token", []byte("owner:chatgpt:access"))
	if err != nil {
		t.Fatal(err)
	}
	store := &quotaProbeStore{credential: credential}
	service := &Service{store: store, security: manager, now: func() time.Time { return now }}

	probe, err := service.PreparePlanQuotaProbeForMember(context.Background(), "member", "plan")
	if err != nil {
		t.Fatal(err)
	}
	if probe.AccessToken != "access-token" || store.memberCredentialCalls != 1 || store.ownerCredentialCalls != 0 || store.memberPlanID != "plan" || store.memberUserID != "member" {
		t.Fatalf("probe = %+v, owner calls = %d, member call = %d (%q, %q)", probe, store.ownerCredentialCalls, store.memberCredentialCalls, store.memberPlanID, store.memberUserID)
	}
}

func TestPrepareAutomaticPlanQuotaProbePreparesStaleSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	credential := domain.PlanQuotaCredential{
		PlanID:                   "plan",
		AccountID:                "account",
		AccountBindingGeneration: 2,
		AccountOwnerUserID:       "owner",
		ChatGPTAccountID:         "chatgpt",
		TokenExpiresAt:           now.Add(time.Hour),
	}
	var err error
	credential.AccessTokenCiphertext, err = manager.Encrypt("access-token", []byte("owner:chatgpt:access"))
	if err != nil {
		t.Fatal(err)
	}
	store := &quotaProbeStore{credential: credential, updatedAt: now.Add(-automaticQuotaProbeTTL)}
	service := &Service{store: store, security: manager, now: func() time.Time { return now }}

	probe, shouldProbe, release, err := service.PrepareAutomaticPlanQuotaProbe(context.Background(), "owner", "plan")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if !shouldProbe || probe.AccessToken != "access-token" || probe.AccountID != "account" {
		t.Fatalf("probe = %+v, shouldProbe = %v, want prepared stale snapshot probe", probe, shouldProbe)
	}
}

func TestRecordResetQuotaSignalsUsesOwnerAccountAndForcedResetStorePath(t *testing.T) {
	now := time.Date(2026, 8, 6, 11, 30, 0, 0, time.UTC)
	store := &quotaProbeStore{}
	service := &Service{store: store, now: func() time.Time { return now }}
	signals := completeQuotaSignals(now)
	probe := PlanQuotaProbe{PlanID: "plan", AccountID: "account", AccountBindingGeneration: 2}
	if err := service.RecordResetQuotaSignals(context.Background(), probe, signals); err != nil {
		t.Fatal(err)
	}
	if store.resetPlanID != "plan" || store.resetAccountID != "account" || store.resetGeneration != 2 || len(store.resetSignals) != 2 || !store.resetRecordedAt.Equal(now) {
		t.Fatalf("reset recording = account %q signals %+v at %v", store.resetAccountID, store.resetSignals, store.resetRecordedAt)
	}
}

func TestQuotaProbeReservationDrainsBeforeResetAndCannotOverwriteIt(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	credential := domain.PlanQuotaCredential{
		PlanID:                   "plan",
		AccountID:                "account",
		AccountBindingGeneration: 2,
		AccountOwnerUserID:       "owner",
		ChatGPTAccountID:         "chatgpt",
		TokenExpiresAt:           now.Add(time.Hour),
	}
	credential.AccessTokenCiphertext, _ = manager.Encrypt("access", []byte("owner:chatgpt:access"))
	store := &quotaProbeStore{credential: credential}
	service := &Service{store: store, security: manager, traffic: newAccountTrafficController(), now: func() time.Time { return now }}

	probe, releaseProbe, err := service.ReservePlanQuotaProbe(context.Background(), "owner", "plan")
	if err != nil {
		t.Fatal(err)
	}
	type resetResult struct {
		probe   PlanQuotaProbe
		release func()
		err     error
	}
	resetDone := make(chan resetResult, 1)
	go func() {
		resetProbe, release, resetErr := service.QuiescePlanQuota(context.Background(), "owner", "plan")
		resetDone <- resetResult{probe: resetProbe, release: release, err: resetErr}
	}()

	deadline := time.Now().Add(time.Second)
	for {
		service.traffic.mu.Lock()
		quiescing := service.traffic.states["account"].quiescing
		service.traffic.mu.Unlock()
		if quiescing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reset never started draining the in-flight quota probe")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-resetDone:
		t.Fatal("reset acquired the account before the quota probe finished")
	default:
	}

	highSignals := completeQuotaSignals(now)
	highSignals[0].AccountUsedMicros = 91_000_000
	highSignals[1].AccountUsedMicros = 87_000_000
	if err := service.RecordManualQuotaSignals(context.Background(), probe, highSignals); err != nil {
		t.Fatal(err)
	}
	releaseProbe()

	reset := <-resetDone
	if reset.err != nil {
		t.Fatal(reset.err)
	}
	if reset.probe.AccountID != "account" || reset.probe.AccountBindingGeneration != 2 {
		t.Fatalf("reset probe = %+v", reset.probe)
	}
	resetSignals := completeQuotaSignals(now.Add(time.Minute))
	resetSignals[0].AccountUsedMicros = 4_000_000
	resetSignals[1].AccountUsedMicros = 9_000_000
	if err := service.RecordResetQuotaSignals(context.Background(), reset.probe, resetSignals); err != nil {
		t.Fatal(err)
	}
	reset.release()

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.recordedSignals) != 2 || store.recordedSignals[0].AccountUsedMicros != 91_000_000 {
		t.Fatalf("old probe signals = %+v", store.recordedSignals)
	}
	if len(store.resetSignals) != 2 || store.resetSignals[0].AccountUsedMicros != 4_000_000 || store.resetSignals[1].AccountUsedMicros != 9_000_000 {
		t.Fatalf("final reset signals = %+v", store.resetSignals)
	}
}

func TestQuotaSignalRecordingUsesPreparedBindingTuple(t *testing.T) {
	now := time.Date(2026, 8, 6, 11, 30, 0, 0, time.UTC)
	store := &quotaProbeStore{}
	service := &Service{store: store, now: func() time.Time { return now }}
	probe := PlanQuotaProbe{PlanID: "old-plan", AccountID: "old-account", AccountBindingGeneration: 7}
	signals := completeQuotaSignals(now)

	for _, record := range []struct {
		name string
		call func() error
	}{
		{name: "manual", call: func() error { return service.RecordManualQuotaSignals(context.Background(), probe, signals) }},
		{name: "automatic", call: func() error { return service.RecordAutomaticQuotaSignals(context.Background(), probe, signals) }},
	} {
		t.Run(record.name, func(t *testing.T) {
			store.recordedPlanID = ""
			if err := record.call(); err != nil {
				t.Fatal(err)
			}
			if store.recordedPlanID != probe.PlanID || store.recordedAccountID != probe.AccountID || store.recordedGeneration != probe.AccountBindingGeneration || len(store.recordedSignals) != 2 || !store.recordedAt.Equal(now) {
				t.Fatalf("recorded tuple = %q/%q/%d, signals = %d, at = %s", store.recordedPlanID, store.recordedAccountID, store.recordedGeneration, len(store.recordedSignals), store.recordedAt)
			}
		})
	}
}

func TestProbeAccountQuotaRequiresBothUniqueWindows(t *testing.T) {
	now := time.Date(2026, 8, 6, 11, 30, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	account := domain.Account{
		ID: "account", OwnerUserID: "owner", ChatGPTAccountID: "chatgpt", Status: domain.StatusActive,
		TokenExpiresAt: now.Add(time.Hour),
	}
	account.AccessTokenCiphertext, _ = manager.Encrypt("access", []byte("owner:chatgpt:access"))
	tests := []struct {
		name    string
		signals []domain.QuotaSignal
		wantErr bool
	}{
		{name: "complete", signals: completeQuotaSignals(now)},
		{name: "missing 7d", signals: completeQuotaSignals(now)[:1], wantErr: true},
		{name: "duplicate 5h", signals: []domain.QuotaSignal{completeQuotaSignals(now)[0], completeQuotaSignals(now)[0]}, wantErr: true},
		{name: "unknown", signals: []domain.QuotaSignal{{WindowType: "30d"}, completeQuotaSignals(now)[1]}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &createPlanStore{account: account}
			service := &Service{store: store, security: manager, quotaProber: &staticQuotaProber{signals: test.signals}, now: func() time.Time { return now }}
			_, _, _, err := service.probeAccountQuota(context.Background(), "owner", "account")
			if test.wantErr && err != domain.ErrAccountUnavailable {
				t.Fatalf("error = %v, want account unavailable", err)
			}
			if !test.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAccountTrafficControllerEnforcesConcurrencyAndRPM(t *testing.T) {
	controller := newAccountTrafficController()
	now := time.Date(2026, 8, 2, 10, 0, 5, 0, time.UTC)
	release, err := controller.acquire("account", 1, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.acquire("account", 1, 2, now); err != domain.ErrAccountConcurrency {
		t.Fatalf("concurrency error = %v", err)
	}
	release()
	release()
	releaseSecond, err := controller.acquire("account", 1, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	releaseSecond()
	if _, err := controller.acquire("account", 1, 2, now); err != domain.ErrAccountRateLimited {
		t.Fatalf("RPM error = %v", err)
	}
	if _, err := controller.acquire("account", 1, 2, now.Add(time.Minute)); err != nil {
		t.Fatalf("new minute did not reset RPM: %v", err)
	}
}

func TestQuiescePlanBindingPreservesBindingErrors(t *testing.T) {
	for _, wantErr := range []error{domain.ErrNotFound, domain.ErrForbidden} {
		t.Run(wantErr.Error(), func(t *testing.T) {
			service := &Service{
				store:   &rebindPlanStore{err: wantErr},
				traffic: newAccountTrafficController(),
			}
			if _, _, err := service.quiescePlanBinding(context.Background(), "owner", "plan"); err != wantErr {
				t.Fatalf("quiesce plan binding error = %v, want %v", err, wantErr)
			}
		})
	}
}

func TestQuiescePlanBindingMapsCanceledDrainToAccountUnavailable(t *testing.T) {
	traffic := newAccountTrafficController()
	releaseRequest, err := traffic.acquire("account", 0, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseRequest()
	service := &Service{
		store:   &rebindPlanStore{plan: domain.Plan{ID: "plan", AccountID: "account"}},
		traffic: traffic,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := service.quiescePlanBinding(ctx, "owner", "plan"); err != domain.ErrAccountUnavailable {
		t.Fatalf("canceled drain error = %v, want account unavailable", err)
	}
}

func (s *dashboardStore) Dashboard(_ context.Context, userID string, todayStart, trendStart, now time.Time) (domain.Dashboard, error) {
	s.userID = userID
	s.todayStart = todayStart
	s.trendStart = trendStart
	s.now = now
	s.dashboarded = true
	return s.result, nil
}

func TestDashboardUsesRequestedTimezoneBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 1, 18, 37, 0, 0, time.UTC)
	want := domain.Dashboard{TodayTokens: domain.TokenUsage{TotalTokens: 21}}
	store := &dashboardStore{result: want}
	service := &Service{store: store, now: func() time.Time { return now }}

	got, err := service.Dashboard(context.Background(), "user-id", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got.TodayTokens.TotalTokens != 21 || store.userID != "user-id" {
		t.Fatalf("Dashboard() = %+v, store user = %q", got, store.userID)
	}
	if wantStart := time.Date(2026, 8, 2, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)); !store.todayStart.Equal(wantStart) {
		t.Fatalf("today start = %s, want %s", store.todayStart, wantStart)
	}
	if wantTrendStart := time.Date(2026, 8, 1, 3, 0, 0, 0, time.FixedZone("CST", 8*60*60)); !store.trendStart.Equal(wantTrendStart) {
		t.Fatalf("trend start = %s, want %s", store.trendStart, wantTrendStart)
	}
	if !store.now.Equal(now) {
		t.Fatalf("now = %s, want %s", store.now, now)
	}
}

func TestDashboardRejectsUnknownTimezone(t *testing.T) {
	store := &dashboardStore{}
	service := &Service{store: store, now: time.Now}
	if _, err := service.Dashboard(context.Background(), "user-id", "Mars/Olympus"); err != domain.ErrInvalidInput {
		t.Fatalf("Dashboard() error = %v, want invalid input", err)
	}
	if store.dashboarded {
		t.Fatal("store was called for an invalid timezone")
	}
}

func (s *createPlanStore) AccountByID(context.Context, string) (domain.Account, error) {
	s.accountLookups++
	return s.account, nil
}

func (s *createPlanStore) CreatePlan(_ context.Context, plan domain.Plan, member domain.Member, signals []domain.QuotaSignal, observedAt time.Time, _ domain.AuditEvent) error {
	s.created = true
	s.createdPlan = plan
	s.createdMember = member
	s.createdSignals = signals
	s.bindingObservedAt = observedAt
	return nil
}

func (s *createPlanStore) PlanDetail(context.Context, string, string, time.Time, time.Time) (domain.PlanDetail, error) {
	return s.detail, nil
}

func TestCreatePlanReturnsStoredDetail(t *testing.T) {
	store := &createPlanStore{
		account: domain.Account{
			ID:          "account-id",
			OwnerUserID: "owner-id",
			Email:       "openai@example.com",
			Status:      domain.StatusActive,
		},
		detail: domain.PlanDetail{
			Members: []domain.Member{{Email: "owner@example.com"}},
			Invites: []domain.Invite{},
		},
	}
	manager := testSecurityManager(t)
	now := time.Unix(0, 0)
	store.account.ChatGPTAccountID = "chatgpt"
	store.account.TokenExpiresAt = now.Add(time.Hour)
	store.account.AccessTokenCiphertext, _ = manager.Encrypt("access", []byte("owner-id:chatgpt:access"))
	signals := completeQuotaSignals(now)
	signals[0].AccountUsedMicros = 10_000_000
	service := &Service{store: store, security: manager, quotaProber: &staticQuotaProber{signals: signals}, now: func() time.Time { return now }}

	detail, err := service.CreatePlan(context.Background(), "owner-id", "account-id", "共享方案", domain.AllocationFixed, 6000)
	if err != nil {
		t.Fatal(err)
	}
	if !store.created {
		t.Fatal("plan was not created")
	}
	if got := detail.Members[0].Email; got != "owner@example.com" {
		t.Fatalf("owner email = %q, want stored platform email", got)
	}
	if store.createdPlan.AllocationMode != domain.AllocationFixed || store.createdMember.ShareBasisPoints != 6000 {
		t.Fatalf("created fixed plan = %+v, member = %+v", store.createdPlan, store.createdMember)
	}
	if len(store.createdSignals) != 2 || store.createdSignals[0].AccountUsedMicros != 10_000_000 || !store.bindingObservedAt.Equal(now) {
		t.Fatalf("binding baseline = %+v at %v", store.createdSignals, store.bindingObservedAt)
	}
}

func TestCreateSharedPlanUsesZeroMemberShare(t *testing.T) {
	store := &createPlanStore{
		account: domain.Account{ID: "account-id", OwnerUserID: "owner-id", Status: domain.StatusActive},
		detail:  domain.PlanDetail{},
	}
	manager := testSecurityManager(t)
	now := time.Unix(0, 0)
	store.account.ChatGPTAccountID = "chatgpt"
	store.account.TokenExpiresAt = now.Add(time.Hour)
	store.account.AccessTokenCiphertext, _ = manager.Encrypt("access", []byte("owner-id:chatgpt:access"))
	service := &Service{store: store, security: manager, quotaProber: &staticQuotaProber{signals: completeQuotaSignals(now)}, now: func() time.Time { return now }}

	if _, err := service.CreatePlan(context.Background(), "owner-id", "account-id", "共享方案", domain.AllocationShared, 0); err != nil {
		t.Fatal(err)
	}
	if store.createdPlan.AllocationMode != domain.AllocationShared || store.createdMember.ShareBasisPoints != 0 {
		t.Fatalf("created shared plan = %+v, member = %+v", store.createdPlan, store.createdMember)
	}
}

func TestCreateFixedPlanAllowsZeroOwnerShare(t *testing.T) {
	store := &createPlanStore{
		account: domain.Account{ID: "account-id", OwnerUserID: "owner-id", Status: domain.StatusActive},
		detail:  domain.PlanDetail{},
	}
	manager := testSecurityManager(t)
	now := time.Unix(0, 0)
	store.account.ChatGPTAccountID = "chatgpt"
	store.account.TokenExpiresAt = now.Add(time.Hour)
	store.account.AccessTokenCiphertext, _ = manager.Encrypt("access", []byte("owner-id:chatgpt:access"))
	service := &Service{store: store, security: manager, quotaProber: &staticQuotaProber{signals: completeQuotaSignals(now)}, now: func() time.Time { return now }}

	if _, err := service.CreatePlan(context.Background(), "owner-id", "account-id", "只读方案", domain.AllocationFixed, 0); err != nil {
		t.Fatal(err)
	}
	if store.createdMember.ShareBasisPoints != 0 {
		t.Fatalf("owner share = %d, want 0", store.createdMember.ShareBasisPoints)
	}
}

func TestCreatePlanWithoutAccountSkipsAccountLookup(t *testing.T) {
	store := &createPlanStore{detail: domain.PlanDetail{
		Plan:    domain.Plan{ID: "plan-id", OwnerUserID: "owner-id", Name: "先探索的 Plan", Status: domain.StatusActive, AllocationMode: domain.AllocationFixed},
		Members: []domain.Member{}, Invites: []domain.Invite{}, Applications: []domain.JoinApplication{},
	}}
	service := &Service{store: store, now: func() time.Time { return time.Unix(0, 0) }}

	detail, err := service.CreatePlan(context.Background(), "owner-id", "", "先探索的 Plan", domain.AllocationFixed, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if store.accountLookups != 0 {
		t.Fatalf("account lookups = %d, want 0", store.accountLookups)
	}
	if !store.created || store.createdPlan.AccountID != "" || detail.Account != nil {
		t.Fatalf("unbound plan = %+v, detail account = %+v", store.createdPlan, detail.Account)
	}
}

func TestCreatePlanRejectsInvalidShareForAllocationMode(t *testing.T) {
	tests := []struct {
		name  string
		mode  string
		share int
	}{
		{name: "fixed negative share", mode: domain.AllocationFixed, share: -1},
		{name: "fixed excessive share", mode: domain.AllocationFixed, share: domain.MaxShareBPS + 1},
		{name: "shared with share", mode: domain.AllocationShared, share: 1},
		{name: "unknown mode", mode: "automatic", share: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &createPlanStore{account: domain.Account{ID: "account-id", OwnerUserID: "owner-id", Status: domain.StatusActive}}
			service := &Service{store: store, now: func() time.Time { return time.Unix(0, 0) }}
			if _, err := service.CreatePlan(context.Background(), "owner-id", "account-id", "共享方案", test.mode, test.share); err != domain.ErrInvalidInput {
				t.Fatalf("CreatePlan() error = %v, want invalid input", err)
			}
			if store.created {
				t.Fatal("invalid plan was created")
			}
		})
	}
}

func TestUpdatePlanDescriptionTrimsAndPersistsDescription(t *testing.T) {
	store := &planMetadataStore{}
	service := &Service{store: store, now: func() time.Time { return time.Unix(0, 0) }}

	plan, err := service.UpdatePlanDescription(context.Background(), "owner-id", "plan-id", "  团队项目的 Codex 协作空间  ")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Description != "团队项目的 Codex 协作空间" || store.description != plan.Description {
		t.Fatalf("description = %q, stored = %q", plan.Description, store.description)
	}
	if store.event.Action != "plan.description_updated" || store.event.ResourceID != "plan-id" {
		t.Fatalf("event = %+v", store.event)
	}
}

func TestUpdatePlanDescriptionRejectsMoreThanTwoThousandCharacters(t *testing.T) {
	store := &planMetadataStore{}
	service := &Service{store: store, now: time.Now}

	if _, err := service.UpdatePlanDescription(context.Background(), "owner-id", "plan-id", strings.Repeat("描", 2001)); err != domain.ErrInvalidInput {
		t.Fatalf("UpdatePlanDescription() error = %v, want invalid input", err)
	}
	if store.description != "" {
		t.Fatal("invalid description was persisted")
	}
}

func TestValidUsername(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "alice_01", want: true},
		{value: "张三", want: true},
		{value: "a", want: false},
		{value: "has space", want: false},
		{value: "name!", want: false},
		{value: "abcdefghijklmnopqrstuvwxyz1234567", want: false},
	}
	for _, test := range tests {
		if got := validUsername(test.value); got != test.want {
			t.Errorf("validUsername(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}

func TestUpdateUserAvatarValidatesContentAndSize(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	store := &avatarStore{user: domain.User{ID: "user", AvatarURL: "/api/users/user/avatar?v=1"}}
	service := &Service{store: store, now: func() time.Time { return now }}
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)

	user, err := service.UpdateUserAvatar(context.Background(), "user", png)
	if err != nil {
		t.Fatal(err)
	}
	if user.AvatarURL == "" || store.avatar.MediaType != "image/png" || string(store.avatar.Data) != string(png) || !store.updatedAt.Equal(now) {
		t.Fatalf("stored avatar = %+v, user = %+v, updated at = %s", store.avatar, user, store.updatedAt)
	}
	for _, data := range [][]byte{nil, []byte("not an image"), make([]byte, MaxAvatarBytes+1)} {
		if _, err := service.UpdateUserAvatar(context.Background(), "user", data); err != domain.ErrInvalidInput {
			t.Fatalf("invalid avatar error = %v, want invalid input", err)
		}
	}
}

func TestValidRoutes(t *testing.T) {
	if !validRoutes([]domain.APIKeyRoute{{PlanID: "plan-a", Priority: 1, Enabled: true}, {PlanID: "plan-b", Priority: 2, Enabled: true}}) {
		t.Fatal("valid routes were rejected")
	}
	for _, routes := range [][]domain.APIKeyRoute{
		nil,
		{{PlanID: "", Priority: 1}},
		{{PlanID: "plan-a", Priority: 0}},
		{{PlanID: "plan-a", Priority: 1}, {PlanID: "plan-a", Priority: 2}},
	} {
		if validRoutes(routes) {
			t.Fatalf("invalid routes were accepted: %+v", routes)
		}
	}
}

type gatewayStore struct {
	Store
	routes             domain.GatewayRouteSet
	exhausted          map[string]bool
	accountExhausted   map[string]bool
	memberChecks       []string
	memberCheckKeys    []string
	accountChecks      []string
	touched            []string
	recordedPlanID     string
	recordedAccountID  string
	recordedGeneration int64
	recordedSignals    []domain.QuotaSignal
	recordedAt         time.Time
	recordedMetric     domain.GatewayMetric
}

func (s *gatewayStore) ResolveGatewayRoutes(context.Context, []byte, time.Time) (domain.GatewayRouteSet, error) {
	return s.routes, nil
}

func (s *gatewayStore) MemberQuotaExhausted(_ context.Context, memberID, planID, accountID string, generation int64, _ int, _ time.Time) (bool, error) {
	s.memberChecks = append(s.memberChecks, memberID)
	s.memberCheckKeys = append(s.memberCheckKeys, fmt.Sprintf("%s/%s/%d/%s", planID, accountID, generation, memberID))
	return s.exhausted[memberID], nil
}

func (s *gatewayStore) AccountQuotaExhausted(_ context.Context, accountID string, _ time.Time) (bool, error) {
	s.accountChecks = append(s.accountChecks, accountID)
	return s.accountExhausted[accountID], nil
}

func (s *gatewayStore) TouchAPIKey(_ context.Context, keyID string, _ time.Time) error {
	s.touched = append(s.touched, keyID)
	return nil
}

func (s *gatewayStore) RecordAccountQuotaSignals(_ context.Context, planID, accountID string, generation int64, signals []domain.QuotaSignal, recordedAt time.Time) error {
	s.recordedPlanID = planID
	s.recordedAccountID = accountID
	s.recordedGeneration = generation
	s.recordedSignals = signals
	s.recordedAt = recordedAt
	return nil
}

func (s *gatewayStore) RecordGatewayMetric(_ context.Context, metric domain.GatewayMetric) error {
	s.recordedMetric = metric
	return nil
}

func TestResolveGatewayAccessBalancesByShareUsage(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	store := &gatewayStore{routes: domain.GatewayRouteSet{
		APIKey: domain.APIKey{ID: "key", Strategy: domain.RouteBalanced},
		Candidates: []domain.GatewayCredential{
			testCredential(t, manager, "key", "member-a", "plan-a", 1, 2_000, 1_000, now),
			testCredential(t, manager, "key", "member-b", "plan-b", 2, 5_000, 1_500, now),
		},
	}, exhausted: map[string]bool{}}
	service := &Service{store: store, security: manager, now: func() time.Time { return now }}

	access, err := service.ResolveGatewayAccess(context.Background(), "sk-sharesub-test")
	if err != nil {
		t.Fatal(err)
	}
	if access.Credential.Plan.ID != "plan-b" {
		t.Fatalf("selected plan = %q, want plan-b", access.Credential.Plan.ID)
	}
}

func TestGatewayQuotaSignalsKeepResolvedBindingTuple(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	credential := domain.GatewayCredential{
		Plan:                     domain.Plan{ID: "plan"},
		Account:                  domain.Account{ID: "account"},
		AccountBindingGeneration: 9,
	}
	store := &gatewayStore{}
	service := &Service{store: store, now: func() time.Time { return now }}
	access := GatewayAccess{Credential: credential}
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "10")
	headers.Set("x-codex-primary-reset-after-seconds", "600")
	headers.Set("x-codex-primary-window-minutes", "300")

	for _, record := range []struct {
		name string
		call func() error
	}{
		{name: "success", call: func() error { return service.RecordGatewayUsage(context.Background(), access, headers, now) }},
		{name: "rejection", call: func() error { return service.RecordGatewayAccountQuota(context.Background(), access, headers, now) }},
	} {
		t.Run(record.name, func(t *testing.T) {
			if err := record.call(); err != nil {
				t.Fatal(err)
			}
			if store.recordedPlanID != "plan" || store.recordedAccountID != "account" || store.recordedGeneration != 9 || len(store.recordedSignals) != 1 || !store.recordedAt.Equal(now) {
				t.Fatalf("recorded tuple = %q/%q/%d, signals = %d, at = %s", store.recordedPlanID, store.recordedAccountID, store.recordedGeneration, len(store.recordedSignals), store.recordedAt)
			}
		})
	}
}

func TestRecordGatewayMetricKeepsResolvedBindingAndRequestStart(t *testing.T) {
	requestStartedAt := time.Date(2026, 7, 31, 11, 59, 58, 123, time.UTC)
	serviceNow := requestStartedAt.Add(10 * time.Second)
	store := &gatewayStore{}
	service := &Service{store: store, now: func() time.Time { return serviceNow }}
	access := GatewayAccess{Credential: domain.GatewayCredential{
		APIKeyID:                 "key",
		Plan:                     domain.Plan{ID: "plan"},
		Account:                  domain.Account{ID: "account"},
		Member:                   domain.Member{ID: "member"},
		AccountBindingGeneration: 9,
	}}

	if err := service.RecordGatewayMetric(context.Background(), access, domain.GatewayMetric{
		RequestID: "request",
		CreatedAt: serviceNow.Add(time.Hour),
	}, requestStartedAt); err != nil {
		t.Fatal(err)
	}
	metric := store.recordedMetric
	if metric.APIKeyID != "key" || metric.PlanID != "plan" || metric.AccountID != "account" || metric.MemberID != "member" || metric.AccountBindingGeneration != 9 {
		t.Fatalf("recorded metric binding = key %q, plan %q, account %q, member %q, generation %d", metric.APIKeyID, metric.PlanID, metric.AccountID, metric.MemberID, metric.AccountBindingGeneration)
	}
	if !metric.CreatedAt.Equal(requestStartedAt) {
		t.Fatalf("recorded metric time = %s, want request start %s (service now %s)", metric.CreatedAt, requestStartedAt, serviceNow)
	}
}

func TestResolveGatewayAccessRejectsZeroShareWithoutQuotaLookup(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	store := &gatewayStore{routes: domain.GatewayRouteSet{
		APIKey:     domain.APIKey{ID: "key", Strategy: domain.RoutePriority},
		Candidates: []domain.GatewayCredential{testCredential(t, manager, "key", "member", "plan", 1, 0, 0, now)},
	}, exhausted: map[string]bool{}}
	service := &Service{store: store, security: manager, now: func() time.Time { return now }}

	if _, err := service.ResolveGatewayAccess(context.Background(), "sk-sharesub-test"); err != domain.ErrQuotaExhausted {
		t.Fatalf("ResolveGatewayAccess() error = %v, want quota exhausted", err)
	}
	if len(store.memberChecks) != 0 {
		t.Fatalf("zero-share member triggered quota lookup: %v", store.memberChecks)
	}
}

func TestResolveGatewayAccessFailsOverBeforeUpstreamRequest(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	broken := testCredential(t, manager, "key", "member-a", "plan-a", 1, 5_000, 0, now)
	broken.AccessTokenCiphertext = []byte("invalid")
	store := &gatewayStore{routes: domain.GatewayRouteSet{
		APIKey: domain.APIKey{ID: "key", Strategy: domain.RoutePriority},
		Candidates: []domain.GatewayCredential{
			broken,
			testCredential(t, manager, "key", "member-b", "plan-b", 2, 5_000, 0, now),
		},
	}, exhausted: map[string]bool{}}
	service := &Service{store: store, security: manager, now: func() time.Time { return now }}

	access, err := service.ResolveGatewayAccess(context.Background(), "sk-sharesub-test")
	if err != nil {
		t.Fatal(err)
	}
	if access.Credential.Plan.ID != "plan-b" {
		t.Fatalf("selected plan = %q, want plan-b", access.Credential.Plan.ID)
	}
}

func TestResolveGatewayAccessFailsOverWhenAccountConcurrencyIsFull(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	first := testCredential(t, manager, "key", "member-a", "plan-a", 1, 5_000, 0, now)
	first.Account.MaxConcurrency = 1
	second := testCredential(t, manager, "key", "member-b", "plan-b", 2, 5_000, 0, now)
	traffic := newAccountTrafficController()
	release, err := traffic.acquire(first.Account.ID, first.Account.MaxConcurrency, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	store := &gatewayStore{routes: domain.GatewayRouteSet{
		APIKey:     domain.APIKey{ID: "key", Strategy: domain.RoutePriority},
		Candidates: []domain.GatewayCredential{first, second},
	}, exhausted: map[string]bool{}}
	service := &Service{store: store, security: manager, now: func() time.Time { return now }, traffic: traffic}

	access, err := service.ResolveGatewayAccess(context.Background(), "sk-sharesub-test")
	if err != nil {
		t.Fatal(err)
	}
	if access.Credential.Plan.ID != "plan-b" {
		t.Fatalf("selected plan = %q, want plan-b", access.Credential.Plan.ID)
	}
	if access.Release == nil {
		t.Fatal("selected account concurrency slot was not reserved")
	}
	access.Release()
}

func TestResolveGatewayAccessExcludesPreviouslyRejectedAccount(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	first := testCredential(t, manager, "key", "member-a", "plan-a", 1, 5_000, 0, now)
	second := testCredential(t, manager, "key", "member-b", "plan-b", 2, 5_000, 0, now)
	store := &gatewayStore{routes: domain.GatewayRouteSet{
		APIKey:     domain.APIKey{ID: "key", Strategy: domain.RoutePriority},
		Candidates: []domain.GatewayCredential{first, second},
	}, exhausted: map[string]bool{}}
	service := &Service{store: store, security: manager, now: func() time.Time { return now }}

	access, err := service.ResolveGatewayAccess(context.Background(), "sk-sharesub-test", first.Account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if access.Credential.Account.ID != second.Account.ID {
		t.Fatalf("selected account = %q, want %q", access.Credential.Account.ID, second.Account.ID)
	}
	if _, err := service.ResolveGatewayAccess(context.Background(), "sk-sharesub-test", first.Account.ID, second.Account.ID); err != domain.ErrNoRouteAvailable {
		t.Fatalf("all accounts excluded error = %v, want no route", err)
	}
}

func TestResolveGatewayAccessSharedPlanUsesAccountQuota(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	credential := testCredential(t, manager, "key", "member-a", "plan-a", 1, 0, 0, now)
	credential.Plan.AllocationMode = domain.AllocationShared
	credential.AccountUsageMicros = 75_000_000
	store := &gatewayStore{
		routes: domain.GatewayRouteSet{
			APIKey:     domain.APIKey{ID: "key", Strategy: domain.RoutePriority},
			Candidates: []domain.GatewayCredential{credential},
		},
		exhausted:        map[string]bool{"member-a": true},
		accountExhausted: map[string]bool{},
	}
	service := &Service{store: store, security: manager, now: func() time.Time { return now }}

	access, err := service.ResolveGatewayAccess(context.Background(), "sk-sharesub-test")
	if err != nil {
		t.Fatal(err)
	}
	if access.Credential.Plan.ID != "plan-a" {
		t.Fatalf("selected plan = %q, want plan-a", access.Credential.Plan.ID)
	}
	if len(store.memberChecks) != 0 || len(store.accountChecks) != 1 || store.accountChecks[0] != credential.Account.ID {
		t.Fatalf("member checks = %v, account checks = %v", store.memberChecks, store.accountChecks)
	}
}

func TestResolveGatewayAccessRejectsExhaustedSharedAccount(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	credential := testCredential(t, manager, "key", "member-a", "plan-a", 1, 0, 0, now)
	credential.Plan.AllocationMode = domain.AllocationShared
	store := &gatewayStore{
		routes: domain.GatewayRouteSet{
			APIKey:     domain.APIKey{ID: "key", Strategy: domain.RoutePriority},
			Candidates: []domain.GatewayCredential{credential},
		},
		exhausted:        map[string]bool{},
		accountExhausted: map[string]bool{credential.Account.ID: true},
	}
	service := &Service{store: store, security: manager, now: func() time.Time { return now }}

	if _, err := service.ResolveGatewayAccess(context.Background(), "sk-sharesub-test"); err != domain.ErrQuotaExhausted {
		t.Fatalf("ResolveGatewayAccess() error = %v, want quota exhausted", err)
	}
}

func TestResolveGatewayAccessRejectsExhaustedAccountForFixedPlan(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	credential := testCredential(t, manager, "key", "member-a", "plan-a", 1, 5_000, 0, now)
	store := &gatewayStore{
		routes: domain.GatewayRouteSet{
			APIKey:     domain.APIKey{ID: "key", Strategy: domain.RoutePriority},
			Candidates: []domain.GatewayCredential{credential},
		},
		exhausted:        map[string]bool{},
		accountExhausted: map[string]bool{credential.Account.ID: true},
	}
	service := &Service{store: store, security: manager, now: func() time.Time { return now }}

	if _, err := service.ResolveGatewayAccess(context.Background(), "sk-sharesub-test"); err != domain.ErrQuotaExhausted {
		t.Fatalf("ResolveGatewayAccess() error = %v, want quota exhausted", err)
	}
	if len(store.memberChecks) != 0 {
		t.Fatalf("member quota was checked after account exhaustion: %v", store.memberChecks)
	}
}

func TestResolveGatewayAccessBalancesSharedPlansByAccountUsage(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	first := testCredential(t, manager, "key", "member-a", "plan-a", 1, 0, 0, now)
	first.Plan.AllocationMode = domain.AllocationShared
	first.AccountUsageMicros = 80_000_000
	second := testCredential(t, manager, "key", "member-b", "plan-b", 2, 0, 0, now)
	second.Plan.AllocationMode = domain.AllocationShared
	second.AccountUsageMicros = 25_000_000
	store := &gatewayStore{
		routes: domain.GatewayRouteSet{
			APIKey:     domain.APIKey{ID: "key", Strategy: domain.RouteBalanced},
			Candidates: []domain.GatewayCredential{first, second},
		},
		exhausted:        map[string]bool{},
		accountExhausted: map[string]bool{},
	}
	service := &Service{store: store, security: manager, now: func() time.Time { return now }}

	access, err := service.ResolveGatewayAccess(context.Background(), "sk-sharesub-test")
	if err != nil {
		t.Fatal(err)
	}
	if access.Credential.Plan.ID != "plan-b" {
		t.Fatalf("selected plan = %q, want plan-b", access.Credential.Plan.ID)
	}
}

func testSecurityManager(t *testing.T) *security.Manager {
	t.Helper()
	manager, err := security.New(make([]byte, 32), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func testCredential(t *testing.T, manager *security.Manager, keyID, memberID, planID string, priority, share int, usage int64, now time.Time) domain.GatewayCredential {
	t.Helper()
	ownerID := "owner-" + planID
	accountID := "account-" + planID
	scope := ownerID + ":" + accountID
	ciphertext, err := manager.Encrypt("access-"+planID, []byte(scope+":access"))
	if err != nil {
		t.Fatal(err)
	}
	return domain.GatewayCredential{
		APIKeyID: keyID, RoutePriority: priority, UsageMicros: usage,
		Member:                domain.Member{ID: memberID, ShareBasisPoints: share},
		Plan:                  domain.Plan{ID: planID, AllocationMode: domain.AllocationFixed},
		Account:               domain.Account{ID: accountID, OwnerUserID: ownerID, ChatGPTAccountID: accountID},
		AccessTokenCiphertext: ciphertext,
		TokenExpiresAt:        now.Add(time.Hour),
	}
}
