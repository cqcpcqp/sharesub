CREATE TABLE payment_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    base_url TEXT NOT NULL,
    pid TEXT NOT NULL,
    key_ciphertext BYTEA NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT false,
    revision BIGINT NOT NULL CHECK (revision > 0)
);
