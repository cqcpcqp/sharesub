package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
	if user.Role == "" {
		user.Role = domain.RoleUser
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO users(id,username,email,password_hash,status,role,must_change_password,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8)`, user.ID, user.Username, user.Email, user.PasswordHash, user.Status, user.Role, user.MustChangePassword, user.CreatedAt)
	return mapError(err)
}

func (s *Store) CreateUserWithAgreement(ctx context.Context, user domain.User, acceptance domain.AgreementAcceptance) error {
	if user.Role == "" {
		user.Role = domain.RoleUser
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO users(id,username,email,password_hash,status,role,must_change_password,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8)`, user.ID, user.Username, user.Email, user.PasswordHash, user.Status, user.Role, user.MustChangePassword, user.CreatedAt); err != nil {
		return mapError(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_agreement_acceptances(user_id,terms_version,privacy_policy_version,acceptable_use_version,accepted_at) VALUES($1,$2,$3,$4,$5)`, user.ID, acceptance.TermsVersion, acceptance.PrivacyPolicyVersion, acceptance.AcceptableUseVersion, acceptance.AcceptedAt); err != nil {
		return mapError(err)
	}
	return tx.Commit(ctx)
}

func (s *Store) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT id,username,email,password_hash,status,role,must_change_password,created_at,avatar_updated_at FROM users WHERE lower(email)=lower($1)`, email))
}

func (s *Store) UserBySessionHash(ctx context.Context, hash []byte, now time.Time) (domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT u.id,u.username,u.email,u.password_hash,u.status,u.role,u.must_change_password,u.created_at,u.avatar_updated_at FROM user_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>$2 AND u.status='active'`, hash, now))
}

func scanUser(row pgx.Row) (domain.User, error) {
	var user domain.User
	var avatarUpdatedAt *time.Time
	err := row.Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.Status, &user.Role, &user.MustChangePassword, &user.CreatedAt, &avatarUpdatedAt)
	user.AvatarURL = userAvatarURL(user.ID, avatarUpdatedAt)
	return user, mapError(err)
}

func (s *Store) UpdateUsername(ctx context.Context, userID, username string) (domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `UPDATE users SET username=$2,updated_at=now() WHERE id=$1 RETURNING id,username,email,password_hash,status,role,must_change_password,created_at,avatar_updated_at`, userID, username))
}

func (s *Store) UpdateUserAvatar(ctx context.Context, userID string, avatar domain.UserAvatar, updatedAt time.Time) (domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `UPDATE users SET avatar_data=$2,avatar_media_type=$3,avatar_updated_at=$4,updated_at=$4 WHERE id=$1 RETURNING id,username,email,password_hash,status,role,must_change_password,created_at,avatar_updated_at`, userID, avatar.Data, avatar.MediaType, updatedAt))
}

func (s *Store) DeleteUserAvatar(ctx context.Context, userID string) (domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `UPDATE users SET avatar_data=NULL,avatar_media_type=NULL,avatar_updated_at=NULL,updated_at=now() WHERE id=$1 RETURNING id,username,email,password_hash,status,role,must_change_password,created_at,avatar_updated_at`, userID))
}

func (s *Store) UpdatePassword(ctx context.Context, userID, passwordHash string, mustChange bool, currentSessionHash []byte) (domain.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)
	user, err := scanUser(tx.QueryRow(ctx, `UPDATE users SET password_hash=$2,must_change_password=$3,updated_at=now() WHERE id=$1 RETURNING id,username,email,password_hash,status,role,must_change_password,created_at,avatar_updated_at`, userID, passwordHash, mustChange))
	if err != nil {
		return domain.User{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_sessions WHERE user_id=$1 AND token_hash<>$2`, userID, currentSessionHash); err != nil {
		return domain.User{}, err
	}
	return user, tx.Commit(ctx)
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
	if account.FastPolicy == nil {
		account.FastPolicy = make([]domain.FastPolicyRule, 0)
	}
	var out domain.Account
	err := s.pool.QueryRow(ctx, `
		INSERT INTO openai_accounts(id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,subscription_expires_at,access_token_ciphertext,refresh_token_ciphertext,proxy_url_ciphertext,max_concurrency,rpm_limit,fast_policy,token_expires_at,status,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17)
		ON CONFLICT(owner_user_id,chatgpt_account_id) DO UPDATE SET name=EXCLUDED.name,notes=EXCLUDED.notes,email=EXCLUDED.email,plan_type=EXCLUDED.plan_type,subscription_expires_at=EXCLUDED.subscription_expires_at,access_token_ciphertext=EXCLUDED.access_token_ciphertext,refresh_token_ciphertext=EXCLUDED.refresh_token_ciphertext,proxy_url_ciphertext=EXCLUDED.proxy_url_ciphertext,max_concurrency=EXCLUDED.max_concurrency,rpm_limit=EXCLUDED.rpm_limit,fast_policy=EXCLUDED.fast_policy,token_expires_at=EXCLUDED.token_expires_at,status='active',last_error='',updated_at=now()
		RETURNING id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,subscription_expires_at,access_token_ciphertext,refresh_token_ciphertext,proxy_url_ciphertext,max_concurrency,rpm_limit,fast_policy,token_expires_at,status,last_error,created_at`,
		account.ID, account.OwnerUserID, account.Name, account.Notes, account.Email, account.ChatGPTAccountID, account.PlanType, account.SubscriptionExpiresAt, account.AccessTokenCiphertext, account.RefreshTokenCiphertext, account.ProxyURLCiphertext, account.MaxConcurrency, account.RPMLimit, account.FastPolicy, account.TokenExpiresAt, account.Status, account.CreatedAt,
	).Scan(&out.ID, &out.OwnerUserID, &out.Name, &out.Notes, &out.Email, &out.ChatGPTAccountID, &out.PlanType, &out.SubscriptionExpiresAt, &out.AccessTokenCiphertext, &out.RefreshTokenCiphertext, &out.ProxyURLCiphertext, &out.MaxConcurrency, &out.RPMLimit, &out.FastPolicy, &out.TokenExpiresAt, &out.Status, &out.LastError, &out.CreatedAt)
	return out, mapError(err)
}

