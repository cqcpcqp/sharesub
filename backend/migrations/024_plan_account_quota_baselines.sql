ALTER TABLE shared_plans
    ADD COLUMN account_binding_generation BIGINT NOT NULL DEFAULT 0 CHECK (account_binding_generation >= 0),
    ADD COLUMN account_bound_at TIMESTAMPTZ;

UPDATE shared_plans
SET account_bound_at = COALESCE(updated_at, created_at)
WHERE account_id IS NOT NULL;

ALTER TABLE shared_plans
    ADD CONSTRAINT shared_plans_account_binding_check CHECK (
        (account_id IS NULL AND account_binding_generation = 0 AND account_bound_at IS NULL)
        OR
        (account_id IS NOT NULL AND account_bound_at IS NOT NULL)
    );

ALTER TABLE gateway_request_metrics
    ADD COLUMN account_binding_generation BIGINT NOT NULL DEFAULT 0 CHECK (account_binding_generation >= 0);

-- Metrics that predate binding generations belong to the legacy generation.
UPDATE gateway_request_metrics
SET account_binding_generation = 0;

DROP INDEX IF EXISTS gateway_request_metrics_request_key;
CREATE UNIQUE INDEX gateway_request_metrics_request_key
    ON gateway_request_metrics(request_id, api_key_id, plan_id, account_id, account_binding_generation);

ALTER TABLE gateway_metric_daily_rollups
    ADD COLUMN account_binding_generation BIGINT NOT NULL DEFAULT 0 CHECK (account_binding_generation >= 0),
    DROP CONSTRAINT gateway_metric_daily_rollups_pkey,
    ADD PRIMARY KEY (usage_day, user_id, plan_id, account_id, member_id, account_binding_generation);

-- Rollups that predate binding generations belong to the legacy generation.
UPDATE gateway_metric_daily_rollups
SET account_binding_generation = 0;

CREATE INDEX gateway_request_metrics_binding_cost_idx
    ON gateway_request_metrics(plan_id, account_id, account_binding_generation, created_at);

CREATE TABLE plan_account_quota_baselines (
    plan_id TEXT NOT NULL REFERENCES shared_plans(id) ON DELETE CASCADE,
    account_id TEXT NOT NULL REFERENCES openai_accounts(id) ON DELETE CASCADE,
    account_binding_generation BIGINT NOT NULL CHECK (account_binding_generation >= 0),
    window_type TEXT NOT NULL CHECK (window_type IN ('5h', '7d')),
    window_start TIMESTAMPTZ NOT NULL,
    reset_at TIMESTAMPTZ NOT NULL,
    baseline_used_micros BIGINT NOT NULL CHECK (baseline_used_micros BETWEEN 0 AND 100000000),
    accounting_started_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (plan_id, account_binding_generation, window_type),
    UNIQUE (plan_id, account_id, account_binding_generation, window_type, window_start)
);

-- A legacy Plan is already using its current account binding. Preserve its
-- existing accounting window instead of treating the upgrade as a new
-- binding: the full current quota usage remains attributable to the legacy
-- generation and all metrics from the current window continue to participate
-- in member allocation.
INSERT INTO plan_account_quota_baselines(
    plan_id, account_id, account_binding_generation, window_type, window_start,
    reset_at, baseline_used_micros, accounting_started_at, updated_at
)
SELECT p.id, p.account_id, p.account_binding_generation, q.window_type,
       q.window_start, q.reset_at, 0, q.window_start, now()
FROM shared_plans p
JOIN account_quota_snapshots q ON q.account_id = p.account_id
WHERE p.account_id IS NOT NULL;

-- A removed experimental migration may already be registered in
-- schema_migrations. Drop its index together with the obsolete attribution
-- tables; IF EXISTS keeps upgrades valid whether 023 ever ran or not.
DROP INDEX IF EXISTS quota_usage_events_account_window_created_idx;
DROP INDEX IF EXISTS quota_usage_events_created_at_idx;
DROP TABLE IF EXISTS quota_usage_events;
DROP TABLE IF EXISTS member_quota_windows;
