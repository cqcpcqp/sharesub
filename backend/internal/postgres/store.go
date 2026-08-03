package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/migrations"
)

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var applied bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, entry.Name()).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(body)); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, entry.Name())
		}
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (s *Store) CreateUser(ctx context.Context, user domain.User) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO users(id,username,email,password_hash,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$6)`, user.ID, user.Username, user.Email, user.PasswordHash, user.Status, user.CreatedAt)
	return mapError(err)
}

func (s *Store) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT id,username,email,password_hash,status,created_at,avatar_updated_at FROM users WHERE lower(email)=lower($1)`, email))
}

func (s *Store) UserBySessionHash(ctx context.Context, hash []byte, now time.Time) (domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT u.id,u.username,u.email,u.password_hash,u.status,u.created_at,u.avatar_updated_at FROM user_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>$2 AND u.status='active'`, hash, now))
}

func scanUser(row pgx.Row) (domain.User, error) {
	var user domain.User
	var avatarUpdatedAt *time.Time
	err := row.Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.Status, &user.CreatedAt, &avatarUpdatedAt)
	user.AvatarURL = userAvatarURL(user.ID, avatarUpdatedAt)
	return user, mapError(err)
}

func (s *Store) UpdateUsername(ctx context.Context, userID, username string) (domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `UPDATE users SET username=$2,updated_at=now() WHERE id=$1 RETURNING id,username,email,password_hash,status,created_at,avatar_updated_at`, userID, username))
}

func (s *Store) UpdateUserAvatar(ctx context.Context, userID string, avatar domain.UserAvatar, updatedAt time.Time) (domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `UPDATE users SET avatar_data=$2,avatar_media_type=$3,avatar_updated_at=$4,updated_at=$4 WHERE id=$1 RETURNING id,username,email,password_hash,status,created_at,avatar_updated_at`, userID, avatar.Data, avatar.MediaType, updatedAt))
}

func (s *Store) DeleteUserAvatar(ctx context.Context, userID string) (domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `UPDATE users SET avatar_data=NULL,avatar_media_type=NULL,avatar_updated_at=NULL,updated_at=now() WHERE id=$1 RETURNING id,username,email,password_hash,status,created_at,avatar_updated_at`, userID))
}

func (s *Store) UserAvatar(ctx context.Context, userID string) (domain.UserAvatar, error) {
	var avatar domain.UserAvatar
	err := s.pool.QueryRow(ctx, `SELECT avatar_data,avatar_media_type FROM users WHERE id=$1 AND avatar_data IS NOT NULL`, userID).Scan(&avatar.Data, &avatar.MediaType)
	return avatar, mapError(err)
}

func userAvatarURL(userID string, updatedAt *time.Time) string {
	if updatedAt == nil {
		return ""
	}
	return fmt.Sprintf("/api/users/%s/avatar?v=%d", url.PathEscape(userID), updatedAt.UnixNano())
}

func (s *Store) CreateSession(ctx context.Context, id, userID string, hash []byte, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO user_sessions(id,user_id,token_hash,expires_at) VALUES($1,$2,$3,$4)`, id, userID, hash, expiresAt)
	return mapError(err)
}

func (s *Store) DeleteSession(ctx context.Context, hash []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM user_sessions WHERE token_hash=$1`, hash)
	return err
}

func (s *Store) CreateOAuthFlow(ctx context.Context, flow application.OAuthFlow) error {
	var targetAccountID any
	if flow.TargetAccountID != "" {
		targetAccountID = flow.TargetAccountID
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO oauth_flows(id,user_id,state_hash,code_verifier,redirect_uri,purpose,target_account_id,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, flow.ID, flow.UserID, flow.StateHash, flow.CodeVerifier, flow.RedirectURI, flow.Purpose, targetAccountID, flow.ExpiresAt)
	return mapError(err)
}

func (s *Store) ConsumeOAuthFlow(ctx context.Context, stateHash []byte, now time.Time) (application.OAuthFlow, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return application.OAuthFlow{}, err
	}
	defer tx.Rollback(ctx)
	var flow application.OAuthFlow
	var targetAccountID *string
	err = tx.QueryRow(ctx, `SELECT id,user_id,state_hash,code_verifier,redirect_uri,purpose,target_account_id,expires_at FROM oauth_flows WHERE state_hash=$1 AND consumed_at IS NULL AND expires_at>$2 FOR UPDATE`, stateHash, now).Scan(&flow.ID, &flow.UserID, &flow.StateHash, &flow.CodeVerifier, &flow.RedirectURI, &flow.Purpose, &targetAccountID, &flow.ExpiresAt)
	if err != nil {
		return application.OAuthFlow{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE oauth_flows SET consumed_at=$1 WHERE id=$2`, now, flow.ID); err != nil {
		return application.OAuthFlow{}, err
	}
	if targetAccountID != nil {
		flow.TargetAccountID = *targetAccountID
	}
	return flow, tx.Commit(ctx)
}

func (s *Store) UpsertAccount(ctx context.Context, account domain.Account) (domain.Account, error) {
	var out domain.Account
	err := s.pool.QueryRow(ctx, `
		INSERT INTO openai_accounts(id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,access_token_ciphertext,refresh_token_ciphertext,proxy_url_ciphertext,max_concurrency,rpm_limit,token_expires_at,status,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$15)
		ON CONFLICT(owner_user_id,chatgpt_account_id) DO UPDATE SET name=EXCLUDED.name,notes=EXCLUDED.notes,email=EXCLUDED.email,plan_type=EXCLUDED.plan_type,access_token_ciphertext=EXCLUDED.access_token_ciphertext,refresh_token_ciphertext=EXCLUDED.refresh_token_ciphertext,proxy_url_ciphertext=EXCLUDED.proxy_url_ciphertext,max_concurrency=EXCLUDED.max_concurrency,rpm_limit=EXCLUDED.rpm_limit,token_expires_at=EXCLUDED.token_expires_at,status='active',last_error='',updated_at=now()
		RETURNING id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,access_token_ciphertext,refresh_token_ciphertext,proxy_url_ciphertext,max_concurrency,rpm_limit,token_expires_at,status,last_error,created_at`,
		account.ID, account.OwnerUserID, account.Name, account.Notes, account.Email, account.ChatGPTAccountID, account.PlanType, account.AccessTokenCiphertext, account.RefreshTokenCiphertext, account.ProxyURLCiphertext, account.MaxConcurrency, account.RPMLimit, account.TokenExpiresAt, account.Status, account.CreatedAt,
	).Scan(&out.ID, &out.OwnerUserID, &out.Name, &out.Notes, &out.Email, &out.ChatGPTAccountID, &out.PlanType, &out.AccessTokenCiphertext, &out.RefreshTokenCiphertext, &out.ProxyURLCiphertext, &out.MaxConcurrency, &out.RPMLimit, &out.TokenExpiresAt, &out.Status, &out.LastError, &out.CreatedAt)
	return out, mapError(err)
}

