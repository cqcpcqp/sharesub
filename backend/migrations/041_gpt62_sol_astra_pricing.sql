-- Official Standard rates supplied on 2026-11-18, in the existing pricing units.
-- Keep historical versions and all existing administrator prices/multipliers intact.
WITH additions AS (
    SELECT value AS model FROM jsonb_array_elements($models$
[
  {
    "model": "gpt-6.2-sol",
    "standard": {
      "input": 2000000,
      "output": 10000000,
      "cache_read": 100000,
      "cache_write": 2500000,
      "image_input": 2000000,
      "image_output": 10000000
    },
    "long_context_tokens": 272000,
    "long_input_bps": 20000,
    "long_output_bps": 15000
  },
  {
    "model": "gpt-6.2-astra",
    "standard": {
      "input": 8000000,
      "output": 40000000,
      "cache_read": 400000,
      "cache_write": 10000000,
      "image_input": 8000000,
      "image_output": 40000000
    },
    "long_context_tokens": 272000,
    "long_input_bps": 20000,
    "long_output_bps": 15000
  }
]
$models$::jsonb)
), current_version AS (
    SELECT p.id, p.config
    FROM current_pricing c JOIN pricing_versions p ON p.id = c.version_id
    WHERE c.singleton
    FOR UPDATE OF c
), missing AS (
    SELECT jsonb_agg(a.model ORDER BY a.model->>'model') AS models
    FROM additions a, current_version v
    WHERE NOT EXISTS (
        SELECT 1 FROM jsonb_array_elements(v.config->'models') existing
        WHERE existing->>'model' = a.model->>'model'
    )
), published AS (
    INSERT INTO pricing_versions(published_by, reason, config)
    SELECT 'system', '新增 GPT-6.2 Sol / Astra 标准计价；保留现有价格和倍率',
           jsonb_set(v.config, '{models}', v.config->'models' || m.models)
    FROM current_version v, missing m
    WHERE m.models IS NOT NULL
    RETURNING id
)
UPDATE current_pricing SET version_id = published.id FROM published WHERE singleton;
