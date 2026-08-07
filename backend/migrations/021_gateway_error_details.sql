ALTER TABLE gateway_request_metrics
    ADD COLUMN endpoint TEXT NOT NULL DEFAULT '',
    ADD COLUMN is_stream BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN error_source TEXT NOT NULL DEFAULT '' CHECK (error_source IN ('', 'request', 'upstream', 'gateway')),
    ADD COLUMN error_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN error_message TEXT NOT NULL DEFAULT '';

CREATE INDEX gateway_request_metrics_plan_errors_time_idx
    ON gateway_request_metrics(plan_id, created_at DESC, id DESC)
    WHERE status_code < 200 OR status_code >= 300;