func (s *Store) ListAccounts(ctx context.Context, userID string) ([]domain.Account, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,proxy_url_ciphertext,max_concurrency,rpm_limit,token_expires_at,status,last_error,created_at FROM openai_accounts WHERE owner_user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Account, 0)
	for rows.Next() {
		var a domain.Account
		if err := rows.Scan(&a.ID, &a.OwnerUserID, &a.Name, &a.Notes, &a.Email, &a.ChatGPTAccountID, &a.PlanType, &a.ProxyURLCiphertext, &a.MaxConcurrency, &a.RPMLimit, &a.TokenExpiresAt, &a.Status, &a.LastError, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) AccountByID(ctx context.Context, id string) (domain.Account, error) {
	var a domain.Account
	err := s.pool.QueryRow(ctx, `SELECT id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,access_token_ciphertext,refresh_token_ciphertext,proxy_url_ciphertext,max_concurrency,rpm_limit,token_expires_at,status,last_error,created_at FROM openai_accounts WHERE id=$1`, id).Scan(&a.ID, &a.OwnerUserID, &a.Name, &a.Notes, &a.Email, &a.ChatGPTAccountID, &a.PlanType, &a.AccessTokenCiphertext, &a.RefreshTokenCiphertext, &a.ProxyURLCiphertext, &a.MaxConcurrency, &a.RPMLimit, &a.TokenExpiresAt, &a.Status, &a.LastError, &a.CreatedAt)
	return a, mapError(err)
}

func (s *Store) UpdateAccountConfig(ctx context.Context, userID string, account domain.Account) (domain.Account, error) {
	var out domain.Account
	err := s.pool.QueryRow(ctx, `UPDATE openai_accounts SET name=$3,notes=$4,proxy_url_ciphertext=$5,max_concurrency=$6,rpm_limit=$7,status=$8,last_error=CASE WHEN $8='active' THEN '' ELSE last_error END,updated_at=now() WHERE id=$1 AND owner_user_id=$2 RETURNING id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,proxy_url_ciphertext,max_concurrency,rpm_limit,token_expires_at,status,last_error,created_at`, account.ID, userID, account.Name, account.Notes, account.ProxyURLCiphertext, account.MaxConcurrency, account.RPMLimit, account.Status).Scan(&out.ID, &out.OwnerUserID, &out.Name, &out.Notes, &out.Email, &out.ChatGPTAccountID, &out.PlanType, &out.ProxyURLCiphertext, &out.MaxConcurrency, &out.RPMLimit, &out.TokenExpiresAt, &out.Status, &out.LastError, &out.CreatedAt)
	return out, mapError(err)
}

func (s *Store) UpdateAccountTokens(ctx context.Context, id string, access, refresh []byte, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE openai_accounts SET access_token_ciphertext=$2,refresh_token_ciphertext=$3,token_expires_at=$4,status='active',last_error='',updated_at=now() WHERE id=$1`, id, access, refresh, expiresAt)
	return mapError(err)
}

func (s *Store) MarkAccountError(ctx context.Context, id, message string) error {
	_, err := s.pool.Exec(ctx, `UPDATE openai_accounts SET status=$2,last_error=$3,updated_at=now() WHERE id=$1`, id, domain.StatusRefreshRequired, message)
	return err
}

func (s *Store) CreatePlan(ctx context.Context, plan domain.Plan, owner domain.Member, event domain.AuditEvent) error {
	if plan.AllocationMode == domain.AllocationFixed && (owner.ShareBasisPoints < 1 || owner.ShareBasisPoints > domain.MaxShareBPS) {
		return domain.ErrInvalidInput
	}
	if plan.AllocationMode == domain.AllocationShared && owner.ShareBasisPoints != 0 {
		return domain.ErrInvalidInput
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var accountOwner, accountStatus string
	if err = tx.QueryRow(ctx, `SELECT owner_user_id,status FROM openai_accounts WHERE id=$1 FOR UPDATE`, plan.AccountID).Scan(&accountOwner, &accountStatus); err != nil {
		return mapError(err)
	}
	if accountOwner != plan.OwnerUserID {
		return domain.ErrForbidden
	}
	if accountStatus != domain.StatusActive {
		return domain.ErrAccountUnavailable
	}
	var alreadyBound bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shared_plans WHERE account_id=$1)`, plan.AccountID).Scan(&alreadyBound); err != nil {
		return err
	}
	if alreadyBound {
		return domain.ErrAccountAlreadyBound
	}
	_, err = tx.Exec(ctx, `INSERT INTO shared_plans(id,owner_user_id,account_id,name,status,visibility,public_slots,public_share_basis_points,allocation_mode,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`, plan.ID, plan.OwnerUserID, plan.AccountID, plan.Name, plan.Status, plan.Visibility, plan.PublicSlots, plan.PublicShareBasisPoints, plan.AllocationMode, plan.CreatedAt)
	if err != nil {
		return mapError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO plan_members(id,plan_id,user_id,role,status,share_basis_points,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7)`, owner.ID, owner.PlanID, owner.UserID, owner.Role, owner.Status, owner.ShareBasisPoints, owner.CreatedAt)
	if err != nil {
		return mapError(err)
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListPlans(ctx context.Context, userID string) ([]domain.Plan, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id,p.owner_user_id,p.account_id,p.name,p.status,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode,p.created_at,p.archived_at FROM shared_plans p JOIN plan_members m ON m.plan_id=p.id WHERE m.user_id=$1 AND m.status='active' ORDER BY p.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Plan, 0)
	for rows.Next() {
		var p domain.Plan
		if err := rows.Scan(&p.ID, &p.OwnerUserID, &p.AccountID, &p.Name, &p.Status, &p.Visibility, &p.PublicSlots, &p.PublicShareBasisPoints, &p.AllocationMode, &p.CreatedAt, &p.ArchivedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PlanDetail(ctx context.Context, planID, userID string) (domain.PlanDetail, error) {
	out := domain.PlanDetail{
		Members:      make([]domain.Member, 0),
		Invites:      make([]domain.Invite, 0),
		Applications: make([]domain.JoinApplication, 0),
		Insights: domain.PlanInsights{
			AccountWindows: make([]domain.QuotaWindow, 0),
			MemberQuotas:   make([]domain.MemberQuota, 0),
			WindowUsage:    make([]domain.WindowUsage, 0),
			MemberRanking:  make([]domain.MemberUsageRank, 0),
		},
	}
	err := s.pool.QueryRow(ctx, `SELECT p.id,p.owner_user_id,p.account_id,p.name,p.status,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode,p.created_at,p.archived_at,a.id,a.owner_user_id,a.name,a.notes,a.email,a.chatgpt_account_id,a.plan_type,a.proxy_url_ciphertext,a.max_concurrency,a.rpm_limit,a.token_expires_at,a.status,a.last_error,a.created_at FROM shared_plans p JOIN plan_members viewer ON viewer.plan_id=p.id AND viewer.user_id=$2 AND viewer.status='active' JOIN openai_accounts a ON a.id=p.account_id WHERE p.id=$1`, planID, userID).Scan(&out.Plan.ID, &out.Plan.OwnerUserID, &out.Plan.AccountID, &out.Plan.Name, &out.Plan.Status, &out.Plan.Visibility, &out.Plan.PublicSlots, &out.Plan.PublicShareBasisPoints, &out.Plan.AllocationMode, &out.Plan.CreatedAt, &out.Plan.ArchivedAt, &out.Account.ID, &out.Account.OwnerUserID, &out.Account.Name, &out.Account.Notes, &out.Account.Email, &out.Account.ChatGPTAccountID, &out.Account.PlanType, &out.Account.ProxyURLCiphertext, &out.Account.MaxConcurrency, &out.Account.RPMLimit, &out.Account.TokenExpiresAt, &out.Account.Status, &out.Account.LastError, &out.Account.CreatedAt)
	if err != nil {
		return out, mapError(err)
	}
	isOwner := out.Plan.OwnerUserID == userID
	rows, err := s.pool.Query(ctx, `SELECT m.id,m.plan_id,m.user_id,u.username,u.email,u.avatar_updated_at,m.role,m.status,m.share_basis_points,m.created_at FROM plan_members m JOIN users u ON u.id=m.user_id WHERE m.plan_id=$1 AND m.status='active' ORDER BY CASE WHEN m.role='owner' THEN 0 ELSE 1 END,m.created_at`, planID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var m domain.Member
		var avatarUpdatedAt *time.Time
		if err := rows.Scan(&m.ID, &m.PlanID, &m.UserID, &m.Username, &m.Email, &avatarUpdatedAt, &m.Role, &m.Status, &m.ShareBasisPoints, &m.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		m.AvatarURL = userAvatarURL(m.UserID, avatarUpdatedAt)
		if !isOwner && m.UserID != userID {
			m.Email = ""
		}
		out.Members = append(out.Members, m)
	}
	rows.Close()
	if isOwner {
		invites, err := s.listInvites(ctx, planID)
		if err != nil {
			return out, err
		}
		out.Invites = invites
		applications, err := s.listJoinApplications(ctx, planID)
		if err != nil {
			return out, err
		}
		out.Applications = applications
	}
	insights, err := s.planInsights(ctx, planID, out.Plan.AccountID, out.Members)
	if err != nil {
		return out, err
	}
	out.Insights = insights
	return out, nil
}

func (s *Store) planInsights(ctx context.Context, planID, accountID string, members []domain.Member) (domain.PlanInsights, error) {
	out := domain.PlanInsights{
		AccountWindows: make([]domain.QuotaWindow, 0),
		MemberQuotas:   make([]domain.MemberQuota, 0, len(members)),
		WindowUsage:    make([]domain.WindowUsage, 0, 2),
		MemberRanking:  make([]domain.MemberUsageRank, 0, len(members)),
	}
	memberIndexes := make(map[string]int, len(members))
	for _, member := range members {
		memberIndexes[member.ID] = len(out.MemberQuotas)
		out.MemberQuotas = append(out.MemberQuotas, domain.MemberQuota{MemberID: member.ID, Windows: make([]domain.QuotaWindow, 0)})
	}

	rows, err := s.pool.Query(ctx, `SELECT window_type,used_micros,used_micros,reset_at FROM account_quota_snapshots WHERE account_id=$1 AND reset_at>now() ORDER BY window_type`, accountID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var window domain.QuotaWindow
		if err := rows.Scan(&window.WindowType, &window.UsedMicros, &window.AccountUsedMicros, &window.ResetAt); err != nil {
			rows.Close()
			return out, err
		}
		out.AccountWindows = append(out.AccountWindows, window)
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `SELECT member_id,window_type,used_micros,account_used_micros,reset_at FROM member_quota_windows WHERE member_id IN (SELECT id FROM plan_members WHERE plan_id=$1 AND status='active') AND account_id=$2 AND reset_at>now() ORDER BY member_id,window_type`, planID, accountID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var memberID string
		var window domain.QuotaWindow
		if err := rows.Scan(&memberID, &window.WindowType, &window.UsedMicros, &window.AccountUsedMicros, &window.ResetAt); err != nil {
			rows.Close()
			return out, err
		}
		if index, ok := memberIndexes[memberID]; ok {
			out.MemberQuotas[index].Windows = append(out.MemberQuotas[index].Windows, window)
		}
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT q.window_type,q.window_start,q.reset_at,
			count(g.id),COALESCE(sum(g.input_tokens),0),COALESCE(sum(g.output_tokens),0),COALESCE(sum(g.cached_tokens),0),COALESCE(sum(g.estimated_cost_micros),0)
		FROM account_quota_snapshots q
		LEFT JOIN gateway_request_metrics g ON g.plan_id=$1 AND g.account_id=q.account_id AND g.created_at>=q.window_start AND g.created_at<q.reset_at
		WHERE q.account_id=$2 AND q.reset_at>now()
		GROUP BY q.window_type,q.window_start,q.reset_at
		ORDER BY q.window_type`, planID, accountID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var usage domain.WindowUsage
		if err := rows.Scan(&usage.WindowType, &usage.WindowStart, &usage.WindowEnd, &usage.RequestCount, &usage.TokenUsage.InputTokens, &usage.TokenUsage.OutputTokens, &usage.TokenUsage.CachedTokens, &usage.EstimatedCostMicros); err != nil {
			rows.Close()
			return out, err
		}
		usage.TokenUsage.TotalTokens = usage.TokenUsage.InputTokens + usage.TokenUsage.OutputTokens
		out.WindowUsage = append(out.WindowUsage, usage)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT m.id,u.username,count(g.id),COALESCE(sum(g.input_tokens),0),COALESCE(sum(g.output_tokens),0),COALESCE(sum(g.cached_tokens),0),COALESCE(sum(g.estimated_cost_micros),0)
		FROM gateway_request_metrics g
		JOIN plan_members m ON m.id=g.member_id
		JOIN users u ON u.id=m.user_id
		WHERE g.plan_id=$1 AND g.created_at>=now()-interval '7 days'
		GROUP BY m.id,u.username
		ORDER BY COALESCE(sum(g.input_tokens+g.output_tokens),0) DESC,count(g.id) DESC,u.username`, planID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var rank domain.MemberUsageRank
		if err := rows.Scan(&rank.MemberID, &rank.Username, &rank.RequestCount, &rank.TokenUsage.InputTokens, &rank.TokenUsage.OutputTokens, &rank.TokenUsage.CachedTokens, &rank.EstimatedCostMicros); err != nil {
			rows.Close()
			return out, err
		}
		rank.TokenUsage.TotalTokens = rank.TokenUsage.InputTokens + rank.TokenUsage.OutputTokens
		out.MemberRanking = append(out.MemberRanking, rank)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	err = s.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE status_code BETWEEN 200 AND 299),COALESCE(avg(ttft_ms),0)::float8,COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY ttft_ms),0)::float8,COALESCE(avg(duration_ms),0)::float8,COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms),0)::float8 FROM gateway_request_metrics WHERE plan_id=$1 AND created_at>=now()-interval '24 hours'`, planID).Scan(&out.Performance.RequestCount, &out.Performance.SuccessCount, &out.Performance.AverageTTFTMs, &out.Performance.P95TTFTMs, &out.Performance.AverageDurationMs, &out.Performance.P95DurationMs)
	if err != nil {
		return out, err
	}
	return out, nil
}

