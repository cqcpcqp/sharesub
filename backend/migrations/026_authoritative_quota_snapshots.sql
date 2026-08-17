ALTER TABLE account_quota_snapshots
    ADD COLUMN authoritative BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN authoritative_at TIMESTAMPTZ,
    ADD CONSTRAINT account_quota_snapshots_authoritative_at_check
        CHECK (NOT authoritative OR authoritative_at IS NOT NULL);

-- Existing rows may have come from either a routed gateway response or an
-- older active probe, so leave them non-authoritative until the next probe.
-- New authoritative writes record when their upstream request started; that
-- timestamp prevents a slower, older probe from replacing a newer result.
