package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

type createPlanStore struct {
	Store
	account       domain.Account
	detail        domain.PlanDetail
	created       bool
	createdPlan   domain.Plan
	createdMember domain.Member
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
}

func (s *planAccountStore) PlanPerformance(_ context.Context, planID, userID string, startedAt, endedAt time.Time, bucketSize time.Duration) (domain.PlanPerformance, error) {
	s.performancePlanID = planID
	s.performanceUserID = userID
	s.performanceStartedAt = startedAt
	s.performanceEndedAt = endedAt
	s.performanceBucket = bucketSize
	return s.performance, nil
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
	credential domain.PlanQuotaCredential
	updatedAt  time.Time
}

func (s *quotaProbeStore) PlanQuotaCredential(context.Context, string, string) (domain.PlanQuotaCredential, error) {
	return s.credential, nil
}

func (s *quotaProbeStore) PlanQuotaCredentialForMember(context.Context, string, string) (domain.PlanQuotaCredential, error) {
	return s.credential, nil
}

func (s *quotaProbeStore) AccountQuotaUpdatedAt(context.Context, string) (time.Time, error) {
	return s.updatedAt, nil
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

	created, err := service.CreateAPIKey(context.Background(), "owner", "Codex", domain.RouteBalanced, []domain.APIKeyRoute{{PlanID: "plan", Priority: 1, Enabled: true}})
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

func TestPlanMemberSeesHydratedAccountConfiguration(t *testing.T) {
	manager := testSecurityManager(t)
	proxyURL := "socks5://proxy.example:1080"
	ciphertext, err := manager.Encrypt(proxyURL, []byte("owner:chatgpt:proxy"))
	if err != nil {
		t.Fatal(err)
	}
	store := &planAccountStore{detail: domain.PlanDetail{
		Plan: domain.Plan{ID: "plan", OwnerUserID: "owner"},
		Account: domain.Account{
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

func TestPrepareAutomaticPlanQuotaProbeSkipsFreshSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	store := &quotaProbeStore{
		credential: domain.PlanQuotaCredential{AccountID: "account"},
		updatedAt:  now.Add(-automaticQuotaProbeTTL + time.Second),
	}
	service := &Service{store: store, now: func() time.Time { return now }}

	probe, shouldProbe, err := service.PrepareAutomaticPlanQuotaProbe(context.Background(), "owner", "plan")
	if err != nil {
		t.Fatal(err)
	}
	if shouldProbe || probe.AccountID != "account" {
		t.Fatalf("probe = %+v, shouldProbe = %v, want fresh snapshot skip", probe, shouldProbe)
	}
}

func TestPrepareAutomaticPlanQuotaProbePreparesStaleSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	manager := testSecurityManager(t)
	credential := domain.PlanQuotaCredential{
		PlanID:             "plan",
		AccountID:          "account",
		OwnerMemberID:      "member",
		AccountOwnerUserID: "owner",
		ChatGPTAccountID:   "chatgpt",
		TokenExpiresAt:     now.Add(time.Hour),
	}
	var err error
	credential.AccessTokenCiphertext, err = manager.Encrypt("access-token", []byte("owner:chatgpt:access"))
	if err != nil {
		t.Fatal(err)
	}
	store := &quotaProbeStore{credential: credential, updatedAt: now.Add(-automaticQuotaProbeTTL)}
	service := &Service{store: store, security: manager, now: func() time.Time { return now }}

	probe, shouldProbe, err := service.PrepareAutomaticPlanQuotaProbe(context.Background(), "owner", "plan")
	if err != nil {
		t.Fatal(err)
	}
	if !shouldProbe || probe.AccessToken != "access-token" || probe.AccountID != "account" {
		t.Fatalf("probe = %+v, shouldProbe = %v, want prepared stale snapshot probe", probe, shouldProbe)
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
	return s.account, nil
}

func (s *createPlanStore) CreatePlan(_ context.Context, plan domain.Plan, member domain.Member, _ domain.AuditEvent) error {
	s.created = true
	s.createdPlan = plan
	s.createdMember = member
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
	service := &Service{store: store, now: func() time.Time { return time.Unix(0, 0) }}

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
}

func TestCreateSharedPlanUsesZeroMemberShare(t *testing.T) {
	store := &createPlanStore{
		account: domain.Account{ID: "account-id", OwnerUserID: "owner-id", Status: domain.StatusActive},
		detail:  domain.PlanDetail{},
	}
	service := &Service{store: store, now: func() time.Time { return time.Unix(0, 0) }}

	if _, err := service.CreatePlan(context.Background(), "owner-id", "account-id", "共享方案", domain.AllocationShared, 0); err != nil {
		t.Fatal(err)
	}
	if store.createdPlan.AllocationMode != domain.AllocationShared || store.createdMember.ShareBasisPoints != 0 {
		t.Fatalf("created shared plan = %+v, member = %+v", store.createdPlan, store.createdMember)
	}
}

func TestCreatePlanRejectsShareForWrongAllocationMode(t *testing.T) {
	tests := []struct {
		name  string
		mode  string
		share int
	}{
		{name: "fixed without share", mode: domain.AllocationFixed, share: 0},
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
	routes           domain.GatewayRouteSet
	exhausted        map[string]bool
	accountExhausted map[string]bool
	memberChecks     []string
	accountChecks    []string
	touched          []string
}

func (s *gatewayStore) ResolveGatewayRoutes(context.Context, []byte, time.Time) (domain.GatewayRouteSet, error) {
	return s.routes, nil
}

func (s *gatewayStore) MemberQuotaExhausted(_ context.Context, memberID, _ string, _ int, _ time.Time) (bool, error) {
	s.memberChecks = append(s.memberChecks, memberID)
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