func (s *Store) listJoinApplications(ctx context.Context, planID string) ([]domain.JoinApplication, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.id,a.plan_id,a.user_id,u.username,u.email,u.avatar_updated_at,a.message,a.status,a.member_id,a.reviewed_at,a.created_at FROM plan_join_applications a JOIN users u ON u.id=a.user_id WHERE a.plan_id=$1 ORDER BY CASE WHEN a.status='pending' THEN 0 ELSE 1 END,a.created_at DESC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.JoinApplication, 0)
	for rows.Next() {
		var application domain.JoinApplication
		var avatarUpdatedAt *time.Time
		if err := rows.Scan(&application.ID, &application.PlanID, &application.UserID, &application.Username, &application.Email, &avatarUpdatedAt, &application.Message, &application.Status, &application.MemberID, &application.ReviewedAt, &application.CreatedAt); err != nil {
			return nil, err
		}
		application.AvatarURL = userAvatarURL(application.UserID, avatarUpdatedAt)
		out = append(out, application)
	}
	return out, rows.Err()
}

func (s *Store) listInvites(ctx context.Context, planID string) ([]domain.Invite, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,plan_id,share_basis_points,CASE WHEN status='pending' AND expires_at<=now() THEN 'expired' ELSE status END,expires_at,accepted_by_user_id,accepted_at,revoked_at,created_at FROM plan_invites WHERE plan_id=$1 ORDER BY created_at DESC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Invite, 0)
	for rows.Next() {
		var v domain.Invite
		if err := rows.Scan(&v.ID, &v.PlanID, &v.ShareBasisPoints, &v.Status, &v.ExpiresAt, &v.AcceptedByUserID, &v.AcceptedAt, &v.RevokedAt, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ListPublicPlans(ctx context.Context, userID string) ([]domain.PublicPlan, error) {
	rows, err := s.pool.Query(ctx, `
			SELECT p.id,p.owner_user_id,p.account_id,p.name,p.status,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode,p.created_at,p.archived_at,
				u.username,u.avatar_updated_at,a.plan_type,
				(SELECT count(*) FROM plan_members m WHERE m.plan_id=p.id AND m.status='active'),
				GREATEST(p.public_slots-(SELECT count(*) FROM plan_join_applications j JOIN plan_members jm ON jm.id=j.member_id AND jm.status='active' WHERE j.plan_id=p.id AND j.status='approved'),0),
			COALESCE((SELECT j.status FROM plan_join_applications j WHERE j.plan_id=p.id AND j.user_id=$1 ORDER BY j.created_at DESC LIMIT 1),'')
		FROM shared_plans p
		JOIN users u ON u.id=p.owner_user_id
		JOIN openai_accounts a ON a.id=p.account_id
		WHERE p.visibility='public' AND p.status='active'
		ORDER BY CASE WHEN p.owner_user_id=$1 THEN 0 ELSE 1 END,p.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.PublicPlan, 0)
	for rows.Next() {
		var item domain.PublicPlan
		var avatarUpdatedAt *time.Time
		if err := rows.Scan(&item.Plan.ID, &item.Plan.OwnerUserID, &item.Plan.AccountID, &item.Plan.Name, &item.Plan.Status, &item.Plan.Visibility, &item.Plan.PublicSlots, &item.Plan.PublicShareBasisPoints, &item.Plan.AllocationMode, &item.Plan.CreatedAt, &item.Plan.ArchivedAt, &item.OwnerUsername, &avatarUpdatedAt, &item.PlanType, &item.MemberCount, &item.AvailableSlots, &item.ApplicationStatus); err != nil {
			return nil, err
		}
		item.OwnerAvatarURL = userAvatarURL(item.Plan.OwnerUserID, avatarUpdatedAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpdatePlanPublication(ctx context.Context, ownerID, planID, visibility string, slots, share int, event domain.AuditEvent) (domain.Plan, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Plan{}, err
	}
	defer tx.Rollback(ctx)
	var plan domain.Plan
	err = tx.QueryRow(ctx, `SELECT id,owner_user_id,account_id,name,status,visibility,public_slots,public_share_basis_points,allocation_mode,created_at,archived_at FROM shared_plans WHERE id=$1 FOR UPDATE`, planID).Scan(&plan.ID, &plan.OwnerUserID, &plan.AccountID, &plan.Name, &plan.Status, &plan.Visibility, &plan.PublicSlots, &plan.PublicShareBasisPoints, &plan.AllocationMode, &plan.CreatedAt, &plan.ArchivedAt)
	if err != nil {
		return domain.Plan{}, mapError(err)
	}
	if plan.OwnerUserID != ownerID {
		return domain.Plan{}, domain.ErrForbidden
	}
	if plan.Status != domain.StatusActive {
		return domain.Plan{}, domain.ErrConflict
	}
	if visibility == domain.VisibilityPrivate {
		slots = 0
		share = 0
	} else if plan.AllocationMode == domain.AllocationShared {
		if share != 0 {
			return domain.Plan{}, domain.ErrInvalidInput
		}
	} else if share < 1 {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	var approved int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM plan_join_applications j JOIN plan_members m ON m.id=j.member_id AND m.status='active' WHERE j.plan_id=$1 AND j.status='approved'`, planID).Scan(&approved)
	if err != nil {
		return domain.Plan{}, err
	}
	if slots < approved {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	if plan.AllocationMode == domain.AllocationFixed {
		var allocated int
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT sum(share_basis_points) FROM plan_members WHERE plan_id=$1 AND status='active'),0)+COALESCE((SELECT sum(share_basis_points) FROM plan_invites WHERE plan_id=$1 AND status='pending' AND expires_at>now()),0)`, planID).Scan(&allocated); err != nil {
			return domain.Plan{}, err
		}
		if allocated+(slots-approved)*share > domain.MaxShareBPS {
			return domain.Plan{}, domain.ErrShareExceeded
		}
	}
	err = tx.QueryRow(ctx, `UPDATE shared_plans SET visibility=$3,public_slots=$4,public_share_basis_points=$5,updated_at=now() WHERE id=$1 AND owner_user_id=$2 RETURNING id,owner_user_id,account_id,name,status,visibility,public_slots,public_share_basis_points,allocation_mode,created_at,archived_at`, planID, ownerID, visibility, slots, share).Scan(&plan.ID, &plan.OwnerUserID, &plan.AccountID, &plan.Name, &plan.Status, &plan.Visibility, &plan.PublicSlots, &plan.PublicShareBasisPoints, &plan.AllocationMode, &plan.CreatedAt, &plan.ArchivedAt)
	if err != nil {
		return domain.Plan{}, mapError(err)
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.Plan{}, err
	}
	return plan, tx.Commit(ctx)
}

func (s *Store) CreateJoinApplication(ctx context.Context, application domain.JoinApplication, event domain.AuditEvent) (domain.JoinApplication, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.JoinApplication{}, err
	}
	defer tx.Rollback(ctx)
	var ownerID string
	var slots, approved int
	err = tx.QueryRow(ctx, `SELECT p.owner_user_id,p.public_slots,(SELECT count(*) FROM plan_join_applications j JOIN plan_members m ON m.id=j.member_id AND m.status='active' WHERE j.plan_id=p.id AND j.status='approved') FROM shared_plans p WHERE p.id=$1 AND p.status='active' AND p.visibility='public' FOR UPDATE`, application.PlanID).Scan(&ownerID, &slots, &approved)
	if err != nil {
		return domain.JoinApplication{}, mapError(err)
	}
	if ownerID == application.UserID {
		return domain.JoinApplication{}, domain.ErrForbidden
	}
	if approved >= slots {
		return domain.JoinApplication{}, domain.ErrPublicPlanFull
	}
	var alreadyMember bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM plan_members WHERE plan_id=$1 AND user_id=$2 AND status='active')`, application.PlanID, application.UserID).Scan(&alreadyMember); err != nil {
		return domain.JoinApplication{}, err
	}
	if alreadyMember {
		return domain.JoinApplication{}, domain.ErrConflict
	}
	err = tx.QueryRow(ctx, `INSERT INTO plan_join_applications(id,plan_id,user_id,message,status,created_at) VALUES($1,$2,$3,$4,$5,$6) RETURNING id,plan_id,user_id,message,status,member_id,reviewed_at,created_at`, application.ID, application.PlanID, application.UserID, application.Message, application.Status, application.CreatedAt).Scan(&application.ID, &application.PlanID, &application.UserID, &application.Message, &application.Status, &application.MemberID, &application.ReviewedAt, &application.CreatedAt)
	if err != nil {
		return domain.JoinApplication{}, mapError(err)
	}
	var avatarUpdatedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT username,email,avatar_updated_at FROM users WHERE id=$1`, application.UserID).Scan(&application.Username, &application.Email, &avatarUpdatedAt); err != nil {
		return domain.JoinApplication{}, err
	}
	application.AvatarURL = userAvatarURL(application.UserID, avatarUpdatedAt)
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.JoinApplication{}, err
	}
	if err := insertNotification(ctx, tx, event.ID+":owner", ownerID, "join_application", "新的加入申请", application.Username+" 申请加入你的 Plan", "plan", application.PlanID, application.CreatedAt); err != nil {
		return domain.JoinApplication{}, err
	}
	return application, tx.Commit(ctx)
}

func (s *Store) ReviewJoinApplication(ctx context.Context, ownerID, applicationID string, approve bool, memberID string, now time.Time, event domain.AuditEvent) (domain.JoinApplication, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.JoinApplication{}, err
	}
	defer tx.Rollback(ctx)
	var application domain.JoinApplication
	var actualOwner, visibility, allocationMode string
	var slots, share int
	var avatarUpdatedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT j.id,j.plan_id,j.user_id,u.username,u.email,u.avatar_updated_at,j.message,j.status,j.member_id,j.reviewed_at,j.created_at,p.owner_user_id,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode FROM plan_join_applications j JOIN users u ON u.id=j.user_id JOIN shared_plans p ON p.id=j.plan_id WHERE j.id=$1 FOR UPDATE OF j,p`, applicationID).Scan(&application.ID, &application.PlanID, &application.UserID, &application.Username, &application.Email, &avatarUpdatedAt, &application.Message, &application.Status, &application.MemberID, &application.ReviewedAt, &application.CreatedAt, &actualOwner, &visibility, &slots, &share, &allocationMode)
	if err != nil {
		return domain.JoinApplication{}, mapError(err)
	}
	application.AvatarURL = userAvatarURL(application.UserID, avatarUpdatedAt)
	if actualOwner != ownerID {
		return domain.JoinApplication{}, domain.ErrForbidden
	}
	if application.Status != "pending" {
		return domain.JoinApplication{}, domain.ErrConflict
	}
	status := "rejected"
	if approve {
		if visibility != domain.VisibilityPublic {
			return domain.JoinApplication{}, domain.ErrConflict
		}
		var approved int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM plan_join_applications j JOIN plan_members m ON m.id=j.member_id AND m.status='active' WHERE j.plan_id=$1 AND j.status='approved'`, application.PlanID).Scan(&approved); err != nil {
			return domain.JoinApplication{}, err
		}
		if approved >= slots {
			return domain.JoinApplication{}, domain.ErrPublicPlanFull
		}
		if allocationMode == domain.AllocationShared && share != 0 {
			return domain.JoinApplication{}, domain.ErrInvalidInput
		}
		if allocationMode == domain.AllocationFixed && share < 1 {
			return domain.JoinApplication{}, domain.ErrInvalidInput
		}
		var existingStatus string
		err = tx.QueryRow(ctx, `SELECT status FROM plan_members WHERE plan_id=$1 AND user_id=$2 FOR UPDATE`, application.PlanID, application.UserID).Scan(&existingStatus)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return domain.JoinApplication{}, err
		}
		if err == nil && existingStatus == domain.StatusActive {
			return domain.JoinApplication{}, domain.ErrConflict
		}
		var actualMemberID string
		err = tx.QueryRow(ctx, `
			INSERT INTO plan_members(id,plan_id,user_id,role,status,share_basis_points,created_at,updated_at,removed_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$7,NULL)
			ON CONFLICT(plan_id,user_id) DO UPDATE SET role='member',status='active',share_basis_points=EXCLUDED.share_basis_points,removed_at=NULL,updated_at=EXCLUDED.updated_at
			RETURNING id`, memberID, application.PlanID, application.UserID, domain.RoleMember, domain.StatusActive, share, now).Scan(&actualMemberID)
		if err != nil {
			return domain.JoinApplication{}, mapError(err)
		}
		application.MemberID = &actualMemberID
		status = "approved"
	}
	err = tx.QueryRow(ctx, `UPDATE plan_join_applications SET status=$2,member_id=$3,reviewed_by_user_id=$4,reviewed_at=$5 WHERE id=$1 RETURNING status,member_id,reviewed_at`, application.ID, status, application.MemberID, ownerID, now).Scan(&application.Status, &application.MemberID, &application.ReviewedAt)
	if err != nil {
		return domain.JoinApplication{}, err
	}
	event.ResourceType = "plan"
	event.ResourceID = application.PlanID
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.JoinApplication{}, err
	}
	title := "加入申请已拒绝"
	body := "你的 Plan 加入申请未通过"
	notificationType := "application_rejected"
	if approve {
		title = "已加入 Plan"
		body = "你的加入申请已通过，可以开始配置 API Key"
		notificationType = "application_approved"
	}
	if err := insertNotification(ctx, tx, event.ID+":applicant", application.UserID, notificationType, title, body, "plan", application.PlanID, now); err != nil {
		return domain.JoinApplication{}, err
	}
	return application, tx.Commit(ctx)
}

func (s *Store) CreateInvite(ctx context.Context, planID, ownerID string, invite domain.Invite, event domain.AuditEvent) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var actualOwner, allocationMode string
	if err = tx.QueryRow(ctx, `SELECT owner_user_id,allocation_mode FROM shared_plans WHERE id=$1 AND status='active' FOR UPDATE`, planID).Scan(&actualOwner, &allocationMode); err != nil {
		return mapError(err)
	}
	if actualOwner != ownerID {
		return domain.ErrForbidden
	}
	if _, err := tx.Exec(ctx, `UPDATE plan_invites SET status='expired' WHERE plan_id=$1 AND status='pending' AND expires_at<=$2`, planID, invite.CreatedAt); err != nil {
		return err
	}
	if allocationMode == domain.AllocationShared {
		if invite.ShareBasisPoints != 0 {
			return domain.ErrInvalidInput
		}
	} else {
		if invite.ShareBasisPoints < 1 {
			return domain.ErrInvalidInput
		}
		var allocated int
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT sum(share_basis_points) FROM plan_members WHERE plan_id=p.id AND status='active'),0)+COALESCE((SELECT sum(share_basis_points) FROM plan_invites WHERE plan_id=p.id AND status='pending' AND expires_at>now()),0)+GREATEST(p.public_slots-(SELECT count(*) FROM plan_join_applications j JOIN plan_members jm ON jm.id=j.member_id AND jm.status='active' WHERE j.plan_id=p.id AND j.status='approved'),0)*p.public_share_basis_points FROM shared_plans p WHERE p.id=$1`, planID).Scan(&allocated); err != nil {
			return err
		}
		if allocated+invite.ShareBasisPoints > domain.MaxShareBPS {
			return domain.ErrShareExceeded
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO plan_invites(id,plan_id,token_hash,share_basis_points,status,expires_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, invite.ID, invite.PlanID, invite.TokenHash, invite.ShareBasisPoints, invite.Status, invite.ExpiresAt, invite.CreatedAt)
	if err != nil {
		return mapError(err)
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AcceptInvite(ctx context.Context, tokenHash []byte, user domain.User, memberID string, now time.Time, event domain.AuditEvent) (domain.Member, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Member{}, err
	}
	defer tx.Rollback(ctx)
	var invite domain.Invite
	var planOwnerID, planStatus string
	err = tx.QueryRow(ctx, `SELECT i.id,i.plan_id,i.share_basis_points,i.status,i.expires_at,p.owner_user_id,p.status FROM plan_invites i JOIN shared_plans p ON p.id=i.plan_id WHERE i.token_hash=$1 FOR UPDATE OF i,p`, tokenHash).Scan(&invite.ID, &invite.PlanID, &invite.ShareBasisPoints, &invite.Status, &invite.ExpiresAt, &planOwnerID, &planStatus)
	if err != nil {
		return domain.Member{}, mapError(err)
	}
	if invite.Status == "pending" && !invite.ExpiresAt.After(now) {
		if _, err := tx.Exec(ctx, `UPDATE plan_invites SET status='expired' WHERE id=$1`, invite.ID); err != nil {
			return domain.Member{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Member{}, err
		}
		return domain.Member{}, domain.ErrConflict
	}
	if invite.Status != "pending" || planStatus != domain.StatusActive {
		return domain.Member{}, domain.ErrConflict
	}
	member := domain.Member{ID: memberID, PlanID: invite.PlanID, UserID: user.ID, Username: user.Username, AvatarURL: user.AvatarURL, Email: user.Email, Role: domain.RoleMember, Status: domain.StatusActive, ShareBasisPoints: invite.ShareBasisPoints, CreatedAt: now}
	var existingStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM plan_members WHERE plan_id=$1 AND user_id=$2 FOR UPDATE`, invite.PlanID, user.ID).Scan(&existingStatus)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.Member{}, err
	}
	if err == nil && existingStatus == domain.StatusActive {
		return domain.Member{}, domain.ErrConflict
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO plan_members(id,plan_id,user_id,role,status,share_basis_points,created_at,updated_at,removed_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$7,NULL)
		ON CONFLICT(plan_id,user_id) DO UPDATE SET role='member',status='active',share_basis_points=EXCLUDED.share_basis_points,removed_at=NULL,updated_at=EXCLUDED.updated_at
		RETURNING id,created_at`, member.ID, member.PlanID, member.UserID, member.Role, member.Status, member.ShareBasisPoints, member.CreatedAt).Scan(&member.ID, &member.CreatedAt)
	if err != nil {
		return domain.Member{}, mapError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE plan_invites SET status='accepted',accepted_by_user_id=$2,accepted_at=$3 WHERE id=$1`, invite.ID, user.ID, now)
	if err != nil {
		return domain.Member{}, err
	}
	event.ResourceType = "plan"
	event.ResourceID = invite.PlanID
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.Member{}, err
	}
	if err := insertNotification(ctx, tx, event.ID+":owner", planOwnerID, "invite_accepted", "邀请已接受", user.Username+" 已加入你的 Plan", "plan", invite.PlanID, now); err != nil {
		return domain.Member{}, err
	}
	if err := insertNotification(ctx, tx, event.ID+":member", user.ID, "plan_joined", "已加入 Plan", "创建 API Key 后即可开始使用", "plan", invite.PlanID, now); err != nil {
		return domain.Member{}, err
	}
	return member, tx.Commit(ctx)
}

func (s *Store) UpdateMemberShare(ctx context.Context, planID, ownerID, memberID string, share int, event domain.AuditEvent) (domain.Member, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Member{}, err
	}
	defer tx.Rollback(ctx)
	var actualOwner, allocationMode string
	if err = tx.QueryRow(ctx, `SELECT owner_user_id,allocation_mode FROM shared_plans WHERE id=$1 FOR UPDATE`, planID).Scan(&actualOwner, &allocationMode); err != nil {
		return domain.Member{}, mapError(err)
	}
	if actualOwner != ownerID {
		return domain.Member{}, domain.ErrForbidden
	}
	if allocationMode == domain.AllocationShared {
		if share != 0 {
			return domain.Member{}, domain.ErrInvalidInput
		}
	} else {
		if share < 1 {
			return domain.Member{}, domain.ErrInvalidInput
		}
		var others int
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT sum(share_basis_points) FROM plan_members WHERE plan_id=p.id AND status='active' AND id<>$2),0)+COALESCE((SELECT sum(share_basis_points) FROM plan_invites WHERE plan_id=p.id AND status='pending' AND expires_at>now()),0)+GREATEST(p.public_slots-(SELECT count(*) FROM plan_join_applications j JOIN plan_members jm ON jm.id=j.member_id AND jm.status='active' WHERE j.plan_id=p.id AND j.status='approved'),0)*p.public_share_basis_points FROM shared_plans p WHERE p.id=$1`, planID, memberID).Scan(&others); err != nil {
			return domain.Member{}, err
		}
		if others+share > domain.MaxShareBPS {
			return domain.Member{}, domain.ErrShareExceeded
		}
	}
	var m domain.Member
	var avatarUpdatedAt *time.Time
	err = tx.QueryRow(ctx, `UPDATE plan_members m SET share_basis_points=$3,updated_at=now() FROM users u WHERE m.id=$2 AND m.plan_id=$1 AND m.status='active' AND u.id=m.user_id RETURNING m.id,m.plan_id,m.user_id,u.username,u.email,u.avatar_updated_at,m.role,m.status,m.share_basis_points,m.created_at`, planID, memberID, share).Scan(&m.ID, &m.PlanID, &m.UserID, &m.Username, &m.Email, &avatarUpdatedAt, &m.Role, &m.Status, &m.ShareBasisPoints, &m.CreatedAt)
	if err != nil {
		return domain.Member{}, mapError(err)
	}
	m.AvatarURL = userAvatarURL(m.UserID, avatarUpdatedAt)
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.Member{}, err
	}
	if m.UserID != ownerID {
		if err := insertNotification(ctx, tx, event.ID+":member", m.UserID, "member_share_updated", "额度份额已更新", "房主调整了你在 Plan 中的额度份额", "plan", planID, event.CreatedAt); err != nil {
			return domain.Member{}, err
		}
	}
	return m, tx.Commit(ctx)
}

