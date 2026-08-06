CREATE TABLE account_token_refresh_leases (
    account_id TEXT PRIMARY KEY REFERENCES openai_accounts(id) ON DELETE CASCADE,
    holder_id TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX account_token_refresh_leases_expires_at_idx
    ON account_token_refresh_leases(expires_at);
