package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Store) EnsureBootstrapAdmin(ctx context.Context, user domain.User) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7864213901)`); err != nil {
		return false, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE role='admin')`).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, tx.Commit(ctx)
	}
	var existingID string
	err = tx.QueryRow(ctx, `SELECT id FROM users WHERE lower(email)=lower($1) FOR UPDATE`, user.Email).Scan(&existingID)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE users SET password_hash=$2,status='active',role='admin',must_change_password=true,email_verified_at=COALESCE(email_verified_at,$3),updated_at=$3 WHERE id=$1`, existingID, user.PasswordHash, user.CreatedAt)
	} else if err == pgx.ErrNoRows {
		_, err = tx.Exec(ctx, `INSERT INTO users(id,username,email,password_hash,status,role,must_change_password,email_verified_at,created_at,updated_at) VALUES($1,CASE WHEN EXISTS(SELECT 1 FROM users WHERE lower(username)='admin') THEN 'admin_' || left($1,8) ELSE $2 END,$3,$4,'active','admin',true,$5,$5,$5)`, user.ID, user.Username, user.Email, user.PasswordHash, user.CreatedAt)
	}
	if err != nil {
		return false, mapError(err)
	}
	return true, tx.Commit(ctx)
}

func (s *Store) ResetAdminPassword(ctx context.Context, email, passwordHash string) (domain.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)
	user, err := scanUser(tx.QueryRow(ctx, `UPDATE users SET password_hash=$2,must_change_password=true,status='active',email_verified_at=COALESCE(email_verified_at,now()),updated_at=now() WHERE lower(email)=lower($1) AND role='admin' RETURNING id,username,email,password_hash,status,role,must_change_password,email_verified_at,created_at,avatar_updated_at`, email, passwordHash))
	if err != nil {
		return domain.User{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_sessions WHERE user_id=$1`, user.ID); err != nil {
		return domain.User{}, err
	}
	return user, tx.Commit(ctx)
}

func (s *Store) AdminOverview(ctx context.Context, metricsStart time.Time) (domain.AdminOverview, error) {
	var out domain.AdminOverview
	var successCount int64
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM users),(SELECT count(*) FROM users WHERE status='active'),
			(SELECT count(*) FROM openai_accounts),(SELECT count(*) FROM openai_accounts WHERE status='active'),
			(SELECT count(*) FROM shared_plans),(SELECT count(*) FROM shared_plans WHERE status='active'),
			(SELECT count(*) FROM api_keys),(SELECT count(*) FROM api_keys WHERE status='active'),
			(SELECT count(*) FROM gateway_request_metrics WHERE created_at>=$1),
			(SELECT COALESCE(sum(input_tokens+output_tokens),0) FROM gateway_request_metrics WHERE created_at>=$1),
			(SELECT COALESCE(sum(estimated_cost_micros),0) FROM gateway_request_metrics WHERE created_at>=$1),
			(SELECT count(*) FROM gateway_request_metrics WHERE created_at>=$1 AND status_code BETWEEN 200 AND 299)`, metricsStart).Scan(
		&out.UserCount, &out.ActiveUserCount, &out.AccountCount, &out.ActiveAccounts,
		&out.PlanCount, &out.ActivePlans, &out.APIKeyCount, &out.ActiveAPIKeys,
		&out.Requests24H, &out.Tokens24H, &out.CostMicros24H, &successCount,
	)
	if err == nil && out.Requests24H > 0 {
		out.SuccessRate24H = float64(successCount) / float64(out.Requests24H) * 100
	}
	return out, err
}

func (s *Store) AdminListUsers(ctx context.Context) ([]domain.AdminUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id,u.username,u.email,u.password_hash,u.status,u.role,u.must_change_password,u.email_verified_at,u.created_at,u.avatar_updated_at,
			(SELECT count(*) FROM openai_accounts a WHERE a.owner_user_id=u.id),
			(SELECT count(*) FROM plan_members m WHERE m.user_id=u.id AND m.status='active'),
			(SELECT count(*) FROM api_keys k WHERE k.user_id=u.id)
		FROM users u ORDER BY u.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.AdminUser, 0)
	for rows.Next() {
		var item domain.AdminUser
		var avatarUpdatedAt *time.Time
		if err := rows.Scan(&item.ID, &item.Username, &item.Email, &item.PasswordHash, &item.Status, &item.Role, &item.MustChangePassword, &item.EmailVerifiedAt, &item.CreatedAt, &avatarUpdatedAt, &item.AccountCount, &item.PlanCount, &item.APIKeyCount); err != nil {
			return nil, err
		}
		item.AvatarURL = userAvatarURL(item.ID, avatarUpdatedAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) AdminUpdateUserStatus(ctx context.Context, userID, status string) (domain.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)
	user, err := scanUser(tx.QueryRow(ctx, `UPDATE users SET status=$2,updated_at=now() WHERE id=$1 RETURNING id,username,email,password_hash,status,role,must_change_password,email_verified_at,created_at,avatar_updated_at`, userID, status))
	if err != nil {
		return domain.User{}, err
	}
	if status == domain.StatusDisabled {
		if _, err := tx.Exec(ctx, `DELETE FROM user_sessions WHERE user_id=$1`, userID); err != nil {
			return domain.User{}, err
		}
	}
	return user, tx.Commit(ctx)
}

func (s *Store) AdminListAccounts(ctx context.Context) ([]domain.AdminAccount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id,a.owner_user_id,a.name,a.notes,a.email,a.chatgpt_account_id,a.plan_type,a.subscription_expires_at,a.proxy_url_ciphertext,a.max_concurrency,a.rpm_limit,a.fast_policy,a.codex_fingerprint_mode,a.token_expires_at,a.status,a.last_error,a.created_at,
			u.username,u.email,COALESCE(p.id,''),COALESCE(p.name,'')
		FROM openai_accounts a JOIN users u ON u.id=a.owner_user_id LEFT JOIN shared_plans p ON p.account_id=a.id
		ORDER BY a.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.AdminAccount, 0)
	for rows.Next() {
		var item domain.AdminAccount
		if err := rows.Scan(&item.ID, &item.OwnerUserID, &item.Name, &item.Notes, &item.Email, &item.ChatGPTAccountID, &item.PlanType, &item.SubscriptionExpiresAt, &item.ProxyURLCiphertext, &item.MaxConcurrency, &item.RPMLimit, &item.FastPolicy, &item.CodexFingerprintMode, &item.TokenExpiresAt, &item.Status, &item.LastError, &item.CreatedAt, &item.OwnerUsername, &item.OwnerEmail, &item.PlanID, &item.PlanName); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) AdminUpdateAccountStatus(ctx context.Context, accountID, status string, event domain.AuditEvent) (domain.AdminAccount, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AdminAccount{}, err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE openai_accounts SET status=$2,last_error=CASE WHEN $2='active' THEN '' ELSE last_error END,updated_at=now() WHERE id=$1`, accountID, status)
	if err != nil {
		return domain.AdminAccount{}, err
	}
	if result.RowsAffected() != 1 {
		return domain.AdminAccount{}, domain.ErrNotFound
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.AdminAccount{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AdminAccount{}, err
	}
	rows, err := s.AdminListAccounts(ctx)
	if err != nil {
		return domain.AdminAccount{}, err
	}
	for _, item := range rows {
		if item.ID == accountID {
			return item, nil
		}
	}
	return domain.AdminAccount{}, domain.ErrNotFound
}