func (s *Store) CreateAPIKey(ctx context.Context, key domain.APIKey, routes []domain.APIKeyRoute) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO api_keys(id,user_id,name,key_prefix,key_hash,key_ciphertext,strategy,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)`, key.ID, key.UserID, key.Name, key.KeyPrefix, key.KeyHash, key.KeyCiphertext, key.Strategy, key.Status, key.CreatedAt)
	if err != nil {
		return mapError(err)
	}
	if err := insertAPIKeyRoutes(ctx, tx, key.ID, key.UserID, routes); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) UpdateAPIKey(ctx context.Context, userID string, key domain.APIKey, routes []domain.APIKeyRoute) (domain.APIKey, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `UPDATE api_keys SET name=$3,strategy=$4,updated_at=now() WHERE id=$1 AND user_id=$2 RETURNING id,user_id,name,key_prefix,key_ciphertext,strategy,status,last_used_at,created_at`, key.ID, userID, key.Name, key.Strategy).Scan(&key.ID, &key.UserID, &key.Name, &key.KeyPrefix, &key.KeyCiphertext, &key.Strategy, &key.Status, &key.LastUsedAt, &key.CreatedAt)
	if err != nil {
		return domain.APIKey{}, mapError(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM api_key_plans WHERE api_key_id=$1`, key.ID); err != nil {
		return domain.APIKey{}, err
	}
	if err := insertAPIKeyRoutes(ctx, tx, key.ID, userID, routes); err != nil {
		return domain.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.APIKey{}, err
	}
	key.Routes = routes
	return key, nil
}

