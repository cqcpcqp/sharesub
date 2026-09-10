UPDATE pricing_versions
SET config = config || jsonb_build_object('fast_multiplier_bps', 20000, 'flex_multiplier_bps', 5000)
WHERE NOT (config ? 'fast_multiplier_bps');
