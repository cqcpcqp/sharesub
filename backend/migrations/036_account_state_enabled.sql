ALTER TABLE openai_accounts
    ADD COLUMN state_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE codex_state_jobs (
    account_id TEXT NOT NULL REFERENCES openai_accounts(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    lease TEXT NOT NULL DEFAULT '',
    lease_until TIMESTAMPTZ NOT NULL DEFAULT '-infinity',
    version TEXT NOT NULL DEFAULT '',
    ciphertext BYTEA NOT NULL DEFAULT ''::bytea,
    expires_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    result TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(account_id,model)
);
CREATE INDEX codex_state_jobs_due ON codex_state_jobs(next_attempt_at);

-- Configuration and credential changes revoke both tickets and in-flight jobs.
CREATE FUNCTION revoke_account_codex_state() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.state_enabled IS DISTINCT FROM NEW.state_enabled
       OR OLD.status IS DISTINCT FROM NEW.status
       OR OLD.proxy_url_ciphertext IS DISTINCT FROM NEW.proxy_url_ciphertext
       OR OLD.refresh_token_ciphertext IS DISTINCT FROM NEW.refresh_token_ciphertext
       OR OLD.chatgpt_account_id IS DISTINCT FROM NEW.chatgpt_account_id
       OR OLD.plan_type IS DISTINCT FROM NEW.plan_type
       OR OLD.codex_fingerprint_mode IS DISTINCT FROM NEW.codex_fingerprint_mode THEN
        DELETE FROM codex_state_jobs WHERE account_id=NEW.id;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER revoke_account_codex_state AFTER UPDATE ON openai_accounts
FOR EACH ROW EXECUTE FUNCTION revoke_account_codex_state();
