ALTER TABLE openai_accounts
    ADD COLUMN name TEXT NOT NULL DEFAULT '',
    ADD COLUMN notes TEXT NOT NULL DEFAULT '',
    ADD COLUMN proxy_url_ciphertext BYTEA,
    ADD COLUMN max_concurrency INTEGER NOT NULL DEFAULT 0 CHECK (max_concurrency BETWEEN 0 AND 100),
    ADD COLUMN rpm_limit INTEGER NOT NULL DEFAULT 0 CHECK (rpm_limit BETWEEN 0 AND 10000);

UPDATE openai_accounts SET name = email WHERE name = '';

ALTER TABLE openai_accounts
    ADD CONSTRAINT openai_accounts_name_check CHECK (length(name) BETWEEN 1 AND 100),
    ADD CONSTRAINT openai_accounts_notes_check CHECK (length(notes) <= 2000);
