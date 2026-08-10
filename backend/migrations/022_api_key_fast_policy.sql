ALTER TABLE api_keys
    ADD COLUMN fast_policy JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_fast_policy_array_check
    CHECK (fast_policy <> 'null'::jsonb AND jsonb_typeof(fast_policy) = 'array');
