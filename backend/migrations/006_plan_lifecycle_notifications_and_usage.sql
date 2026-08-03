ALTER TABLE shared_plans
    DROP CONSTRAINT shared_plans_status_check,
    ADD CONSTRAINT shared_plans_status_check CHECK (status IN ('active', 'disabled', 'archived')),
    ADD COLUMN archived_at TIMESTAMPTZ;

ALTER TABLE plan_members
    ADD COLUMN removed_at TIMESTAMPTZ;

CREATE UNIQUE INDEX plan_members_active_owner_idx
    ON plan_members(plan_id)
    WHERE role = 'owner' AND status = 'active';

ALTER TABLE plan_invites
    ADD COLUMN revoked_at TIMESTAMPTZ,
    ADD COLUMN revoked_by_user_id TEXT REFERENCES users(id);

ALTER TABLE oauth_flows
    ADD COLUMN purpose TEXT NOT NULL DEFAULT 'connect' CHECK (purpose IN ('connect', 'reauthorize')),
    ADD COLUMN target_account_id TEXT REFERENCES openai_accounts(id) ON DELETE CASCADE;

ALTER TABLE member_quota_windows ADD COLUMN account_id TEXT REFERENCES openai_accounts(id);
UPDATE member_quota_windows q
SET account_id = p.account_id
FROM plan_members m
JOIN shared_plans p ON p.id = m.plan_id
WHERE m.id = q.member_id;
ALTER TABLE member_quota_windows ALTER COLUMN account_id SET NOT NULL;
ALTER TABLE member_quota_windows DROP CONSTRAINT member_quota_windows_pkey;
ALTER TABLE member_quota_windows
    ADD PRIMARY KEY (member_id, account_id, window_type, window_start);

ALTER TABLE quota_usage_events ADD COLUMN account_id TEXT REFERENCES openai_accounts(id);
UPDATE quota_usage_events q
SET account_id = p.account_id
FROM plan_members m
JOIN shared_plans p ON p.id = m.plan_id
WHERE m.id = q.member_id;
ALTER TABLE quota_usage_events ALTER COLUMN account_id SET NOT NULL;
ALTER TABLE quota_usage_events
    DROP CONSTRAINT quota_usage_events_member_id_window_type_window_start_reque_key;
ALTER TABLE quota_usage_events
    ADD CONSTRAINT quota_usage_events_member_account_window_request_key
    UNIQUE (member_id, account_id, window_type, window_start, request_id);

ALTER TABLE gateway_request_metrics
    ADD COLUMN account_id TEXT REFERENCES openai_accounts(id),
    ADD COLUMN estimated_cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (estimated_cost_micros >= 0);
UPDATE gateway_request_metrics g
SET account_id = p.account_id
FROM shared_plans p
WHERE p.id = g.plan_id;
ALTER TABLE gateway_request_metrics ALTER COLUMN account_id SET NOT NULL;
CREATE INDEX gateway_request_metrics_plan_account_time_idx
    ON gateway_request_metrics(plan_id, account_id, created_at DESC);

CREATE TABLE notifications (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 120),
    body TEXT NOT NULL DEFAULT '' CHECK (char_length(body) <= 500),
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id TEXT NOT NULL DEFAULT '',
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX notifications_user_time_idx ON notifications(user_id, created_at DESC);
CREATE INDEX notifications_user_unread_idx ON notifications(user_id, created_at DESC) WHERE read_at IS NULL;
