package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

const stateJobColumns = `account_id,model,fingerprint,lease,version,ciphertext,expires_at,next_attempt_at,result,attempts,last_used_at`

func scanStateJob(row pgx.Row) (application.CodexStateJob, error) {
	var j application.CodexStateJob
	err := row.Scan(&j.AccountID, &j.Model, &j.Fingerprint, &j.Lease, &j.Version, &j.Ciphertext, &j.ExpiresAt, &j.NextAttemptAt, &j.Result, &j.Attempts, &j.LastUsedAt)
	return j, mapError(err)
}

func (s *Store) EnsureStateJob(ctx context.Context, accountID, model, fingerprint, scope string) (application.CodexStateJob, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return application.CodexStateJob{}, err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	var account domain.Account
	account.ID = accountID
	if err = tx.QueryRow(ctx, `SELECT state_enabled AND status='active',chatgpt_account_id,plan_type,refresh_token_ciphertext,proxy_url_ciphertext,codex_fingerprint_mode FROM openai_accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&enabled, &account.ChatGPTAccountID, &account.PlanType, &account.RefreshTokenCiphertext, &account.ProxyURLCiphertext, &account.CodexFingerprintMode); err != nil {
		return application.CodexStateJob{}, mapError(err)
	}
	if !enabled || scope != application.CodexStateAccountScope(account) {
		return application.CodexStateJob{}, domain.ErrAccountUnavailable
	}
	_, err = tx.Exec(ctx, `DELETE FROM codex_state_jobs WHERE account_id=$1 AND (last_used_at<now()-interval '24 hours' OR (model=$2 AND fingerprint<>$3))`, accountID, model, fingerprint)
	if err != nil {
		return application.CodexStateJob{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO codex_state_jobs(account_id,model,fingerprint) SELECT $1,$2,$3 WHERE (SELECT count(*) FROM codex_state_jobs WHERE account_id=$1)<8 ON CONFLICT(account_id,model) DO NOTHING`, accountID, model, fingerprint)
	if err != nil {
		return application.CodexStateJob{}, err
	}
	j, err := scanStateJob(tx.QueryRow(ctx, `UPDATE codex_state_jobs SET last_used_at=now() WHERE account_id=$1 AND model=$2 RETURNING `+stateJobColumns, accountID, model))
	if err != nil {
		return j, err
	}
	return j, tx.Commit(ctx)
}

func (s *Store) ClaimStateJobs(ctx context.Context, lease string) ([]application.CodexStateJob, error) {
	_, err := s.pool.Exec(ctx, `DELETE FROM codex_state_jobs WHERE last_used_at<now()-interval '24 hours'`)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `UPDATE codex_state_jobs SET lease=$1,lease_until=now()+interval '3 minutes' WHERE (account_id,model) IN (SELECT account_id,model FROM codex_state_jobs WHERE next_attempt_at<=now() AND lease_until<now() ORDER BY next_attempt_at LIMIT 2 FOR UPDATE SKIP LOCKED) RETURNING `+stateJobColumns, lease)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]application.CodexStateJob, 0)
	for rows.Next() {
		j, e := scanStateJob(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) FinishStateJob(ctx context.Context, j application.CodexStateJob) error {
	// A deleted/replaced job, expired lease or changed fingerprint cannot be revived.
	_, err := s.pool.Exec(ctx, `UPDATE codex_state_jobs SET lease='',lease_until='-infinity',version=CASE WHEN $9='ready' THEN $5 ELSE version END,ciphertext=CASE WHEN $9='ready' THEN $6 ELSE ciphertext END,expires_at=CASE WHEN $9='ready' THEN $7 ELSE expires_at END,next_attempt_at=$8,result=$9,attempts=$10 WHERE account_id=$1 AND model=$2 AND fingerprint=$3 AND lease=$4 AND lease_until>now()`, j.AccountID, j.Model, j.Fingerprint, j.Lease, j.Version, j.Ciphertext, j.ExpiresAt, j.NextAttemptAt, j.Result, j.Attempts)
	return err
}

func (s *Store) RejectStateTicket(ctx context.Context, r domain.CodexStateReceipt, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE codex_state_jobs SET version='',ciphertext=''::bytea,expires_at=NULL,result=$4,next_attempt_at=now()+interval '5 minutes' WHERE account_id=$1 AND model=$2 AND version=$3 AND version<>''`, r.AccountID, r.Model, r.Version, reason)
	return err
}

func (s *Store) ListStateJobs(ctx context.Context, accountID string) ([]application.CodexStateJob, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+stateJobColumns+` FROM codex_state_jobs WHERE account_id=$1 ORDER BY model`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]application.CodexStateJob, 0)
	for rows.Next() {
		j, e := scanStateJob(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) RefreshStateJobs(ctx context.Context, accountID string) error {
	// Manual refresh respects the cooldown and does not revoke usable tickets.
	_, err := s.pool.Exec(ctx, `UPDATE codex_state_jobs SET next_attempt_at=now() WHERE account_id=$1 AND result='ready' AND next_attempt_at>now() AND lease_until<now()`, accountID)
	return err
}
