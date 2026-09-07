package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func membershipTestStore(t *testing.T) *Store {
	t.Helper()
	databaseURL := os.Getenv("SHARESUB_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SHARESUB_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("membership_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	store := &Store{pool: pool}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO users(id,username,email,password_hash,status,role) VALUES('owner','owner','owner@example.test','hash','active','user'),('member','member','member@example.test','hash','active','user'),('new','new','new@example.test','hash','active','user'),('admin','admin','admin@example.test','hash','active','admin')`)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func membershipAudit(id string, now time.Time) domain.AuditEvent {
	return domain.AuditEvent{ID: id, ActorUserID: "admin", Action: "membership.test", ResourceType: "membership", ResourceID: "member", CreatedAt: now}
}

func TestMembershipActivationSerializesPlanEligibility(t *testing.T) {
	store := membershipTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `LOCK TABLE shared_plans IN SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	result := make(chan error, 1)
	go func() {
		result <- store.CreatePlan(ctx, domain.Plan{ID: "rollout-race", OwnerUserID: "new", Name: "rollout-race", Status: domain.StatusActive, Visibility: domain.VisibilityPrivate, AllocationMode: domain.AllocationShared, CreatedAt: now}, domain.Member{ID: "rollout-owner", PlanID: "rollout-race", UserID: "new", Role: domain.RoleOwner, Status: domain.StatusActive, CreatedAt: now}, nil, time.Time{}, membershipAudit("rollout-create", now))
	}()
	for {
		var waiting bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='shared_plans'::regclass AND NOT granted)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("creation did not wait for rollout: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := activateMemberships(ctx, tx, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, domain.ErrSVIPRequired) {
		t.Fatalf("creation bypassed activated membership gate: %v", err)
	}
}
func enableMembershipTest(t *testing.T, store *Store, now time.Time) {
	t.Helper()
	if err := store.SavePaymentSettings(context.Background(), domain.PaymentSettings{BaseURL: "https://pay.example.test", PID: "merchant", KeyCiphertext: []byte("encrypted"), Enabled: true}, membershipAudit("enable", now)); err != nil {
		t.Fatal(err)
	}
}
func createMembershipTestPlan(store *Store, id, owner string, now time.Time) error {
	return store.CreatePlan(context.Background(), domain.Plan{ID: id, OwnerUserID: owner, Name: id, Status: domain.StatusActive, Visibility: domain.VisibilityPrivate, AllocationMode: domain.AllocationShared, CreatedAt: now}, domain.Member{ID: "owner-" + id, PlanID: id, UserID: owner, Role: domain.RoleOwner, Status: domain.StatusActive, CreatedAt: now}, nil, time.Time{}, membershipAudit("create-"+id, now))
}
func TestMembershipRolloutOwnershipAndRouting(t *testing.T) {
	store := membershipTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, id := range []string{"first", "second", "third"} {
		if err := createMembershipTestPlan(store, id, "owner", now); err != nil {
			t.Fatal(err)
		}
	}
	_, err := store.pool.Exec(ctx, `INSERT INTO plan_members(id,plan_id,user_id,role,status,share_basis_points) VALUES('member-first','first','member','member','active',0),('member-second','second','member','member','active',0);
	INSERT INTO openai_accounts(id,owner_user_id,name,email,chatgpt_account_id,plan_type,access_token_ciphertext,refresh_token_ciphertext,token_expires_at,status) VALUES('account','owner','account','account@example.test','account','plus','access','refresh',now()+INTERVAL '1 day','active');
	UPDATE shared_plans SET account_id='account',account_bound_at=now() WHERE id='first';
	INSERT INTO api_keys(id,user_id,name,key_prefix,key_hash,status,strategy) VALUES('member-key','member','key','prefix','member-hash','active','priority'),('owner-key','owner','key','prefix2','owner-hash','active','priority');
	INSERT INTO api_key_plans(api_key_id,plan_id,priority,enabled) VALUES('member-key','first',1,true),('owner-key','first',1,true);`)
	if err != nil {
		t.Fatal(err)
	}
	enableMembershipTest(t, store, now)
	owner, err := store.Membership(ctx, "owner", now)
	if err != nil || owner.Tier != "svip" || owner.OwnerLimit != 3 || !owner.Active || owner.OwnedPlans != 3 {
		t.Fatal(owner, err)
	}
	member, err := store.Membership(ctx, "member", now)
	if err != nil || member.Tier != "vip" || !member.Active {
		t.Fatal(member, err)
	}
	newUser, err := store.Membership(ctx, "new", now)
	if err != nil || newUser.Active {
		t.Fatal("unaffiliated user received gift", newUser, err)
	}
	if err = createMembershipTestPlan(store, "fourth", "owner", now); !errors.Is(err, domain.ErrOwnerLimit) {
		t.Fatal("owner limit bypass", err)
	}
	if err = createMembershipTestPlan(store, "member-plan", "member", now); !errors.Is(err, domain.ErrSVIPRequired) {
		t.Fatal("VIP created Plan", err)
	}
	if _, err = store.TransferPlanOwnership(ctx, "second", "owner", "member-second", membershipAudit("transfer-denied", now)); !errors.Is(err, domain.ErrSVIPRequired) {
		t.Fatal("transfer bypass", err)
	}
	end := now.Add(-time.Hour)
	if err = store.AdjustMembership(ctx, "owner", domain.MembershipAdjustment{Tier: "svip", ExpiresAt: &end, OwnerLimitOverride: owner.OwnerLimitOverride, Revision: owner.Revision}, membershipAudit("expire-owner", now)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ResolveGatewayRoutes(ctx, []byte("owner-hash"), now); !errors.Is(err, domain.ErrMembershipRequired) {
		t.Fatal("expired owner used gateway", err)
	}
	routes, err := store.ResolveGatewayRoutes(ctx, []byte("member-hash"), now)
	if err != nil || len(routes.Candidates) != 1 {
		t.Fatal("owner expiry stopped member", routes, err)
	}
	if _, err = store.ResolveGatewayRoutes(ctx, []byte("member-hash"), *member.ExpiresAt); !errors.Is(err, domain.ErrMembershipRequired) {
		t.Fatal("expiry boundary bypass", err)
	}
	if _, err = store.RenamePlan(ctx, "first", "owner", "renamed", membershipAudit("rename", now)); err != nil {
		t.Fatal("maintenance blocked", err)
	}
	settings, err := store.PaymentSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.Enabled = false
	if err = store.SavePaymentSettings(ctx, settings, membershipAudit("disable", now)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ResolveGatewayRoutes(ctx, []byte("owner-hash"), now); !errors.Is(err, domain.ErrMembershipRequired) {
		t.Fatal("collection switch bypass", err)
	}
	settings, err = store.PaymentSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.Enabled = true
	if err = store.SavePaymentSettings(ctx, settings, membershipAudit("reenable", now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	owner, err = store.Membership(ctx, "owner", now)
	if err != nil || owner.Active {
		t.Fatal("repeated gift", owner, err)
	}
}

func TestMembershipPaymentsAndUpgrade(t *testing.T) {
	store := membershipTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	enableMembershipTest(t, store, now)
	order := func(id, product string, at time.Time) (domain.MembershipOrder, error) {
		return store.CreateMembershipOrder(ctx, domain.MembershipOrder{ID: id, UserID: "new", Product: product, AmountCents: 1, ProviderPID: "merchant", ProviderBaseURL: "https://pay.example.test", ProviderKeyCiphertext: []byte("encrypted"), PaymentMethod: "alipay", CreatedAt: at, ExpiresAt: at.Add(30 * time.Minute)})
	}
	first, err := order("first", "vip", now)
	if err != nil || first.AmountCents != 990 {
		t.Fatal(first, err)
	}
	reused, err := order("duplicate", "vip", now)
	if err != nil || reused.ID != "first" {
		t.Fatal(reused, err)
	}
	if _, err = order("conflict", "svip", now); !errors.Is(err, domain.ErrPendingMembershipOrder) {
		t.Fatal(err)
	}
	if err = store.ConfirmMembershipPayment(ctx, "first", "trade1", "merchant", 1, now); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for index := 0; index < 5; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := store.ConfirmMembershipPayment(ctx, "first", "trade1", "merchant", 990, now); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	member, err := store.Membership(ctx, "new", now)
	if err != nil || member.Tier != "vip" || !member.ExpiresAt.Equal(now.Add(720*time.Hour)) {
		t.Fatal("duplicate fulfillment", member, err)
	}
	if _, err = order("invalid-svip", "svip", now); !errors.Is(err, domain.ErrMembershipProduct) {
		t.Fatal(err)
	}
	upgrade, err := order("upgrade", "upgrade", now)
	if err != nil || upgrade.AmountCents != 1000 || !upgrade.UpgradeExpiresAt.Equal(*member.ExpiresAt) {
		t.Fatal(upgrade, err)
	}
	if err = store.ConfirmMembershipPayment(ctx, "upgrade", "trade2", "merchant", 1000, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	upgraded, err := store.Membership(ctx, "new", now)
	if err != nil || upgraded.Tier != "svip" || !upgraded.ExpiresAt.Equal(*member.ExpiresAt) {
		t.Fatal("upgrade extended expiry", upgraded, err)
	}
	if _, err = order("downgrade", "vip", now); !errors.Is(err, domain.ErrMembershipProduct) {
		t.Fatal(err)
	}
	if _, err = order("renew", "svip", now); err != nil {
		t.Fatal(err)
	}
	if err = store.ConfirmMembershipPayment(ctx, "renew", "trade3", "merchant", 1990, now); err != nil {
		t.Fatal(err)
	}
	renewed, err := store.Membership(ctx, "new", now)
	if err != nil || !renewed.ExpiresAt.Equal(now.Add(1440*time.Hour)) {
		t.Fatal(renewed, err)
	}
	later := renewed.ExpiresAt.Add(time.Hour)
	if _, err = order("later-vip", "vip", later); err != nil {
		t.Fatal(err)
	}
	if err = store.ConfirmMembershipPayment(ctx, "later-vip", "trade4", "merchant", 990, later); err != nil {
		t.Fatal(err)
	}
	member, err = store.Membership(ctx, "new", later)
	if err != nil || member.Tier != "vip" || !member.ExpiresAt.Equal(later.Add(720*time.Hour)) {
		t.Fatal(member, err)
	}
	if _, err = order("late-upgrade", "upgrade", later); err != nil {
		t.Fatal(err)
	}
	if err = store.ConfirmMembershipPayment(ctx, "late-upgrade", "trade5", "merchant", 1000, member.ExpiresAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	review, err := store.MembershipOrder(ctx, "late-upgrade")
	if err != nil || review.Status != "review_required" || review.PaidAt == nil {
		t.Fatal("late payment lost", review, err)
	}
	end := member.ExpiresAt.Add(48 * time.Hour)
	if err = store.AdjustMembership(ctx, "new", domain.MembershipAdjustment{Tier: "svip", ExpiresAt: &end, Revision: member.Revision, ReviewOrderID: review.ID}, membershipAudit("resolve-review", later)); err != nil {
		t.Fatal(err)
	}
	review, err = store.MembershipOrder(ctx, review.ID)
	if err != nil || review.Status != "paid" {
		t.Fatal(review, err)
	}
}

func TestMembershipOwnerLimitsAndAdministration(t *testing.T) {
	store := membershipTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if err := createMembershipTestPlan(store, "existing", "owner", now); err != nil {
		t.Fatal(err)
	}
	enableMembershipTest(t, store, now)
	member, err := store.Membership(ctx, "owner", now)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	outcomes := make(chan error, 2)
	for _, id := range []string{"concurrent-a", "concurrent-b"} {
		wait.Add(1)
		go func(id string) { defer wait.Done(); outcomes <- createMembershipTestPlan(store, id, "owner", now) }(id)
	}
	wait.Wait()
	close(outcomes)
	successes := 0
	for err := range outcomes {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent creation escaped capacity: %d", successes)
	}
	member, err = store.Membership(ctx, "owner", now)
	if err != nil || member.OwnedPlans != 2 {
		t.Fatal(member, err)
	}
	if _, err = store.UpdatePlanStatus(ctx, "existing", "owner", domain.StatusArchived, membershipAudit("archive", now)); err != nil {
		t.Fatal(err)
	}
	if err = createMembershipTestPlan(store, "replacement", "owner", now); err != nil {
		t.Fatal("archive did not release slot", err)
	}
	if _, err = store.UpdatePlanStatus(ctx, "existing", "owner", domain.StatusActive, membershipAudit("restore-denied", now)); !errors.Is(err, domain.ErrOwnerLimit) {
		t.Fatal("restore bypass", err)
	}
	limit := 4
	input := domain.MembershipAdjustment{Tier: "svip", ExpiresAt: member.ExpiresAt, OwnerLimitOverride: &limit, Revision: member.Revision}
	if err = store.AdjustMembership(ctx, "owner", input, membershipAudit("limit-four", now)); err != nil {
		t.Fatal(err)
	}
	if err = store.AdjustMembership(ctx, "owner", input, membershipAudit("stale-limit", now)); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale admin overwrite", err)
	}
	if _, err = store.UpdatePlanStatus(ctx, "existing", "owner", domain.StatusActive, membershipAudit("restore", now)); err != nil {
		t.Fatal(err)
	}
	member, err = store.Membership(ctx, "owner", now)
	if err != nil || member.OwnedPlans != 3 {
		t.Fatal(member, err)
	}
	limit = 1
	input.Revision = member.Revision
	if err = store.AdjustMembership(ctx, "owner", input, membershipAudit("limit-one", now)); err != nil {
		t.Fatal(err)
	}
	member, err = store.Membership(ctx, "owner", now)
	if err != nil || member.OwnedPlans != 3 || member.OwnerLimit != 1 {
		t.Fatal("lowering limit removed Plans", member, err)
	}
	if err = createMembershipTestPlan(store, "over-limit", "owner", now); !errors.Is(err, domain.ErrOwnerLimit) {
		t.Fatal(err)
	}
	input.OwnerLimitOverride = nil
	input.Revision = member.Revision
	if err = store.AdjustMembership(ctx, "owner", input, membershipAudit("reset-default", now)); err != nil {
		t.Fatal(err)
	}
	member, err = store.Membership(ctx, "owner", now)
	if err != nil || member.OwnerLimitOverride != nil || member.OwnerLimit != 2 {
		t.Fatal(member, err)
	}
	_, err = store.pool.Exec(ctx, `INSERT INTO plan_members(id,plan_id,user_id,role,status,share_basis_points) VALUES('new-owner','existing','new','member','active',0)`)
	if err != nil {
		t.Fatal(err)
	}
	end := now.Add(720 * time.Hour)
	if err = store.AdjustMembership(ctx, "new", domain.MembershipAdjustment{Tier: "svip", ExpiresAt: &end}, membershipAudit("grant-new", now)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.TransferPlanOwnership(ctx, "existing", "owner", "new-owner", membershipAudit("transfer", now)); err != nil {
		t.Fatal(err)
	}
	newOwner, err := store.Membership(ctx, "new", now)
	if err != nil || newOwner.OwnedPlans != 1 {
		t.Fatal(newOwner, err)
	}
}