func (s *Store) RecordAuditEvent(ctx context.Context, event domain.AuditEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AdminListPlans(ctx context.Context, metricsStart time.Time) ([]domain.AdminPlan, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id,p.owner_user_id,p.account_id,p.name,p.description,p.status,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode,p.created_at,p.archived_at,
			u.username,COALESCE(a.email,''),
			(SELECT count(*) FROM plan_members m WHERE m.plan_id=p.id AND m.status='active'),
			(SELECT count(*) FROM gateway_request_metrics g WHERE g.plan_id=p.id AND g.created_at>=$1),
			(SELECT COALESCE(sum(g.input_tokens+g.output_tokens),0) FROM gateway_request_metrics g WHERE g.plan_id=p.id AND g.created_at>=$1)
		FROM shared_plans p JOIN users u ON u.id=p.owner_user_id LEFT JOIN openai_accounts a ON a.id=p.account_id
		ORDER BY p.created_at DESC`, metricsStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.AdminPlan, 0)
	for rows.Next() {
		var item domain.AdminPlan
		var accountID *string
		if err := rows.Scan(&item.ID, &item.OwnerUserID, &accountID, &item.Name, &item.Description, &item.Status, &item.Visibility, &item.PublicSlots, &item.PublicShareBasisPoints, &item.AllocationMode, &item.CreatedAt, &item.ArchivedAt, &item.OwnerUsername, &item.AccountEmail, &item.MemberCount, &item.Requests24H, &item.TotalTokens24H); err != nil {
			return nil, err
		}
		if accountID != nil {
			item.AccountID = *accountID
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) AdminPlanByID(ctx context.Context, planID string) (domain.Plan, error) {
	plan, err := scanPlan(s.pool.QueryRow(ctx, `
		SELECT id,owner_user_id,account_id,name,description,status,visibility,public_slots,public_share_basis_points,allocation_mode,created_at,archived_at
		FROM shared_plans
		WHERE id=$1`, planID))
	if err != nil {
		return domain.Plan{}, mapError(err)
	}
	return plan, nil
}

func (s *Store) AdminListAPIKeys(ctx context.Context) ([]domain.AdminAPIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT k.id,k.user_id,u.username,u.email,k.name,k.key_prefix,k.strategy,k.status,k.last_used_at,k.created_at,count(r.plan_id)
		FROM api_keys k JOIN users u ON u.id=k.user_id LEFT JOIN api_key_plans r ON r.api_key_id=k.id
		GROUP BY k.id,u.username,u.email ORDER BY k.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.AdminAPIKey, 0)
	for rows.Next() {
		var item domain.AdminAPIKey
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.Email, &item.Name, &item.KeyPrefix, &item.Strategy, &item.Status, &item.LastUsedAt, &item.CreatedAt, &item.RouteCount); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) AdminRevokeAPIKey(ctx context.Context, keyID string) error {
	result, err := s.pool.Exec(ctx, `UPDATE api_keys SET status='revoked',updated_at=now() WHERE id=$1 AND status='active'`, keyID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}