func insertAPIKeyRoutes(ctx context.Context, tx pgx.Tx, keyID, userID string, routes []domain.APIKeyRoute) error {
	for _, route := range routes {
		result, err := tx.Exec(ctx, `INSERT INTO api_key_plans(api_key_id,plan_id,priority,enabled) SELECT $1,p.id,$3,$4 FROM shared_plans p JOIN plan_members m ON m.plan_id=p.id WHERE p.id=$2 AND p.status='active' AND m.user_id=$5 AND m.status='active'`, keyID, route.PlanID, route.Priority, route.Enabled, userID)
		if err != nil {
			return mapError(err)
		}
		if result.RowsAffected() != 1 {
			return domain.ErrForbidden
		}
	}
	return nil
}

func (s *Store) ListAPIKeys(ctx context.Context, userID string) ([]domain.APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,user_id,name,key_prefix,key_ciphertext,strategy,status,last_used_at,created_at FROM api_keys WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.APIKey, 0)
	for rows.Next() {
		var k domain.APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.KeyCiphertext, &k.Strategy, &k.Status, &k.LastUsedAt, &k.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		k.Routes = make([]domain.APIKeyRoute, 0)
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for index := range out {
		routes, err := s.listAPIKeyRoutes(ctx, out[index].ID)
		if err != nil {
			return nil, err
		}
		out[index].Routes = routes
	}
	return out, nil
}

