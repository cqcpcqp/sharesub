CREATE TABLE user_agreement_acceptances (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    terms_version TEXT NOT NULL,
    privacy_policy_version TEXT NOT NULL,
    acceptable_use_version TEXT NOT NULL,
    accepted_at TIMESTAMPTZ NOT NULL,
    UNIQUE (user_id, terms_version, privacy_policy_version, acceptable_use_version)
);

CREATE INDEX user_agreement_acceptances_user_time_idx
    ON user_agreement_acceptances(user_id, accepted_at DESC);
