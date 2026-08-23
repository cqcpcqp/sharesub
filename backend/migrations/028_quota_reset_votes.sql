CREATE TABLE quota_reset_votes (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES shared_plans(id) ON DELETE CASCADE,
    account_id TEXT NOT NULL,
    account_binding_generation BIGINT NOT NULL CHECK (account_binding_generation >= 0),
    initiator_member_id TEXT NOT NULL,
    initiator_user_id TEXT NOT NULL,
    allocation_mode TEXT NOT NULL CHECK (allocation_mode IN ('fixed', 'shared')),
    status TEXT NOT NULL CHECK (status IN ('active', 'executing', 'succeeded', 'succeeded_unsynced', 'expired', 'cancelled', 'outcome_unknown')),
    eligible_count INTEGER NOT NULL CHECK (eligible_count > 0),
    eligible_weight_basis_points INTEGER NOT NULL CHECK (eligible_weight_basis_points BETWEEN 0 AND 10000),
    windows_reset INTEGER NOT NULL DEFAULT 0 CHECK (windows_reset >= 0),
    result_code TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    execution_started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CHECK (expires_at > created_at)
);

CREATE UNIQUE INDEX quota_reset_votes_one_open_per_plan_idx
    ON quota_reset_votes(plan_id)
    WHERE status IN ('active', 'executing');
CREATE INDEX quota_reset_votes_plan_time_idx
    ON quota_reset_votes(plan_id, created_at DESC);

CREATE TABLE quota_reset_vote_members (
    vote_id TEXT NOT NULL REFERENCES quota_reset_votes(id) ON DELETE CASCADE,
    member_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    username TEXT NOT NULL,
    avatar_url TEXT NOT NULL DEFAULT '',
    weight_basis_points INTEGER NOT NULL CHECK (weight_basis_points BETWEEN 0 AND 10000),
    choice TEXT CHECK (choice IN ('support', 'oppose')),
    voted_at TIMESTAMPTZ,
    PRIMARY KEY (vote_id, member_id),
    UNIQUE (vote_id, user_id)
);

CREATE INDEX quota_reset_vote_members_vote_choice_idx
    ON quota_reset_vote_members(vote_id, choice);

CREATE TABLE quota_reset_execution_leases (
    plan_id TEXT PRIMARY KEY REFERENCES shared_plans(id) ON DELETE CASCADE,
    operation_id TEXT NOT NULL UNIQUE,
    account_id TEXT NOT NULL,
    account_binding_generation BIGINT NOT NULL CHECK (account_binding_generation >= 0),
    vote_id TEXT REFERENCES quota_reset_votes(id) ON DELETE CASCADE,
    acquired_at TIMESTAMPTZ NOT NULL
);
