CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_idx ON users(lower(email));

CREATE TABLE user_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX user_sessions_user_id_idx ON user_sessions(user_id);

CREATE TABLE oauth_flows (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    state_hash BYTEA NOT NULL UNIQUE,
    code_verifier TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE openai_accounts (
    id TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL REFERENCES users(id),
    email TEXT NOT NULL,
    chatgpt_account_id TEXT NOT NULL,
    plan_type TEXT NOT NULL DEFAULT '',
    access_token_ciphertext BYTEA NOT NULL,
    refresh_token_ciphertext BYTEA NOT NULL,
    token_expires_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'refresh_required', 'disabled')),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, chatgpt_account_id)
);

CREATE TABLE shared_plans (
    id TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL REFERENCES users(id),
    account_id TEXT NOT NULL REFERENCES openai_accounts(id),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, name),
    UNIQUE (id, owner_user_id)
);

CREATE TABLE plan_members (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES shared_plans(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id),
    role TEXT NOT NULL CHECK (role IN ('owner', 'member')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    share_basis_points INTEGER NOT NULL CHECK (share_basis_points BETWEEN 1 AND 10000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (plan_id, user_id)
);

CREATE TABLE plan_invites (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES shared_plans(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE,
    share_basis_points INTEGER NOT NULL CHECK (share_basis_points BETWEEN 1 AND 10000),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'revoked', 'expired')),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_by_user_id TEXT REFERENCES users(id),
    accepted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX plan_invites_pending_email_idx
    ON plan_invites(plan_id, lower(email)) WHERE status = 'pending';

CREATE TABLE member_api_keys (
    id TEXT PRIMARY KEY,
    member_id TEXT NOT NULL REFERENCES plan_members(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    key_prefix TEXT NOT NULL,
    key_hash BYTEA NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX member_api_keys_user_id_idx ON member_api_keys(user_id);

CREATE TABLE member_quota_windows (
    member_id TEXT NOT NULL REFERENCES plan_members(id) ON DELETE CASCADE,
    window_type TEXT NOT NULL CHECK (window_type IN ('5h', '7d')),
    window_start TIMESTAMPTZ NOT NULL,
    reset_at TIMESTAMPTZ NOT NULL,
    used_micros BIGINT NOT NULL DEFAULT 0 CHECK (used_micros >= 0),
    account_used_micros BIGINT NOT NULL CHECK (account_used_micros BETWEEN 0 AND 100000000),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (member_id, window_type, window_start)
);

CREATE TABLE quota_usage_events (
    id TEXT PRIMARY KEY,
    member_id TEXT NOT NULL REFERENCES plan_members(id) ON DELETE CASCADE,
    window_type TEXT NOT NULL CHECK (window_type IN ('5h', '7d')),
    window_start TIMESTAMPTZ NOT NULL,
    request_id TEXT NOT NULL,
    delta_micros BIGINT NOT NULL CHECK (delta_micros >= 0),
    account_used_micros BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (member_id, window_type, window_start, request_id)
);

CREATE TABLE account_quota_snapshots (
    account_id TEXT NOT NULL REFERENCES openai_accounts(id) ON DELETE CASCADE,
    window_type TEXT NOT NULL CHECK (window_type IN ('5h', '7d')),
    window_start TIMESTAMPTZ NOT NULL,
    reset_at TIMESTAMPTZ NOT NULL,
    used_micros BIGINT NOT NULL CHECK (used_micros BETWEEN 0 AND 100000000),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, window_type)
);

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    actor_user_id TEXT REFERENCES users(id),
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_resource_idx ON audit_events(resource_type, resource_id, created_at DESC);
