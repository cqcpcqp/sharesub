-- Repair databases that applied the original 024 migration, which treated an
-- already-active legacy binding as a new binding and subtracted its current
-- quota usage. Generation 0 is reserved for bindings that predate 024; real
-- bindings created by the new code start at generation 1 and must retain their
-- observed-at baseline.
UPDATE plan_account_quota_baselines b
SET baseline_used_micros = 0,
    accounting_started_at = b.window_start,
    updated_at = now()
FROM shared_plans p
WHERE p.id = b.plan_id
  AND p.account_id = b.account_id
  AND p.account_binding_generation = 0
  AND b.account_binding_generation = 0
  AND (
      b.baseline_used_micros <> 0
      OR b.accounting_started_at <> b.window_start
  );
