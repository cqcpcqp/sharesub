ALTER TABLE openai_accounts
    ADD COLUMN codex_fingerprint_mode TEXT NOT NULL DEFAULT 'session'
    CHECK (codex_fingerprint_mode IN ('off', 'device', 'session', 'full'));
