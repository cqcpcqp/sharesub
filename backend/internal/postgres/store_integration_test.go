package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/migrations"
)

func TestMigrationAndPublicPlanWorkflow(t *testing.T) {
	databaseURL := os.Getenv("SHARESUB_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SHARESUB_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	schema := fmt.Sprintf("sharesub_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	}()

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := &Store{pool: pool}

	initial, err := migrations.Files.ReadFile("001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now()); INSERT INTO schema_migrations(name) VALUES('001_initial.sql')`); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	audit := func(id, actor, resourceID string) domain.AuditEvent {
		return domain.AuditEvent{ID: id, ActorUserID: actor, Action: id, ResourceType: "plan", ResourceID: resourceID, Metadata: json.RawMessage(`{}`), CreatedAt: now}
	}
	batch := &pgx.Batch{}
	batch.Queue(`INSERT INTO users(id,email,password_hash,status,created_at,updated_at) VALUES('owner','owner@example.com','hash','active',$1,$1)`, now)
	batch.Queue(`INSERT INTO openai_accounts(id,owner_user_id,email,chatgpt_account_id,plan_type,access_token_ciphertext,refresh_token_ciphertext,token_expires_at,status,created_at,updated_at) VALUES('account','owner','openai@example.com','chatgpt-account','plus',$2,$3,$4,'active',$1,$1)`, now, []byte("access"), []byte("refresh"), now.Add(time.Hour))
	batch.Queue(`INSERT INTO shared_plans(id,owner_user_id,account_id,name,status,created_at,updated_at) VALUES('plan','owner','account','公开测试','active',$1,$1)`, now)
	batch.Queue(`INSERT INTO plan_members(id,plan_id,user_id,role,status,share_basis_points,created_at,updated_at) VALUES('owner-member','plan','owner','owner','active',6000,$1,$1)`, now)
	batch.Queue(`INSERT INTO member_api_keys(id,member_id,user_id,name,key_prefix,key_hash,status,created_at) VALUES('legacy-key','owner-member','owner','旧 Key','sk-sharesub-old',$2,'active',$1)`, now, []byte("hash"))
	if err := pool.SendBatch(ctx, batch).Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var username, migratedPlanID, allocationMode string
	if err := pool.QueryRow(ctx, `SELECT username FROM users WHERE id='owner'`).Scan(&username); err != nil {
		t.Fatal(err)
	}
	if username == "" {
		t.Fatal("migration did not assign a username")
	}
	registered := domain.User{ID: "registered", Username: "registered", Email: "registered@example.com", PasswordHash: "hash", Status: domain.StatusActive, Role: domain.RoleUser, CreatedAt: now}
	acceptance := domain.AgreementAcceptance{UserID: registered.ID, TermsVersion: "2026-08-05", PrivacyPolicyVersion: "2026-08-05", AcceptableUseVersion: "2026-08-05", AcceptedAt: now}
	if err := store.CreateUserWithAgreement(ctx, registered, acceptance); err != nil {
		t.Fatal(err)
	}
	var acceptedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT accepted_at FROM user_agreement_acceptances WHERE user_id=$1 AND terms_version=$2 AND privacy_policy_version=$3 AND acceptable_use_version=$4`, registered.ID, acceptance.TermsVersion, acceptance.PrivacyPolicyVersion, acceptance.AcceptableUseVersion).Scan(&acceptedAt); err != nil {
		t.Fatal(err)
	}
	if !acceptedAt.Equal(now) {
		t.Fatalf("agreement accepted_at = %s, want %s", acceptedAt, now)
	}
	avatarTime := now.Add(time.Second)
	ownerAvatar := domain.UserAvatar{Data: []byte("\x89PNG\r\n\x1a\nowner-avatar"), MediaType: "image/png"}
	updatedOwner, err := store.UpdateUserAvatar(ctx, "owner", ownerAvatar, avatarTime)
	if err != nil {
		t.Fatal(err)
	}
	if updatedOwner.AvatarURL != fmt.Sprintf("/api/users/owner/avatar?v=%d", avatarTime.UnixNano()) {
		t.Fatalf("owner avatar URL = %q", updatedOwner.AvatarURL)
	}
	storedAvatar, err := store.UserAvatar(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if storedAvatar.MediaType != ownerAvatar.MediaType || string(storedAvatar.Data) != string(ownerAvatar.Data) {
		t.Fatalf("stored owner avatar = %+v", storedAvatar)
	}
	if err := pool.QueryRow(ctx, `SELECT plan_id FROM api_key_plans WHERE api_key_id='legacy-key'`).Scan(&migratedPlanID); err != nil {
		t.Fatal(err)
	}
	if migratedPlanID != "plan" {
		t.Fatalf("legacy key route = %q", migratedPlanID)
	}
	if err := pool.QueryRow(ctx, `SELECT allocation_mode FROM shared_plans WHERE id='plan'`).Scan(&allocationMode); err != nil {
		t.Fatal(err)
	}
	if allocationMode != domain.AllocationFixed {
		t.Fatalf("legacy plan allocation mode = %q, want fixed", allocationMode)
	}
	legacyAccount, err := store.AccountByID(ctx, "account")
	if err != nil {
		t.Fatal(err)
	}
	if legacyAccount.Name != "openai@example.com" || legacyAccount.MaxConcurrency != 0 || legacyAccount.RPMLimit != 0 {
		t.Fatalf("migrated account configuration = %+v", legacyAccount)
	}
	configuredAccount, err := store.UpdateAccountConfig(ctx, "owner", domain.Account{
		ID: "account", Name: "团队主账号", Notes: "仅用于 Codex", ProxyURLCiphertext: []byte("encrypted-proxy"),
		MaxConcurrency: 6, RPMLimit: 90, Status: domain.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if configuredAccount.Name != "团队主账号" || configuredAccount.Notes != "仅用于 Codex" || configuredAccount.MaxConcurrency != 6 || configuredAccount.RPMLimit != 90 || string(configuredAccount.ProxyURLCiphertext) != "encrypted-proxy" {
		t.Fatalf("updated account configuration = %+v", configuredAccount)
	}

	if err := store.CreateUser(ctx, domain.User{ID: "applicant", Username: "applicant", Email: "applicant@example.com", PasswordHash: "hash", Status: domain.StatusActive, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateUser(ctx, domain.User{ID: "second", Username: "second", Email: "second@example.com", PasswordHash: "hash", Status: domain.StatusActive, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdatePlanPublication(ctx, "owner", "plan", domain.VisibilityPublic, 1, 3000, audit("publish-plan", "owner", "plan")); err != nil {
		t.Fatal(err)
	}
	publicPlans, err := store.ListPublicPlans(ctx, "applicant")
	if err != nil {
		t.Fatal(err)
	}
	if len(publicPlans) != 1 || publicPlans[0].OwnerAvatarURL != updatedOwner.AvatarURL {
		t.Fatalf("public plan owner avatar = %+v", publicPlans)
	}
	application, err := store.CreateJoinApplication(ctx, domain.JoinApplication{ID: "application", PlanID: "plan", UserID: "applicant", Status: "pending", CreatedAt: now}, audit("apply-plan", "applicant", "plan"))
	if err != nil {
		t.Fatal(err)
	}
	approved, err := store.ReviewJoinApplication(ctx, "owner", application.ID, true, "applicant-member", now, audit("approve-plan", "owner", "plan"))
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != "approved" || approved.MemberID == nil {
		t.Fatalf("approved application = %+v", approved)
	}
	detail, err := store.PlanDetail(ctx, "plan", "applicant", now.Truncate(24*time.Hour), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Members) != 2 || detail.Members[1].ShareBasisPoints != 3000 {
		t.Fatalf("members after approval = %+v", detail.Members)
	}
	if detail.Members[0].AvatarURL != updatedOwner.AvatarURL {
		t.Fatalf("plan owner avatar = %q", detail.Members[0].AvatarURL)
	}
	if detail.Account.Name != "团队主账号" || detail.Account.Notes != "仅用于 Codex" || detail.Account.Email != "openai@example.com" || detail.Account.ChatGPTAccountID != "chatgpt-account" || detail.Account.MaxConcurrency != 6 || detail.Account.RPMLimit != 90 || string(detail.Account.ProxyURLCiphertext) != "encrypted-proxy" {
		t.Fatalf("member-visible account configuration = %+v", detail.Account)
	}
	encodedAccount, err := json.Marshal(detail.Account)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedAccount), "access_token_ciphertext") || strings.Contains(string(encodedAccount), "refresh_token_ciphertext") || strings.Contains(string(encodedAccount), "proxy_url_ciphertext") {
		t.Fatalf("account JSON exposed encrypted credentials: %s", encodedAccount)
	}
	_, err = store.CreateJoinApplication(ctx, domain.JoinApplication{ID: "second-application", PlanID: "plan", UserID: "second", Status: "pending", CreatedAt: now}, audit("second-apply", "second", "plan"))
	if !errors.Is(err, domain.ErrPublicPlanFull) {
		t.Fatalf("second application error = %v, want public plan full", err)
	}
	_, err = store.UpdatePlanPublication(ctx, "owner", "plan", domain.VisibilityPublic, 2, 3000, audit("over-publish", "owner", "plan"))
	if !errors.Is(err, domain.ErrShareExceeded) {
		t.Fatalf("over-allocation error = %v, want share exceeded", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO openai_accounts(id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,access_token_ciphertext,refresh_token_ciphertext,max_concurrency,rpm_limit,token_expires_at,status,created_at,updated_at) VALUES('shared-account','owner','共享账号','','shared@example.com','shared-chatgpt','plus',$2,$3,0,0,$4,'active',$1,$1)`, now, []byte("access"), []byte("refresh"), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sharedPlan := domain.Plan{ID: "shared-plan", OwnerUserID: "owner", AccountID: "shared-account", Name: "共享额度测试", Status: domain.StatusActive, Visibility: domain.VisibilityPrivate, AllocationMode: domain.AllocationShared, CreatedAt: now}
	sharedOwner := domain.Member{ID: "shared-owner-member", PlanID: sharedPlan.ID, UserID: "owner", Role: domain.RoleOwner, Status: domain.StatusActive, ShareBasisPoints: 0, CreatedAt: now}
	if err := store.CreatePlan(ctx, sharedPlan, sharedOwner, audit("create-shared", "owner", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdatePlanPublication(ctx, "owner", sharedPlan.ID, domain.VisibilityPublic, 2, 0, audit("publish-shared", "owner", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	sharedApplication, err := store.CreateJoinApplication(ctx, domain.JoinApplication{ID: "shared-application", PlanID: sharedPlan.ID, UserID: "second", Status: "pending", CreatedAt: now}, audit("apply-shared", "second", sharedPlan.ID))
	if err != nil {
		t.Fatal(err)
	}
	sharedApproved, err := store.ReviewJoinApplication(ctx, "owner", sharedApplication.ID, true, "shared-member", now, audit("approve-shared", "owner", sharedPlan.ID))
	if err != nil {
		t.Fatal(err)
	}
	if sharedApproved.Status != "approved" {
		t.Fatalf("shared application status = %q, want approved", sharedApproved.Status)
	}
	sharedDetail, err := store.PlanDetail(ctx, sharedPlan.ID, "second", now.Truncate(24*time.Hour), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if sharedDetail.Plan.AllocationMode != domain.AllocationShared || sharedDetail.Members[1].ShareBasisPoints != 0 {
		t.Fatalf("shared plan detail = %+v", sharedDetail)
	}
	if _, err := store.UpdateMemberShare(ctx, sharedPlan.ID, "owner", "shared-member", 0, audit("share-shared", "owner", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateMemberShare(ctx, sharedPlan.ID, "owner", "shared-member", 1, audit("invalid-share", "owner", sharedPlan.ID)); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("shared member nonzero share error = %v, want invalid input", err)
	}
	sharedInvite := domain.Invite{ID: "shared-invite", PlanID: sharedPlan.ID, TokenHash: []byte("shared-token"), ShareBasisPoints: 0, Status: "pending", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if err := store.CreateInvite(ctx, sharedPlan.ID, "owner", sharedInvite, audit("invite-shared", "owner", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	sharedInvite.ID = "invalid-shared-invite"
	sharedInvite.TokenHash = []byte("invalid-shared-token")
	sharedInvite.ShareBasisPoints = 1
	if err := store.CreateInvite(ctx, sharedPlan.ID, "owner", sharedInvite, audit("invalid-invite", "owner", sharedPlan.ID)); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("shared invite nonzero share error = %v, want invalid input", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO account_quota_snapshots(account_id,window_type,window_start,reset_at,used_micros,updated_at) VALUES('account','5h',$1,$2,100000000,$1)`, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	exhausted, err := store.AccountQuotaExhausted(ctx, "account", now)
	if err != nil {
		t.Fatal(err)
	}
	if !exhausted {
		t.Fatal("shared account at 100% was not exhausted")
	}

	if err := store.RecordGatewayMetric(ctx, domain.GatewayMetric{
		RequestID: "applicant-request", APIKeyID: "legacy-key", PlanID: "plan", AccountID: "account", MemberID: "applicant-member",
		Model: "gpt-5.6-sol", RequestedModel: "gpt-5.6-sol", UpstreamModel: "gpt-5.6-sol", BillingModel: "gpt-5.6-sol", AccountCostMicros: 125,
		StatusCode: http.StatusOK, TTFT: 120 * time.Millisecond, Duration: 850 * time.Millisecond,
		TokenUsage: domain.TokenUsage{InputTokens: 1200, OutputTokens: 300, CachedTokens: 400, CacheCreationTokens: 50, ImageInputTokens: 20, ImageOutputTokens: 30, ImageCount: 1}, WebSearchCalls: 2, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordGatewayMetric(ctx, domain.GatewayMetric{
		RequestID: "applicant-request", APIKeyID: "legacy-key", PlanID: "plan", AccountID: "account", MemberID: "applicant-member",
		Model: "gpt-5.6-sol", RequestedModel: "gpt-5.6-sol", UpstreamModel: "gpt-5.6-sol", BillingModel: "gpt-5.6-sol",
		StatusCode: http.StatusOK, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	var idempotentMetricCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM gateway_request_metrics WHERE request_id='applicant-request' AND api_key_id='legacy-key' AND account_id='account'`).Scan(&idempotentMetricCount); err != nil || idempotentMetricCount != 1 {
		t.Fatalf("idempotent metric count = %d, error = %v", idempotentMetricCount, err)
	}
	if err := store.RecordGatewayMetric(ctx, domain.GatewayMetric{
		RequestID: "owner-request", APIKeyID: "legacy-key", PlanID: "plan", AccountID: "account", MemberID: "owner-member",
		Model: "gpt-5.6-terra", RequestedModel: "gpt-5.6-terra", UpstreamModel: "gpt-5.6-terra", BillingModel: "gpt-5.6-terra", AccountCostMicros: 850,
		StatusCode: http.StatusInternalServerError, TTFT: 300 * time.Millisecond, Duration: 1300 * time.Millisecond,
		TokenUsage: domain.TokenUsage{InputTokens: 9000, OutputTokens: 1000, CachedTokens: 2000}, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	dashboard, err := store.Dashboard(ctx, "applicant", now.Add(-12*time.Hour), now.Add(-23*time.Hour), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.TodayTokens.TotalTokens != 1500 || dashboard.TotalTokens.TotalTokens != 1500 || dashboard.TodayTokens.CachedTokens != 400 {
		t.Fatalf("dashboard token totals = %+v / %+v", dashboard.TodayTokens, dashboard.TotalTokens)
	}
	if dashboard.TodayTokens.CacheCreationTokens != 50 || dashboard.TodayTokens.ImageInputTokens != 20 || dashboard.TodayTokens.ImageOutputTokens != 30 || dashboard.TodayTokens.ImageCount != 1 {
		t.Fatalf("dashboard detailed token totals = %+v", dashboard.TodayTokens)
	}
	if dashboard.Performance.RequestsToday != 1 || dashboard.Performance.SuccessRate != 100 || dashboard.Performance.ActivePlans != 1 {
		t.Fatalf("dashboard performance = %+v", dashboard.Performance)
	}
	if len(dashboard.Trend) != 24 || dashboard.Trend[23].InputTokens != 1200 || dashboard.Trend[23].OutputTokens != 300 {
		t.Fatalf("dashboard trend = %+v", dashboard.Trend)
	}
	usageDetail, err := store.PlanDetail(ctx, "plan", "applicant", now.Truncate(24*time.Hour), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(usageDetail.Insights.ModelUsage) != 2 || usageDetail.Insights.ModelUsage[0].Model != "gpt-5.6-terra" || usageDetail.Insights.ModelUsage[1].EstimatedCostMicros != 125 {
		t.Fatalf("plan model usage = %+v", usageDetail.Insights.ModelUsage)
	}
	if len(usageDetail.Insights.TokenTrend) != 24 || usageDetail.Insights.TokenTrend[23].InputTokens != 10200 || usageDetail.Insights.TokenTrend[23].OutputTokens != 1300 {
		t.Fatalf("plan token trend = %+v", usageDetail.Insights.TokenTrend)
	}
	if len(usageDetail.Insights.RecentUsage) != 2 || usageDetail.Insights.RecentUsage[0].MemberID != "owner-member" || len(usageDetail.Insights.RecentUsage[0].Trend) != 24 {
		t.Fatalf("plan recent usage = %+v", usageDetail.Insights.RecentUsage)
	}
	adminOverview, err := store.AdminOverview(ctx, now.Add(-24*time.Hour))
	if err != nil || adminOverview.UserCount != 3 || adminOverview.Requests24H != 2 || adminOverview.Tokens24H != 11500 {
		t.Fatalf("admin overview = %+v, error = %v", adminOverview, err)
	}
	adminUsers, err := store.AdminListUsers(ctx)
	if err != nil || len(adminUsers) != 3 {
		t.Fatalf("admin users = %+v, error = %v", adminUsers, err)
	}
	adminAccounts, err := store.AdminListAccounts(ctx)
	if err != nil || len(adminAccounts) != 2 {
		t.Fatalf("admin accounts = %+v, error = %v", adminAccounts, err)
	}
	adminPlans, err := store.AdminListPlans(ctx, now.Add(-24*time.Hour))
	var adminPlanTokens int64
	for _, item := range adminPlans {
		if item.ID == "plan" {
			adminPlanTokens = item.TotalTokens24H
		}
	}
	if err != nil || len(adminPlans) != 2 || adminPlanTokens != 11500 {
		t.Fatalf("admin plans = %+v, error = %v", adminPlans, err)
	}
	adminKeys, err := store.AdminListAPIKeys(ctx)
	if err != nil || len(adminKeys) != 1 || adminKeys[0].KeyPrefix != "sk-sharesub-old" {
		t.Fatalf("admin keys = %+v, error = %v", adminKeys, err)
	}

	if err := store.CreateUser(ctx, domain.User{ID: "invitee", Username: "invitee", Email: "invitee@example.com", PasswordHash: "hash", Status: domain.StatusActive, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	pendingInvite := domain.Invite{ID: "pending-lifecycle-invite", PlanID: sharedPlan.ID, TokenHash: []byte("pending-lifecycle-token"), ShareBasisPoints: 0, Status: "pending", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if err := store.CreateInvite(ctx, sharedPlan.ID, "owner", pendingInvite, audit("create-pending-lifecycle", "owner", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevokeInvite(ctx, sharedPlan.ID, "owner", pendingInvite.ID, audit("revoke-pending-lifecycle", "owner", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevokeInvite(ctx, sharedPlan.ID, "owner", "shared-invite", audit("revoke-shared-invite", "owner", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}

	lifecycleInvite := domain.Invite{ID: "lifecycle-invite", PlanID: sharedPlan.ID, TokenHash: []byte("lifecycle-token"), ShareBasisPoints: 0, Status: "pending", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if err := store.CreateInvite(ctx, sharedPlan.ID, "owner", lifecycleInvite, audit("create-lifecycle-invite", "owner", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	preview, err := store.InvitePreview(ctx, lifecycleInvite.TokenHash, now)
	if err != nil {
		t.Fatal(err)
	}
	if preview.PlanID != sharedPlan.ID || preview.OwnerUsername != "user_owner" {
		t.Fatalf("invite preview = %+v", preview)
	}
	invitee := domain.User{ID: "invitee", Username: "invitee", Email: "invitee@example.com", Status: domain.StatusActive}
	joined, err := store.AcceptInvite(ctx, lifecycleInvite.TokenHash, invitee, "invitee-member", now, audit("accept-lifecycle-invite", "invitee", sharedPlan.ID))
	if err != nil {
		t.Fatal(err)
	}
	if joined.ID != "invitee-member" {
		t.Fatalf("joined member = %+v", joined)
	}
	if _, err := store.AcceptInvite(ctx, lifecycleInvite.TokenHash, domain.User{ID: "second", Username: "second", Email: "second@example.com", Status: domain.StatusActive}, "second-invite-member", now, audit("accept-used-invite", "second", sharedPlan.ID)); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second invite acceptance error = %v, want conflict", err)
	}
	inviteeKey := domain.APIKey{ID: "invitee-key", UserID: invitee.ID, Name: "邀请成员 Key", KeyPrefix: "sk-sharesub-invitee", KeyHash: []byte("invitee-key-hash"), Strategy: domain.RoutePriority, Status: domain.StatusActive, CreatedAt: now}
	if err := store.CreateAPIKey(ctx, inviteeKey, []domain.APIKeyRoute{{PlanID: sharedPlan.ID, Priority: 1, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := store.RemovePlanMember(ctx, sharedPlan.ID, invitee.ID, joined.ID, audit("invitee-left", invitee.ID, sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	var routeEnabled bool
	if err := pool.QueryRow(ctx, `SELECT enabled FROM api_key_plans WHERE api_key_id=$1 AND plan_id=$2`, inviteeKey.ID, sharedPlan.ID).Scan(&routeEnabled); err != nil {
		t.Fatal(err)
	}
	if routeEnabled {
		t.Fatal("member route remained enabled after leaving")
	}
	rejoinInvite := domain.Invite{ID: "rejoin-invite", PlanID: sharedPlan.ID, TokenHash: []byte("rejoin-token"), ShareBasisPoints: 0, Status: "pending", ExpiresAt: now.Add(2 * time.Hour), CreatedAt: now.Add(time.Minute)}
	if err := store.CreateInvite(ctx, sharedPlan.ID, "owner", rejoinInvite, audit("create-rejoin", "owner", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	rejoined, err := store.AcceptInvite(ctx, rejoinInvite.TokenHash, invitee, "unused-new-member-id", now.Add(time.Minute), audit("accept-rejoin", invitee.ID, sharedPlan.ID))
	if err != nil {
		t.Fatal(err)
	}
	if rejoined.ID != joined.ID {
		t.Fatalf("reactivated member id = %q, want %q", rejoined.ID, joined.ID)
	}
	if err := pool.QueryRow(ctx, `SELECT enabled FROM api_key_plans WHERE api_key_id=$1 AND plan_id=$2`, inviteeKey.ID, sharedPlan.ID).Scan(&routeEnabled); err != nil {
		t.Fatal(err)
	}
	if routeEnabled {
		t.Fatal("old API route was silently enabled after rejoining")
	}
	if err := store.RemovePlanMember(ctx, sharedPlan.ID, "owner", joined.ID, audit("owner-removed-invitee", "owner", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	if err := store.RemovePlanMember(ctx, sharedPlan.ID, "owner", "shared-owner-member", audit("remove-owner-invalid", "owner", sharedPlan.ID)); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("remove Plan owner error = %v, want conflict", err)
	}

	duplicatePlan := domain.Plan{ID: "duplicate-account-plan", OwnerUserID: "owner", AccountID: sharedPlan.AccountID, Name: "重复账号", Status: domain.StatusActive, Visibility: domain.VisibilityPrivate, AllocationMode: domain.AllocationShared, CreatedAt: now}
	duplicateOwner := domain.Member{ID: "duplicate-owner", PlanID: duplicatePlan.ID, UserID: "owner", Role: domain.RoleOwner, Status: domain.StatusActive, CreatedAt: now}
	if err := store.CreatePlan(ctx, duplicatePlan, duplicateOwner, audit("duplicate-account-plan", "owner", duplicatePlan.ID)); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second Plan for account error = %v, want conflict", err)
	}

	baselineSignal := domain.QuotaSignal{WindowType: domain.Window5H, WindowStart: now, ResetAt: now.Add(5 * time.Hour), AccountUsedMicros: 0}
	if err := store.RecordQuotaSignals(ctx, sharedPlan.AccountID, "shared-member", []domain.QuotaSignal{baselineSignal}, "shared-quota-baseline", now); err != nil {
		t.Fatal(err)
	}
	signal := domain.QuotaSignal{WindowType: domain.Window5H, WindowStart: now, ResetAt: now.Add(5 * time.Hour), AccountUsedMicros: 100_000_000}
	if err := store.RecordQuotaSignals(ctx, sharedPlan.AccountID, "shared-member", []domain.QuotaSignal{signal}, "shared-quota-request", now); err != nil {
		t.Fatal(err)
	}
	if exhausted, err := store.MemberQuotaExhausted(ctx, "shared-member", sharedPlan.AccountID, 10000, now); err != nil || !exhausted {
		t.Fatalf("old account member quota exhausted = %v, %v", exhausted, err)
	}

	transferred, err := store.TransferPlanOwnership(ctx, sharedPlan.ID, "owner", "shared-member", audit("transfer-shared", "owner", sharedPlan.ID))
	if err != nil {
		t.Fatal(err)
	}
	if transferred.OwnerUserID != "second" {
		t.Fatalf("transferred Plan = %+v", transferred)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO openai_accounts(id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,access_token_ciphertext,refresh_token_ciphertext,max_concurrency,rpm_limit,token_expires_at,status,created_at,updated_at) VALUES('second-account','second','接管账号','','second-openai@example.com','second-chatgpt','plus',$2,$3,0,0,$4,'active',$1,$1)`, now, []byte("access"), []byte("refresh"), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rebound, err := store.RebindPlanAccount(ctx, sharedPlan.ID, "second", "second-account", audit("rebind-shared", "second", sharedPlan.ID))
	if err != nil {
		t.Fatal(err)
	}
	if rebound.AccountID != "second-account" {
		t.Fatalf("rebound Plan = %+v", rebound)
	}
	if exhausted, err := store.MemberQuotaExhausted(ctx, "shared-member", "second-account", 10000, now); err != nil || exhausted {
		t.Fatalf("new account inherited old quota: exhausted=%v err=%v", exhausted, err)
	}
	if _, err := store.PlanQuotaCredential(ctx, sharedPlan.ID, "second"); err != nil {
		t.Fatal(err)
	}
	if credential, err := store.PlanQuotaCredentialForMember(ctx, sharedPlan.ID, "owner"); err != nil {
		t.Fatal(err)
	} else if credential.OwnerMemberID != "shared-member" {
		t.Fatalf("member-triggered quota credential owner = %q, want shared-member", credential.OwnerMemberID)
	}
	events, err := store.ListPlanAuditEvents(ctx, sharedPlan.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 6 {
		t.Fatalf("Plan audit events = %d, want lifecycle history", len(events))
	}
	notifications, err := store.ListNotifications(ctx, "second")
	if err != nil {
		t.Fatal(err)
	}
	if notifications.UnreadCount == 0 || len(notifications.Items) == 0 {
		t.Fatalf("notifications = %+v", notifications)
	}
	if _, err := store.ReadAllNotifications(ctx, "second", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if _, err := store.UpdatePlanStatus(ctx, sharedPlan.ID, "owner", domain.StatusArchived, audit("archive-forbidden", "owner", sharedPlan.ID)); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("former owner archive error = %v, want forbidden", err)
	}
	if _, err := store.UpdatePlanStatus(ctx, sharedPlan.ID, "second", domain.StatusArchived, audit("archive-shared", "second", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PlanQuotaCredential(ctx, sharedPlan.ID, "second"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("archived Plan quota credential error = %v, want not found", err)
	}
	if _, err := store.PlanQuotaCredentialForMember(ctx, sharedPlan.ID, "owner"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("archived Plan member quota credential error = %v, want not found", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO shared_plans(id,owner_user_id,account_id,name,status,visibility,allocation_mode,archived_at,created_at,updated_at) VALUES('legacy-duplicate-plan','second','second-account','旧归档 Plan','archived','private','shared',$1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO plan_members(id,plan_id,user_id,role,status,share_basis_points,created_at,updated_at) VALUES('legacy-duplicate-owner','legacy-duplicate-plan','second','owner','active',0,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdatePlanStatus(ctx, sharedPlan.ID, "second", domain.StatusActive, audit("restore-duplicate-account", "second", sharedPlan.ID)); !errors.Is(err, domain.ErrAccountAlreadyBound) {
		t.Fatalf("restore duplicate account Plan error = %v, want account already bound", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM shared_plans WHERE id='legacy-duplicate-plan'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdatePlanStatus(ctx, sharedPlan.ID, "second", domain.StatusActive, audit("restore-shared", "second", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdatePlanStatus(ctx, sharedPlan.ID, "second", domain.StatusArchived, audit("archive-shared-final", "second", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	if err := store.DeletePlan(ctx, sharedPlan.ID, "second", audit("delete-shared", "second", sharedPlan.ID)); err != nil {
		t.Fatal(err)
	}
	var deleteAuditExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_events WHERE id='delete-shared')`).Scan(&deleteAuditExists); err != nil || !deleteAuditExists {
		t.Fatalf("delete audit exists = %v, %v", deleteAuditExists, err)
	}

	cleanupNow := now.Add(91 * 24 * time.Hour)
	cleaned, err := store.CleanupResources(ctx, cleanupNow, RetentionPolicy{
		GatewayMetrics: 90 * 24 * time.Hour, QuotaEvents: 90 * 24 * time.Hour,
		AuditEvents: 365 * 24 * time.Hour, ReadNotifications: 90 * 24 * time.Hour,
		TerminalRecords: 90 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleaned.GatewayMetrics != 2 {
		t.Fatalf("cleaned gateway metrics = %d, want 2", cleaned.GatewayMetrics)
	}
	dashboard, err = store.Dashboard(ctx, "applicant", cleanupNow.Add(-12*time.Hour), cleanupNow.Add(-23*time.Hour), cleanupNow)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.TodayTokens.TotalTokens != 0 || dashboard.TotalTokens.TotalTokens != 1500 {
		t.Fatalf("rolled-up dashboard totals = today %+v, total %+v", dashboard.TodayTokens, dashboard.TotalTokens)
	}
	if dashboard.TotalTokens.CacheCreationTokens != 50 || dashboard.TotalTokens.ImageCount != 1 {
		t.Fatalf("rolled-up detailed dashboard totals = %+v", dashboard.TotalTokens)
	}
	ranking, err := store.memberUsageRanking(ctx, "plan", now.Add(-time.Hour), cleanupNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranking) != 2 || ranking[0].TokenUsage.TotalTokens != 10_000 || ranking[1].TokenUsage.TotalTokens != 1500 {
		t.Fatalf("rolled-up member ranking = %+v", ranking)
	}
	bootstrapUser := domain.User{ID: "bootstrap-admin", Username: "admin", Email: "admin@underelay.com", PasswordHash: "bootstrap-hash", Status: domain.StatusActive, Role: domain.RoleAdmin, MustChangePassword: true, CreatedAt: cleanupNow}
	created, err := store.EnsureBootstrapAdmin(ctx, bootstrapUser)
	if err != nil || !created {
		t.Fatalf("create bootstrap admin = %t, error = %v", created, err)
	}
	created, err = store.EnsureBootstrapAdmin(ctx, domain.User{ID: "second-bootstrap", Username: "admin2", Email: "second-admin@example.com", PasswordHash: "other-hash", Status: domain.StatusActive, Role: domain.RoleAdmin, MustChangePassword: true, CreatedAt: cleanupNow})
	if err != nil || created {
		t.Fatalf("repeat bootstrap admin = %t, error = %v", created, err)
	}
	currentSessionHash := []byte("current-session-hash")
	if err := store.CreateSession(ctx, "bootstrap-current-session", bootstrapUser.ID, currentSessionHash, cleanupNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, "bootstrap-other-session", bootstrapUser.ID, []byte("other-session-hash"), cleanupNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	changedAdmin, err := store.UpdatePassword(ctx, bootstrapUser.ID, "changed-hash", false, currentSessionHash)
	if err != nil || changedAdmin.MustChangePassword || changedAdmin.PasswordHash != "changed-hash" {
		t.Fatalf("change bootstrap password = %+v, error = %v", changedAdmin, err)
	}
	var remainingSessions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id=$1`, bootstrapUser.ID).Scan(&remainingSessions); err != nil || remainingSessions != 1 {
		t.Fatalf("remaining bootstrap sessions = %d, error = %v", remainingSessions, err)
	}
	var remainingSessionHash []byte
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM user_sessions WHERE user_id=$1`, bootstrapUser.ID).Scan(&remainingSessionHash); err != nil || !bytes.Equal(remainingSessionHash, currentSessionHash) {
		t.Fatalf("remaining bootstrap session hash = %q, error = %v", remainingSessionHash, err)
	}
	resetAdmin, err := store.ResetAdminPassword(ctx, bootstrapUser.Email, "reset-hash")
	if err != nil || resetAdmin.Role != domain.RoleAdmin || !resetAdmin.MustChangePassword || resetAdmin.PasswordHash != "reset-hash" {
		t.Fatalf("reset bootstrap admin = %+v, error = %v", resetAdmin, err)
	}
}
