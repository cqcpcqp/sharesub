CREATE TABLE plan_pro_subscriptions (
    plan_id TEXT PRIMARY KEY REFERENCES shared_plans(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    trial_granted_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plan_billing_rollout (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    started_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE plan_payment_orders (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES shared_plans(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    amount_cents INTEGER NOT NULL CHECK (amount_cents=4990),
    period_days INTEGER NOT NULL CHECK (period_days=30),
    provider_pid TEXT NOT NULL,
    provider_base_url TEXT NOT NULL,
    provider_key_ciphertext BYTEA NOT NULL,
    payment_method TEXT NOT NULL CHECK (payment_method IN ('alipay','wxpay')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','expired','paid')),
    trade_no TEXT UNIQUE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    paid_at TIMESTAMPTZ,
    service_expires_at TIMESTAMPTZ,
    last_checked_at TIMESTAMPTZ,
    CHECK ((status='paid')=(paid_at IS NOT NULL AND service_expires_at IS NOT NULL AND trade_no IS NOT NULL))
);
CREATE INDEX plan_payment_orders_plan_created ON plan_payment_orders(plan_id,created_at DESC);
CREATE INDEX plan_payment_orders_reconcile ON plan_payment_orders(last_checked_at,created_at) WHERE status<>'paid';
CREATE UNIQUE INDEX plan_payment_orders_one_pending ON plan_payment_orders(plan_id) WHERE status='pending';
CREATE INDEX plan_payment_orders_pending_expiry ON plan_payment_orders(expires_at) WHERE status='pending';

CREATE TABLE plan_billing_reminders (
    plan_id TEXT NOT NULL REFERENCES shared_plans(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    days_before INTEGER NOT NULL,
    PRIMARY KEY(plan_id,expires_at,days_before)
);
