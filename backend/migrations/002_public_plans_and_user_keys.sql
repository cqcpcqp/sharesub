ALTER TABLE users ADD COLUMN username TEXT;
UPDATE users SET username = 'user_' || left(id, 12);
ALTER TABLE users ALTER COLUMN username SET NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_username_length CHECK (char_length(username) BETWEEN 2 AND 32);
CREATE UNIQUE INDEX users_username_idx ON users(lower(username));

ALTER TABLE shared_plans
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'public')),
    ADD COLUMN public_slots INTEGER NOT NULL DEFAULT 0 CHECK (public_slots BETWEEN 0 AND 100),
    ADD COLUMN public_share_basis_points INTEGER NOT NULL DEFAULT 0 CHECK (public_share_basis_points BETWEEN 0 AND 10000);

CREATE TABLE plan_join_applications (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES shared_plans(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message TEXT NOT NULL DEFAULT '' CHECK (char_length(message) <= 500),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    member_id TEXT REFERENCES plan_members(id) ON DELETE SET NULL,
    reviewed_by_user_id TEXT REFERENCES users(id),
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX plan_join_applications_pending_idx
    ON plan_join_applications(plan_id, user_id) WHERE status = 'pending';
CREATE INDEX plan_join_applications_owner_idx ON plan_join_applications(plan_id, created_at DESC);

CREATE TABLE api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    key_prefix TEXT NOT NULL,
    key_hash BYTEA NOT NULL UNIQUE,
    strategy TEXT NOT NULL DEFAULT 'balanced' CHECK (strategy IN ('priority', 'balanced')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_key_plans (
    api_key_id TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    plan_id TEXT NOT NULL REFERENCES shared_plans(id) ON DELETE CASCADE,
    priority INTEGER NOT NULL DEFAULT 100 CHECK (priority BETWEEN 1 AND 10000),
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (api_key_id, plan_id)
);

INSERT INTO api_keys(id,user_id,name,key_prefix,key_hash,status,last_used_at,created_at,updated_at)
SELECT id,user_id,name,key_prefix,key_hash,status,last_used_at,created_at,created_at
FROM member_api_keys;

INSERT INTO api_key_plans(api_key_id,plan_id,priority,enabled,created_at)
SELECT k.id,m.plan_id,100,true,k.created_at
FROM member_api_keys k
JOIN plan_members m ON m.id=k.member_id;

DROP TABLE member_api_keys;
CREATE INDEX api_keys_user_id_idx ON api_keys(user_id, created_at DESC);
CREATE INDEX api_key_plans_plan_id_idx ON api_key_plans(plan_id);

CREATE TABLE gateway_request_metrics (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    api_key_id TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    plan_id TEXT NOT NULL REFERENCES shared_plans(id) ON DELETE CASCADE,
    member_id TEXT NOT NULL REFERENCES plan_members(id) ON DELETE CASCADE,
    status_code INTEGER NOT NULL CHECK (status_code BETWEEN 100 AND 599),
    ttft_ms BIGINT NOT NULL CHECK (ttft_ms >= 0),
    duration_ms BIGINT NOT NULL CHECK (duration_ms >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX gateway_request_metrics_plan_time_idx ON gateway_request_metrics(plan_id, created_at DESC);
CREATE INDEX gateway_request_metrics_member_time_idx ON gateway_request_metrics(member_id, created_at DESC);
