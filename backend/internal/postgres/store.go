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
