ALTER TABLE gateway_request_metrics
    ADD COLUMN model TEXT NOT NULL DEFAULT '',
    ADD COLUMN service_tier TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN gateway_request_metrics.estimated_cost_micros IS
    'Account-billed cost in micro-USD, calculated from the request model pricing; legacy column name retained for API compatibility.';
