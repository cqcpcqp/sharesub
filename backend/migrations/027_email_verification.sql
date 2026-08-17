ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;

-- Accounts created before email verification existed have already been able to
-- sign in, so preserve that behavior and treat their stored address as verified.
UPDATE users SET email_verified_at = created_at;

CREATE TABLE email_verification_tokens (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX email_verification_tokens_user_created_idx
    ON email_verification_tokens(user_id, created_at DESC);
CREATE INDEX email_verification_tokens_expiry_idx
    ON email_verification_tokens(expires_at);