func (s *Store) listAPIKeyRoutes(ctx context.Context, keyID string) ([]domain.APIKeyRoute, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.plan_id,p.name,r.priority,r.enabled FROM api_key_plans r JOIN shared_plans p ON p.id=r.plan_id WHERE r.api_key_id=$1 ORDER BY r.priority,p.name`, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.APIKeyRoute, 0)
	for rows.Next() {
		var route domain.APIKeyRoute
		if err := rows.Scan(&route.PlanID, &route.PlanName, &route.Priority, &route.Enabled); err != nil {
			return nil, err
		}
		out = append(out, route)
	}
	return out, rows.Err()
}
func (s *Store) RevokeAPIKey(ctx context.Context, userID, keyID string) error {
	r, err := s.pool.Exec(ctx, `UPDATE api_keys SET status='revoked',updated_at=now() WHERE id=$1 AND user_id=$2`, keyID, userID)
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) ResolveGatewayRoutes(ctx context.Context, hash []byte, now time.Time) (domain.GatewayRouteSet, error) {
	var out domain.GatewayRouteSet
	err := s.pool.QueryRow(ctx, `SELECT id,user_id,name,key_prefix,strategy,status,last_used_at,created_at FROM api_keys WHERE key_hash=$1 AND status='active'`, hash).Scan(&out.APIKey.ID, &out.APIKey.UserID, &out.APIKey.Name, &out.APIKey.KeyPrefix, &out.APIKey.Strategy, &out.APIKey.Status, &out.APIKey.LastUsedAt, &out.APIKey.CreatedAt)
	if err != nil {
		return out, mapError(err)
	}
	out.APIKey.Routes = make([]domain.APIKeyRoute, 0)
	out.Candidates = make([]domain.GatewayCredential, 0)
	rows, err := s.pool.Query(ctx, `
		SELECT k.id,k.strategy,r.priority,
			m.id,m.plan_id,m.user_id,u.username,u.email,m.role,m.status,m.share_basis_points,m.created_at,
			p.id,p.owner_user_id,p.account_id,p.name,p.status,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode,p.created_at,
			a.id,a.owner_user_id,a.name,a.notes,a.email,a.chatgpt_account_id,a.plan_type,a.access_token_ciphertext,a.refresh_token_ciphertext,a.proxy_url_ciphertext,a.max_concurrency,a.rpm_limit,a.token_expires_at,a.status,a.last_error,a.created_at,
			COALESCE((SELECT max(q.used_micros) FROM member_quota_windows q WHERE q.member_id=m.id AND q.account_id=a.id AND q.reset_at>$2),0),
			COALESCE((SELECT max(q.used_micros) FROM account_quota_snapshots q WHERE q.account_id=a.id AND q.reset_at>$2),0)
		FROM api_keys k
		JOIN api_key_plans r ON r.api_key_id=k.id AND r.enabled=true
		JOIN shared_plans p ON p.id=r.plan_id AND p.status='active'
		JOIN plan_members m ON m.plan_id=p.id AND m.user_id=k.user_id AND m.status='active'
		JOIN users u ON u.id=m.user_id
		JOIN openai_accounts a ON a.id=p.account_id AND a.status='active'
		WHERE k.id=$1
		ORDER BY r.priority,p.created_at`, out.APIKey.ID, now)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var credential domain.GatewayCredential
		err := rows.Scan(&credential.APIKeyID, &credential.APIKeyStrategy, &credential.RoutePriority,
			&credential.Member.ID, &credential.Member.PlanID, &credential.Member.UserID, &credential.Member.Username, &credential.Member.Email, &credential.Member.Role, &credential.Member.Status, &credential.Member.ShareBasisPoints, &credential.Member.CreatedAt,
			&credential.Plan.ID, &credential.Plan.OwnerUserID, &credential.Plan.AccountID, &credential.Plan.Name, &credential.Plan.Status, &credential.Plan.Visibility, &credential.Plan.PublicSlots, &credential.Plan.PublicShareBasisPoints, &credential.Plan.AllocationMode, &credential.Plan.CreatedAt,
			&credential.Account.ID, &credential.Account.OwnerUserID, &credential.Account.Name, &credential.Account.Notes, &credential.Account.Email, &credential.Account.ChatGPTAccountID, &credential.Account.PlanType, &credential.AccessTokenCiphertext, &credential.RefreshTokenCiphertext, &credential.ProxyURLCiphertext, &credential.Account.MaxConcurrency, &credential.Account.RPMLimit, &credential.TokenExpiresAt, &credential.Account.Status, &credential.Account.LastError, &credential.Account.CreatedAt,
			&credential.UsageMicros, &credential.AccountUsageMicros)
		if err != nil {
			return out, err
		}
		out.Candidates = append(out.Candidates, credential)
	}
	return out, rows.Err()
}

func (s *Store) TouchAPIKey(ctx context.Context, keyID string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=$2,updated_at=$2 WHERE id=$1`, keyID, now)
	return err
}

