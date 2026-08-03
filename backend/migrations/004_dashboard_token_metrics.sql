ALTER TABLE gateway_request_metrics
    ADD COLUMN input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    ADD COLUMN output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    ADD COLUMN cached_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cached_tokens >= 0);
