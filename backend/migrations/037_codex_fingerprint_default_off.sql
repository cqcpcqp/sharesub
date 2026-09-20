-- Preserve every existing account's explicitly stored mode.
ALTER TABLE openai_accounts
    ALTER COLUMN codex_fingerprint_mode SET DEFAULT 'off';