func (s *Store) MemberQuotaExhausted(ctx context.Context, memberID, accountID string, shareBPS int, now time.Time) (bool, error) {
	var exhausted bool
	limit := int64(shareBPS) * 10_000
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member_quota_windows WHERE member_id=$1 AND account_id=$2 AND reset_at>$3 AND used_micros >= $4)`, memberID, accountID, now, limit).Scan(&exhausted)
	return exhausted, err
}

func (s *Store) AccountQuotaExhausted(ctx context.Context, accountID string, now time.Time) (bool, error) {
	var exhausted bool
	limit := int64(domain.MaxShareBPS) * 10_000
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_quota_snapshots WHERE account_id=$1 AND reset_at>$2 AND used_micros >= $3)`, accountID, now, limit).Scan(&exhausted)
	return exhausted, err
}

func (s *Store) RecordAccountQuotaSignals(ctx context.Context, accountID string, signals []domain.QuotaSignal, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, signal := range signals {
		_, err = tx.Exec(ctx, `INSERT INTO account_quota_snapshots(account_id,window_type,window_start,reset_at,used_micros,updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(account_id,window_type) DO UPDATE SET window_start=EXCLUDED.window_start,reset_at=EXCLUDED.reset_at,used_micros=EXCLUDED.used_micros,updated_at=EXCLUDED.updated_at`, accountID, signal.WindowType, signal.WindowStart, signal.ResetAt, signal.AccountUsedMicros, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) RecordQuotaSignals(ctx context.Context, accountID, memberID string, signals []domain.QuotaSignal, requestID string, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, signal := range signals {
		var oldUsed int64
		var oldStart time.Time
		err = tx.QueryRow(ctx, `SELECT used_micros,window_start FROM account_quota_snapshots WHERE account_id=$1 AND window_type=$2 FOR UPDATE`, accountID, signal.WindowType).Scan(&oldUsed, &oldStart)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		delta := int64(0)
		if err == nil && oldStart.Equal(signal.WindowStart) && signal.AccountUsedMicros > oldUsed {
			delta = signal.AccountUsedMicros - oldUsed
		}
		_, err = tx.Exec(ctx, `INSERT INTO account_quota_snapshots(account_id,window_type,window_start,reset_at,used_micros,updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(account_id,window_type) DO UPDATE SET window_start=EXCLUDED.window_start,reset_at=EXCLUDED.reset_at,used_micros=EXCLUDED.used_micros,updated_at=EXCLUDED.updated_at`, accountID, signal.WindowType, signal.WindowStart, signal.ResetAt, signal.AccountUsedMicros, now)
		if err != nil {
			return err
		}
		if requestID == "" {
			continue
		}
		eventID := requestID + ":" + signal.WindowType
		tag, err := tx.Exec(ctx, `INSERT INTO quota_usage_events(id,member_id,account_id,window_type,window_start,request_id,delta_micros,account_used_micros,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(member_id,account_id,window_type,window_start,request_id) DO NOTHING`, eventID, memberID, accountID, signal.WindowType, signal.WindowStart, requestID, delta, signal.AccountUsedMicros, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		_, err = tx.Exec(ctx, `INSERT INTO member_quota_windows(member_id,account_id,window_type,window_start,reset_at,used_micros,account_used_micros,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(member_id,account_id,window_type,window_start) DO UPDATE SET reset_at=EXCLUDED.reset_at,used_micros=member_quota_windows.used_micros+EXCLUDED.used_micros,account_used_micros=EXCLUDED.account_used_micros,updated_at=EXCLUDED.updated_at`, memberID, accountID, signal.WindowType, signal.WindowStart, signal.ResetAt, delta, signal.AccountUsedMicros, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) RecordGatewayMetric(ctx context.Context, metric domain.GatewayMetric) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO gateway_request_metrics(request_id,api_key_id,plan_id,account_id,member_id,model,service_tier,status_code,ttft_ms,duration_ms,input_tokens,output_tokens,cached_tokens,estimated_cost_micros,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, metric.RequestID, metric.APIKeyID, metric.PlanID, metric.AccountID, metric.MemberID, metric.Model, metric.ServiceTier, metric.StatusCode, metric.TTFT.Milliseconds(), metric.Duration.Milliseconds(), metric.TokenUsage.InputTokens, metric.TokenUsage.OutputTokens, metric.TokenUsage.CachedTokens, metric.AccountCostMicros, metric.CreatedAt)
	return err
}

func (s *Store) Dashboard(ctx context.Context, userID string, todayStart, trendStart, now time.Time) (domain.Dashboard, error) {
	var out domain.Dashboard
	var successToday int64
	err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(g.input_tokens) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(SUM(g.output_tokens) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(SUM(g.cached_tokens) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(SUM(g.input_tokens), 0),
			COALESCE(SUM(g.output_tokens), 0),
			COALESCE(SUM(g.cached_tokens), 0),
			COUNT(*) FILTER (WHERE g.created_at >= $2),
			COUNT(*) FILTER (WHERE g.created_at >= $2 AND g.status_code BETWEEN 200 AND 299),
			COALESCE(AVG(g.ttft_ms) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(AVG(g.duration_ms) FILTER (WHERE g.created_at >= $2), 0),
			COUNT(DISTINCT g.plan_id) FILTER (WHERE g.created_at >= $2),
			COUNT(*) FILTER (WHERE g.created_at >= $3::timestamptz - INTERVAL '1 minute'),
			COALESCE(SUM(g.input_tokens + g.output_tokens) FILTER (WHERE g.created_at >= $3::timestamptz - INTERVAL '1 minute'), 0)
		FROM gateway_request_metrics g
		JOIN plan_members m ON m.id = g.member_id
		WHERE m.user_id = $1 AND g.created_at <= $3::timestamptz`, userID, todayStart, now).Scan(
		&out.TodayTokens.InputTokens,
		&out.TodayTokens.OutputTokens,
		&out.TodayTokens.CachedTokens,
		&out.TotalTokens.InputTokens,
		&out.TotalTokens.OutputTokens,
		&out.TotalTokens.CachedTokens,
		&out.Performance.RequestsToday,
		&successToday,
		&out.Performance.AverageTTFTMs,
		&out.Performance.AverageDurationMs,
		&out.Performance.ActivePlans,
		&out.Performance.RequestsPerMinute,
		&out.Performance.TokensPerMinute,
	)
	if err != nil {
		return out, err
	}
	out.TodayTokens.TotalTokens = out.TodayTokens.InputTokens + out.TodayTokens.OutputTokens
	out.TotalTokens.TotalTokens = out.TotalTokens.InputTokens + out.TotalTokens.OutputTokens
	if out.Performance.RequestsToday > 0 {
		out.Performance.SuccessRate = float64(successToday) / float64(out.Performance.RequestsToday) * 100
	}

	rows, err := s.pool.Query(ctx, `
		WITH buckets AS (
			SELECT generate_series($2::timestamptz, $2::timestamptz + INTERVAL '23 hours', INTERVAL '1 hour') AS bucket_start
		), usage AS (
			SELECT
				date_trunc('hour', g.created_at - $2::timestamptz) + $2::timestamptz AS bucket_start,
				SUM(g.input_tokens) AS input_tokens,
				SUM(g.output_tokens) AS output_tokens,
				SUM(g.cached_tokens) AS cached_tokens
			FROM gateway_request_metrics g
			JOIN plan_members m ON m.id = g.member_id
			WHERE m.user_id = $1 AND g.created_at >= $2::timestamptz AND g.created_at <= $3::timestamptz
			GROUP BY 1
		)
		SELECT b.bucket_start, COALESCE(u.input_tokens, 0), COALESCE(u.output_tokens, 0), COALESCE(u.cached_tokens, 0)
		FROM buckets b
		LEFT JOIN usage u ON u.bucket_start = b.bucket_start
		ORDER BY b.bucket_start`, userID, trendStart, now)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Trend = make([]domain.DashboardTrendPoint, 0, 24)
	for rows.Next() {
		var point domain.DashboardTrendPoint
		if err := rows.Scan(&point.BucketStart, &point.InputTokens, &point.OutputTokens, &point.CachedTokens); err != nil {
			return out, err
		}
		out.Trend = append(out.Trend, point)
	}
	return out, rows.Err()
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return domain.ErrConflict
		case "23503", "23514":
			return domain.ErrInvalidInput
		case "40001":
			return domain.ErrConflict
		}
	}
	return err
}
