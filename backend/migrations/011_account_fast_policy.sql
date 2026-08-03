ALTER TABLE openai_accounts
    ADD COLUMN fast_policy JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE openai_accounts
    ADD CONSTRAINT openai_accounts_fast_policy_array_check
    CHECK (fast_policy <> 'null'::jsonb AND jsonb_typeof(fast_policy) = 'array');
