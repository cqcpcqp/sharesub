CREATE TABLE gateway_metric_daily_rollups (
    usage_day DATE NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id TEXT NOT NULL REFERENCES shared_plans(id) ON DELETE CASCADE,
    account_id TEXT NOT NULL REFERENCES openai_accounts(id) ON DELETE CASCADE,
    member_id TEXT NOT NULL REFERENCES plan_members(id) ON DELETE CASCADE,
    request_count BIGINT NOT NULL CHECK (request_count >= 0),
    success_count BIGINT NOT NULL CHECK (success_count >= 0),
    input_tokens BIGINT NOT NULL CHECK (input_tokens >= 0),
    output_tokens BIGINT NOT NULL CHECK (output_tokens >= 0),
    cached_tokens BIGINT NOT NULL CHECK (cached_tokens >= 0),
    estimated_cost_micros BIGINT NOT NULL CHECK (estimated_cost_micros >= 0),
    PRIMARY KEY (usage_day, user_id, plan_id, account_id, member_id)
);

CREATE INDEX gateway_metric_daily_rollups_user_idx
    ON gateway_metric_daily_rollups(user_id, usage_day);
CREATE INDEX gateway_metric_daily_rollups_plan_idx
    ON gateway_metric_daily_rollups(plan_id, usage_day);

CREATE INDEX user_sessions_expires_at_idx ON user_sessions(expires_at);
CREATE INDEX oauth_flows_expires_at_idx ON oauth_flows(expires_at);
CREATE INDEX quota_usage_events_created_at_idx ON quota_usage_events(created_at);
CREATE INDEX audit_events_created_at_idx ON audit_events(created_at);
CREATE INDEX notifications_read_created_at_idx
    ON notifications(created_at) WHERE read_at IS NOT NULL;
CREATE INDEX plan_invites_terminal_time_idx
    ON plan_invites(COALESCE(accepted_at, revoked_at, expires_at));
CREATE INDEX plan_join_applications_terminal_time_idx
    ON plan_join_applications(COALESCE(reviewed_at, created_at)) WHERE status <> 'pending';
CREATE INDEX api_keys_revoked_updated_at_idx
    ON api_keys(updated_at) WHERE status = 'revoked';
