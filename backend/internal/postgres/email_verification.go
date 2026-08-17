package postgres

import (
	"context"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Store) CreateUserWithEmailVerification(ctx context.Context, user domain.User, acceptance domain.AgreementAcceptance, verification domain.EmailVerificationToken) error {
	if user.Role == "" {
		user.Role = domain.RoleUser
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO users(id,username,email,password_hash,status,role,must_change_password,email_verified_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,NULL,$8,$8)`, user.ID, user.Username, user.Email, user.PasswordHash, user.Status, user.Role, user.MustChangePassword, user.CreatedAt); err != nil {
		return mapError(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_agreement_acceptances(user_id,terms_version,privacy_policy_version,acceptable_use_version,accepted_at) VALUES($1,$2,$3,$4,$5)`, acceptance.UserID, acceptance.TermsVersion, acceptance.PrivacyPolicyVersion, acceptance.AcceptableUseVersion, acceptance.AcceptedAt); err != nil {
		return mapError(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO email_verification_tokens(id,user_id,token_hash,expires_at,created_at) VALUES($1,$2,$3,$4,$5)`, verification.ID, verification.UserID, verification.TokenHash, verification.ExpiresAt, verification.CreatedAt); err != nil {
		return mapError(err)
	}
	return tx.Commit(ctx)
}

func (s *Store) CreateEmailVerificationToken(ctx context.Context, verification domain.EmailVerificationToken, cooldown, limitWindow time.Duration, limit int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var verifiedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT email_verified_at FROM users WHERE id=$1 FOR UPDATE`, verification.UserID).Scan(&verifiedAt); err != nil {
		return mapError(err)
	}
	if verifiedAt != nil {
		return domain.ErrConflict
	}
	var lastSentAt *time.Time
	var sendsInWindow int
	if err := tx.QueryRow(ctx, `SELECT max(created_at),count(*) FILTER (WHERE created_at>$2) FROM email_verification_tokens WHERE user_id=$1`, verification.UserID, verification.CreatedAt.Add(-limitWindow)).Scan(&lastSentAt, &sendsInWindow); err != nil {
		return err
	}
	if lastSentAt != nil && verification.CreatedAt.Before(lastSentAt.Add(cooldown)) {
		return domain.ErrEmailResendTooSoon
	}
	if sendsInWindow >= limit {
		return domain.ErrEmailVerificationLimited
	}
	if _, err := tx.Exec(ctx, `INSERT INTO email_verification_tokens(id,user_id,token_hash,expires_at,created_at) VALUES($1,$2,$3,$4,$5)`, verification.ID, verification.UserID, verification.TokenHash, verification.ExpiresAt, verification.CreatedAt); err != nil {
		return mapError(err)
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteEmailVerificationToken(ctx context.Context, verificationID, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM email_verification_tokens WHERE id=$1 AND user_id=$2`, verificationID, userID)
	return err
}

func (s *Store) SupersedeEmailVerificationTokens(ctx context.Context, userID, currentVerificationID string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE email_verification_tokens SET consumed_at=$3 WHERE user_id=$1 AND id<>$2 AND consumed_at IS NULL`, userID, currentVerificationID, now)
	return err
}

func (s *Store) ConsumeEmailVerificationToken(ctx context.Context, tokenHash []byte, now time.Time) (domain.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)
	var verificationID, userID string
	if err := tx.QueryRow(ctx, `SELECT id,user_id FROM email_verification_tokens WHERE token_hash=$1 AND consumed_at IS NULL AND expires_at>$2 FOR UPDATE`, tokenHash, now).Scan(&verificationID, &userID); err != nil {
		return domain.User{}, mapError(err)
	}
	user, err := scanUser(tx.QueryRow(ctx, `UPDATE users SET email_verified_at=COALESCE(email_verified_at,$2),updated_at=$2 WHERE id=$1 AND status='active' RETURNING id,username,email,password_hash,status,role,must_change_password,email_verified_at,created_at,avatar_updated_at`, userID, now))
	if err != nil {
		return domain.User{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE email_verification_tokens SET consumed_at=$2 WHERE user_id=$1 AND consumed_at IS NULL`, userID, now); err != nil {
		return domain.User{}, err
	}
	return user, tx.Commit(ctx)
}