func (s *Store) ListAccounts(ctx context.Context, userID string) ([]domain.Account, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,subscription_expires_at,proxy_url_ciphertext,max_concurrency,rpm_limit,fast_policy,token_expires_at,status,last_error,created_at FROM openai_accounts WHERE owner_user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Account, 0)
	for rows.Next() {
		var a domain.Account
		if err := rows.Scan(&a.ID, &a.OwnerUserID, &a.Name, &a.Notes, &a.Email, &a.ChatGPTAccountID, &a.PlanType, &a.SubscriptionExpiresAt, &a.ProxyURLCiphertext, &a.MaxConcurrency, &a.RPMLimit, &a.FastPolicy, &a.TokenExpiresAt, &a.Status, &a.LastError, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) AccountByID(ctx context.Context, id string) (domain.Account, error) {
	var a domain.Account
	err := s.pool.QueryRow(ctx, `SELECT id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,subscription_expires_at,access_token_ciphertext,refresh_token_ciphertext,proxy_url_ciphertext,max_concurrency,rpm_limit,fast_policy,token_expires_at,status,last_error,created_at FROM openai_accounts WHERE id=$1`, id).Scan(&a.ID, &a.OwnerUserID, &a.Name, &a.Notes, &a.Email, &a.ChatGPTAccountID, &a.PlanType, &a.SubscriptionExpiresAt, &a.AccessTokenCiphertext, &a.RefreshTokenCiphertext, &a.ProxyURLCiphertext, &a.MaxConcurrency, &a.RPMLimit, &a.FastPolicy, &a.TokenExpiresAt, &a.Status, &a.LastError, &a.CreatedAt)
	return a, mapError(err)
}

func (s *Store) UpdateAccountConfig(ctx context.Context, userID string, account domain.Account) (domain.Account, error) {
	if account.FastPolicy == nil {
		account.FastPolicy = make([]domain.FastPolicyRule, 0)
	}
	var out domain.Account
	err := s.pool.QueryRow(ctx, `UPDATE openai_accounts SET name=$3,notes=$4,proxy_url_ciphertext=$5,max_concurrency=$6,rpm_limit=$7,fast_policy=$8,status=$9,last_error=CASE WHEN $9='active' THEN '' ELSE last_error END,updated_at=now() WHERE id=$1 AND owner_user_id=$2 RETURNING id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,subscription_expires_at,proxy_url_ciphertext,max_concurrency,rpm_limit,fast_policy,token_expires_at,status,last_error,created_at`, account.ID, userID, account.Name, account.Notes, account.ProxyURLCiphertext, account.MaxConcurrency, account.RPMLimit, account.FastPolicy, account.Status).Scan(&out.ID, &out.OwnerUserID, &out.Name, &out.Notes, &out.Email, &out.ChatGPTAccountID, &out.PlanType, &out.SubscriptionExpiresAt, &out.ProxyURLCiphertext, &out.MaxConcurrency, &out.RPMLimit, &out.FastPolicy, &out.TokenExpiresAt, &out.Status, &out.LastError, &out.CreatedAt)
	return out, mapError(err)
}

func (s *Store) UpdateAccountTokensIfRefreshTokenUnchanged(ctx context.Context, id string, expectedRefresh, access, refresh []byte, expiresAt time.Time) (bool, error) {
	result, err := s.pool.Exec(ctx, `UPDATE openai_accounts SET access_token_ciphertext=$3,refresh_token_ciphertext=$4,token_expires_at=$5,last_error='',updated_at=now() WHERE id=$1 AND refresh_token_ciphertext=$2 AND status='active'`, id, expectedRefresh, access, refresh, expiresAt)
	if err != nil {
		return false, mapError(err)
	}
	return result.RowsAffected() == 1, nil
}

func (s *Store) UpdateAccountSubscriptionExpiresAtIfRefreshTokenUnchanged(ctx context.Context, id string, expectedRefresh []byte, subscriptionExpiresAt *time.Time) (bool, error) {
	result, err := s.pool.Exec(ctx, `UPDATE openai_accounts SET subscription_expires_at=$3,updated_at=now() WHERE id=$1 AND refresh_token_ciphertext=$2 AND status='active'`, id, expectedRefresh, subscriptionExpiresAt)
	if err != nil {
		return false, mapError(err)
	}
	return result.RowsAffected() == 1, nil
}

func (s *Store) ListExpiringAccounts(ctx context.Context, expiresBefore time.Time, limit int) ([]domain.Account, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,subscription_expires_at,access_token_ciphertext,refresh_token_ciphertext,proxy_url_ciphertext,max_concurrency,rpm_limit,fast_policy,token_expires_at,status,last_error,created_at FROM openai_accounts WHERE status='active' AND token_expires_at<$1 ORDER BY token_expires_at,id LIMIT $2`, expiresBefore, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := make([]domain.Account, 0)
	for rows.Next() {
		var account domain.Account
		if err := rows.Scan(&account.ID, &account.OwnerUserID, &account.Name, &account.Notes, &account.Email, &account.ChatGPTAccountID, &account.PlanType, &account.SubscriptionExpiresAt, &account.AccessTokenCiphertext, &account.RefreshTokenCiphertext, &account.ProxyURLCiphertext, &account.MaxConcurrency, &account.RPMLimit, &account.FastPolicy, &account.TokenExpiresAt, &account.Status, &account.LastError, &account.CreatedAt); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) TryAcquireAccountRefreshLease(ctx context.Context, accountID, holderID string, expiresAt time.Time) (bool, error) {
	var acquired string
	err := s.pool.QueryRow(ctx, `INSERT INTO account_token_refresh_leases(account_id,holder_id,expires_at) VALUES($1,$2,$3) ON CONFLICT(account_id) DO UPDATE SET holder_id=EXCLUDED.holder_id,expires_at=EXCLUDED.expires_at WHERE account_token_refresh_leases.expires_at<=now() RETURNING holder_id`, accountID, holderID, expiresAt).Scan(&acquired)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, mapError(err)
	}
	return acquired == holderID, nil
}

func (s *Store) ReleaseAccountRefreshLease(ctx context.Context, accountID, holderID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM account_token_refresh_leases WHERE account_id=$1 AND holder_id=$2`, accountID, holderID)
	return err
}

func (s *Store) MarkAccountErrorIfRefreshTokenUnchanged(ctx context.Context, id string, expectedRefresh []byte, message string) (bool, error) {
	result, err := s.pool.Exec(ctx, `UPDATE openai_accounts SET status=$3,last_error=$4,updated_at=now() WHERE id=$1 AND refresh_token_ciphertext=$2 AND status='active'`, id, expectedRefresh, domain.StatusRefreshRequired, message)
	if err != nil {
		return false, mapError(err)
	}
	return result.RowsAffected() == 1, nil
}
