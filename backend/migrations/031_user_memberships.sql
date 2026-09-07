DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM plan_payment_orders) THEN
        RAISE EXCEPTION 'Legacy Plan payment orders exist. Reconcile and archive them before migrating to user memberships.';
    END IF;
END $$;

CREATE TABLE membership_rollout (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    started_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE user_memberships (
    user_id TEXT PRIMARY KEY REFERENCES users(id),
    tier TEXT NOT NULL DEFAULT 'none' CHECK (tier IN ('none','vip','svip')),
    expires_at TIMESTAMPTZ,
    owner_limit_override INTEGER CHECK (owner_limit_override BETWEEN 0 AND 10000),
    revision BIGINT NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'none',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE membership_payment_orders (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    product TEXT NOT NULL CHECK (product IN ('vip','svip','upgrade')),
    amount_cents INTEGER NOT NULL,
    CHECK ((product='vip' AND amount_cents=990) OR (product='svip' AND amount_cents=1990) OR (product='upgrade' AND amount_cents=1000)),
    membership_revision BIGINT NOT NULL,
    upgrade_expires_at TIMESTAMPTZ,
    provider_pid TEXT NOT NULL,
    provider_base_url TEXT NOT NULL,
    provider_key_ciphertext BYTEA NOT NULL,
    payment_method TEXT NOT NULL CHECK (payment_method IN ('alipay','wxpay')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','expired','paid','review_required')),
    trade_no TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    paid_at TIMESTAMPTZ,
    service_expires_at TIMESTAMPTZ,
    last_checked_at TIMESTAMPTZ,
    CHECK ((status IN ('paid','review_required'))=(paid_at IS NOT NULL AND trade_no IS NOT NULL)),
    UNIQUE(provider_base_url,provider_pid,trade_no)
);
CREATE UNIQUE INDEX membership_one_pending ON membership_payment_orders(user_id) WHERE status='pending';
CREATE INDEX membership_order_history ON membership_payment_orders(user_id,created_at DESC);
CREATE INDEX membership_reconcile ON membership_payment_orders(last_checked_at,created_at) WHERE status IN ('pending','expired');
CREATE TABLE membership_reminders (
    user_id TEXT NOT NULL REFERENCES users(id),
    expires_at TIMESTAMPTZ NOT NULL,
    days_before INTEGER NOT NULL,
    PRIMARY KEY(user_id,expires_at,days_before)
);
UPDATE payment_settings SET enabled=false,revision=revision+1 WHERE enabled;
