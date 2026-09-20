package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/migrations"
)

func TestCodexFingerprintDefaultMigrationPreservesExistingModes(t *testing.T) {
	store := membershipTestStore(t)
	ctx := context.Background()
	// Recreate the pre-037 default with accounts covering every stored mode.
	if _, err := store.pool.Exec(ctx, `ALTER TABLE openai_accounts ALTER COLUMN codex_fingerprint_mode SET DEFAULT 'session'`); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO openai_accounts(id,owner_user_id,name,email,chatgpt_account_id,access_token_ciphertext,refresh_token_ciphertext,token_expires_at,status)
 VALUES($1,'owner','test','owner@example.test',$1,'access','refresh',now()+interval '1 day','active')`
	for _, mode := range []string{"off", "device", "session", "full"} {
		if _, err := store.pool.Exec(ctx, insert, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `UPDATE openai_accounts SET codex_fingerprint_mode=$1 WHERE id=$1`, mode); err != nil {
			t.Fatal(err)
		}
	}
	// This row represents an account created with the old implicit default.
	if _, err := store.pool.Exec(ctx, insert, "old-default"); err != nil {
		t.Fatal(err)
	}
	migration, err := migrations.Files.ReadFile("037_codex_fingerprint_default_off.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"off", "device", "session", "full", "old-default"} {
		account, err := store.AccountByID(ctx, mode)
		if err != nil {
			t.Fatal(err)
		}
		want := mode
		if mode == "old-default" {
			want = "session"
		}
		if account.CodexFingerprintMode != want {
			t.Fatalf("existing %s changed to %s", mode, account.CodexFingerprintMode)
		}
		// Reconnecting with the new creation default must not overwrite configuration.
		account.ID = "replacement-" + mode
		account.CodexFingerprintMode = "off"
		account.CreatedAt = time.Now()
		updated, err := store.CreateOrRotateAccountAuthorization(ctx, account, false)
		if err != nil {
			t.Fatal(err)
		}
		if updated.ID != mode || updated.CodexFingerprintMode != want {
			t.Fatalf("reauthorization changed identity/mode: %+v", updated)
		}
	}
	if _, err := store.pool.Exec(ctx, insert, "new-default"); err != nil {
		t.Fatal(err)
	}
	account, err := store.AccountByID(ctx, "new-default")
	if err != nil {
		t.Fatal(err)
	}
	if account.CodexFingerprintMode != "off" {
		t.Fatalf("new account default = %s", account.CodexFingerprintMode)
	}
}
