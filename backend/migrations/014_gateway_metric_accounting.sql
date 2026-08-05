ALTER TABLE gateway_request_metrics
    ADD COLUMN requested_model TEXT NOT NULL DEFAULT '',
    ADD COLUMN upstream_model TEXT NOT NULL DEFAULT '',
    ADD COLUMN billing_model TEXT NOT NULL DEFAULT '',
    ADD COLUMN cache_creation_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_creation_tokens >= 0),
    ADD COLUMN image_input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (image_input_tokens >= 0),
    ADD COLUMN image_output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (image_output_tokens >= 0),
    ADD COLUMN image_count BIGINT NOT NULL DEFAULT 0 CHECK (image_count >= 0),
    ADD COLUMN web_search_calls BIGINT NOT NULL DEFAULT 0 CHECK (web_search_calls >= 0),
    ADD COLUMN input_cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (input_cost_micros >= 0),
    ADD COLUMN output_cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (output_cost_micros >= 0),
    ADD COLUMN cache_creation_cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (cache_creation_cost_micros >= 0),
    ADD COLUMN cache_read_cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (cache_read_cost_micros >= 0),
    ADD COLUMN image_input_cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (image_input_cost_micros >= 0),
    ADD COLUMN image_output_cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (image_output_cost_micros >= 0),
    ADD COLUMN web_search_cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (web_search_cost_micros >= 0);

UPDATE gateway_request_metrics
SET requested_model=model,upstream_model=model,billing_model=model
WHERE requested_model='';

DELETE FROM gateway_request_metrics doomed
USING gateway_request_metrics winner
WHERE doomed.request_id=winner.request_id
  AND doomed.api_key_id=winner.api_key_id
  AND doomed.account_id=winner.account_id
  AND doomed.id<winner.id;

CREATE UNIQUE INDEX gateway_request_metrics_request_key
    ON gateway_request_metrics(request_id,api_key_id,account_id);

ALTER TABLE gateway_metric_daily_rollups
    ADD COLUMN cache_creation_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_creation_tokens >= 0),
    ADD COLUMN image_input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (image_input_tokens >= 0),
    ADD COLUMN image_output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (image_output_tokens >= 0),
    ADD COLUMN image_count BIGINT NOT NULL DEFAULT 0 CHECK (image_count >= 0),
    ADD COLUMN web_search_calls BIGINT NOT NULL DEFAULT 0 CHECK (web_search_calls >= 0);
