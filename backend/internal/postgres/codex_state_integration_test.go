package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestCodexStatePersistenceLeasesAndRevocation(t *testing.T) {
	url := os.Getenv("SHARESUB_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SHARESUB_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("state_test_%d", time.Now().UnixNano())
	id := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+id); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+id+" CASCADE")
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := &Store{pool: pool}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO users(id,email,username,password_hash) VALUES('u','u@example.test','testuser','hash')`); err != nil {
		t.Fatal(err)
	}
	account, err := s.CreateOrRotateAccountAuthorization(ctx, domain.Account{ID: "a", OwnerUserID: "u", Name: "Test", Email: "a@example.test", ChatGPTAccountID: "c", PlanType: "pro", AccessTokenCiphertext: []byte("access"), RefreshTokenCiphertext: []byte("refresh"), TokenExpiresAt: time.Now().Add(time.Hour), Status: domain.StatusActive, CodexFingerprintMode: "session", StateEnabled: true, CreatedAt: time.Now()}, false)
	if err != nil || !account.StateEnabled {
		t.Fatalf("create: enabled=%v err=%v", account.StateEnabled, err)
	}
	scope := application.CodexStateAccountScope(account)
	if _, err = s.EnsureStateJob(ctx, "a", "stale-model", "stale-fingerprint", "old-scope"); err != domain.ErrAccountUnavailable {
		t.Fatalf("stale scope enrolled: %v", err)
	}

	job, err := s.EnsureStateJob(ctx, "a", "m", "fp", scope)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := s.ClaimStateJobs(ctx, "lease1")
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: %v %v", jobs, err)
	}
	others, err := s.ClaimStateJobs(ctx, "lease2")
	if err != nil || len(others) != 0 {
		t.Fatalf("duplicate lease: %v %v", others, err)
	}
	job = jobs[0]
	expires := time.Now().Add(time.Hour)
	job.Version = "v1"
	job.Ciphertext = []byte("cipher")
	job.ExpiresAt = &expires
	job.NextAttemptAt = time.Now().Add(50 * time.Minute)
	job.Result = "ready"
	if err = s.FinishStateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err = s.RejectStateTicket(ctx, domain.CodexStateReceipt{AccountID: "a", Model: "m", Version: "stale"}, "state_312"); err != nil {
		t.Fatal(err)
	}
	job, err = s.EnsureStateJob(ctx, "a", "m", "fp", scope)
	if err != nil || job.Version != "v1" {
		t.Fatal("stale receipt deleted replacement", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE openai_accounts SET name='Renamed',notes='updated',proxy_url_ciphertext=proxy_url_ciphertext WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	preserved, err := s.EnsureStateJob(ctx, "a", "m", "fp", scope)
	if err != nil || preserved.Version != "v1" {
		t.Fatal("unchanged proxy revoked ticket", err)
	}
	if _, err = s.EnsureStateJob(ctx, "a", "m", "stale-fp", "old-scope"); err != domain.ErrAccountUnavailable {
		t.Fatalf("stale enrollment accepted: %v", err)
	}
	preserved, err = s.EnsureStateJob(ctx, "a", "m", "fp", scope)
	if err != nil || preserved.Version != "v1" {
		t.Fatal("stale enrollment deleted current ticket", err)
	}
	if err = s.RefreshStateJobs(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	jobs, err = s.ClaimStateJobs(ctx, "renewal")
	if err != nil || len(jobs) != 1 {
		t.Fatal("renewal claim", err)
	}
	renewal := jobs[0]
	if err = s.RejectStateTicket(ctx, domain.CodexStateReceipt{AccountID: "a", Model: "m", Version: "v1"}, "model_mismatch"); err != nil {
		t.Fatal(err)
	}
	renewal.Result = "rate_limited"
	renewal.NextAttemptAt = time.Now().Add(5 * time.Minute)
	if err = s.FinishStateJob(ctx, renewal); err != nil {
		t.Fatal(err)
	}
	job, err = s.EnsureStateJob(ctx, "a", "m", "fp", scope)
	if err != nil || job.Version != "" {
		t.Fatal("failed renewal revived revoked ticket", err)
	}
	// Configuration changes delete jobs and invalidate their outstanding leases.
	if _, err = pool.Exec(ctx, `UPDATE openai_accounts SET state_enabled=false WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	renewal.Result = "ready"
	renewal.Version = "late"
	if err = s.FinishStateJob(ctx, renewal); err != nil {
		t.Fatal(err)
	}
	jobs, err = s.ListStateJobs(ctx, "a")
	if err != nil || len(jobs) != 0 {
		t.Fatal("disabled job revived", err)
	}
	if _, err = s.EnsureStateJob(ctx, "a", "m", "fp", scope); err != domain.ErrAccountUnavailable {
		t.Fatalf("disabled enrollment: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE openai_accounts SET state_enabled=true WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err = s.EnsureStateJob(ctx, "a", fmt.Sprint(i), "fp", scope); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.EnsureStateJob(ctx, "a", "ninth", "fp", scope); err == nil {
		t.Fatal("model cap ignored")
	}
	if _, err = pool.Exec(ctx, `UPDATE openai_accounts SET refresh_token_ciphertext='new'::bytea WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	jobs, err = s.ListStateJobs(ctx, "a")
	if err != nil || len(jobs) != 0 {
		t.Fatal("authorization change did not revoke", err)
	}
}
