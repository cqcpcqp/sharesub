CREATE TABLE pricing_versions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    published_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    published_by TEXT NOT NULL,
    reason TEXT NOT NULL,
    config JSONB NOT NULL CHECK (jsonb_typeof(config) = 'object')
);

CREATE TABLE current_pricing (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    version_id BIGINT NOT NULL REFERENCES pricing_versions(id)
);

ALTER TABLE gateway_request_metrics ADD COLUMN pricing_version_id BIGINT REFERENCES pricing_versions(id);

INSERT INTO pricing_versions(published_by,reason,config) VALUES ('system','初始化现有计价；历史请求未记录价格版本',$pricing$
{
  "models": [
    {
      "model": "codex-auto-review",
      "standard": {
        "input": 200000,
        "output": 1200000,
        "cache_read": 20000,
        "cache_write": 0,
        "image_input": 200000,
        "image_output": 1200000
      },
      "priority": {
        "input": 400000,
        "output": 2400000,
        "cache_read": 40000,
        "cache_write": 0,
        "image_input": 400000,
        "image_output": 2400000
      },
      "flex": {
        "input": 100000,
        "output": 600000,
        "cache_read": 10000,
        "cache_write": 0,
        "image_input": 100000,
        "image_output": 600000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-3.5-turbo",
      "standard": {
        "input": 500000,
        "output": 1500000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 500000,
        "image_output": 1500000
      },
      "priority": {
        "input": 1000000,
        "output": 3000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1000000,
        "image_output": 3000000
      },
      "flex": {
        "input": 250000,
        "output": 750000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 250000,
        "image_output": 750000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-3.5-turbo-0125",
      "standard": {
        "input": 500000,
        "output": 1500000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 500000,
        "image_output": 1500000
      },
      "priority": {
        "input": 1000000,
        "output": 3000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1000000,
        "image_output": 3000000
      },
      "flex": {
        "input": 250000,
        "output": 750000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 250000,
        "image_output": 750000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-3.5-turbo-1106",
      "standard": {
        "input": 1000000,
        "output": 2000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1000000,
        "image_output": 2000000
      },
      "priority": {
        "input": 2000000,
        "output": 4000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2000000,
        "image_output": 4000000
      },
      "flex": {
        "input": 500000,
        "output": 1000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 500000,
        "image_output": 1000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-3.5-turbo-16k",
      "standard": {
        "input": 3000000,
        "output": 4000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 3000000,
        "image_output": 4000000
      },
      "priority": {
        "input": 6000000,
        "output": 8000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 6000000,
        "image_output": 8000000
      },
      "flex": {
        "input": 1500000,
        "output": 2000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1500000,
        "image_output": 2000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-3.5-turbo-instruct",
      "standard": {
        "input": 1500000,
        "output": 2000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1500000,
        "image_output": 2000000
      },
      "priority": {
        "input": 3000000,
        "output": 4000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 3000000,
        "image_output": 4000000
      },
      "flex": {
        "input": 750000,
        "output": 1000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 750000,
        "image_output": 1000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-3.5-turbo-instruct-0914",
      "standard": {
        "input": 1500000,
        "output": 2000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1500000,
        "image_output": 2000000
      },
      "priority": {
        "input": 3000000,
        "output": 4000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 3000000,
        "image_output": 4000000
      },
      "flex": {
        "input": 750000,
        "output": 1000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 750000,
        "image_output": 1000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4",
      "standard": {
        "input": 30000000,
        "output": 60000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 30000000,
        "image_output": 60000000
      },
      "priority": {
        "input": 60000000,
        "output": 120000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 60000000,
        "image_output": 120000000
      },
      "flex": {
        "input": 15000000,
        "output": 30000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 15000000,
        "image_output": 30000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4-0125-preview",
      "standard": {
        "input": 10000000,
        "output": 30000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 10000000,
        "image_output": 30000000
      },
      "priority": {
        "input": 20000000,
        "output": 60000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 20000000,
        "image_output": 60000000
      },
      "flex": {
        "input": 5000000,
        "output": 15000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 15000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4-0314",
      "standard": {
        "input": 30000000,
        "output": 60000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 30000000,
        "image_output": 60000000
      },
      "priority": {
        "input": 60000000,
        "output": 120000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 60000000,
        "image_output": 120000000
      },
      "flex": {
        "input": 15000000,
        "output": 30000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 15000000,
        "image_output": 30000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4-0613",
      "standard": {
        "input": 30000000,
        "output": 60000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 30000000,
        "image_output": 60000000
      },
      "priority": {
        "input": 60000000,
        "output": 120000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 60000000,
        "image_output": 120000000
      },
      "flex": {
        "input": 15000000,
        "output": 30000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 15000000,
        "image_output": 30000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4-1106-preview",
      "standard": {
        "input": 10000000,
        "output": 30000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 10000000,
        "image_output": 30000000
      },
      "priority": {
        "input": 20000000,
        "output": 60000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 20000000,
        "image_output": 60000000
      },
      "flex": {
        "input": 5000000,
        "output": 15000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 15000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4-turbo",
      "standard": {
        "input": 10000000,
        "output": 30000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 10000000,
        "image_output": 30000000
      },
      "priority": {
        "input": 20000000,
        "output": 60000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 20000000,
        "image_output": 60000000
      },
      "flex": {
        "input": 5000000,
        "output": 15000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 15000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4-turbo-2024-04-09",
      "standard": {
        "input": 10000000,
        "output": 30000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 10000000,
        "image_output": 30000000
      },
      "priority": {
        "input": 20000000,
        "output": 60000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 20000000,
        "image_output": 60000000
      },
      "flex": {
        "input": 5000000,
        "output": 15000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 15000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4-turbo-preview",
      "standard": {
        "input": 10000000,
        "output": 30000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 10000000,
        "image_output": 30000000
      },
      "priority": {
        "input": 20000000,
        "output": 60000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 20000000,
        "image_output": 60000000
      },
      "flex": {
        "input": 5000000,
        "output": 15000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 15000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4.1",
      "standard": {
        "input": 2000000,
        "output": 8000000,
        "cache_read": 500000,
        "cache_write": 0,
        "image_input": 2000000,
        "image_output": 8000000
      },
      "priority": {
        "input": 3500000,
        "output": 14000000,
        "cache_read": 875000,
        "cache_write": 0,
        "image_input": 3500000,
        "image_output": 14000000
      },
      "flex": {
        "input": 1000000,
        "output": 4000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 1000000,
        "image_output": 4000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4.1-2025-04-14",
      "standard": {
        "input": 2000000,
        "output": 8000000,
        "cache_read": 500000,
        "cache_write": 0,
        "image_input": 2000000,
        "image_output": 8000000
      },
      "priority": {
        "input": 4000000,
        "output": 16000000,
        "cache_read": 1000000,
        "cache_write": 0,
        "image_input": 4000000,
        "image_output": 16000000
      },
      "flex": {
        "input": 1000000,
        "output": 4000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 1000000,
        "image_output": 4000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4.1-mini",
      "standard": {
        "input": 400000,
        "output": 1600000,
        "cache_read": 100000,
        "cache_write": 0,
        "image_input": 400000,
        "image_output": 1600000
      },
      "priority": {
        "input": 700000,
        "output": 2800000,
        "cache_read": 175000,
        "cache_write": 0,
        "image_input": 700000,
        "image_output": 2800000
      },
      "flex": {
        "input": 200000,
        "output": 800000,
        "cache_read": 50000,
        "cache_write": 0,
        "image_input": 200000,
        "image_output": 800000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4.1-mini-2025-04-14",
      "standard": {
        "input": 400000,
        "output": 1600000,
        "cache_read": 100000,
        "cache_write": 0,
        "image_input": 400000,
        "image_output": 1600000
      },
      "priority": {
        "input": 800000,
        "output": 3200000,
        "cache_read": 200000,
        "cache_write": 0,
        "image_input": 800000,
        "image_output": 3200000
      },
      "flex": {
        "input": 200000,
        "output": 800000,
        "cache_read": 50000,
        "cache_write": 0,
        "image_input": 200000,
        "image_output": 800000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4.1-nano",
      "standard": {
        "input": 100000,
        "output": 400000,
        "cache_read": 25000,
        "cache_write": 0,
        "image_input": 100000,
        "image_output": 400000
      },
      "priority": {
        "input": 200000,
        "output": 800000,
        "cache_read": 50000,
        "cache_write": 0,
        "image_input": 200000,
        "image_output": 800000
      },
      "flex": {
        "input": 50000,
        "output": 200000,
        "cache_read": 12500,
        "cache_write": 0,
        "image_input": 50000,
        "image_output": 200000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4.1-nano-2025-04-14",
      "standard": {
        "input": 100000,
        "output": 400000,
        "cache_read": 25000,
        "cache_write": 0,
        "image_input": 100000,
        "image_output": 400000
      },
      "priority": {
        "input": 200000,
        "output": 800000,
        "cache_read": 50000,
        "cache_write": 0,
        "image_input": 200000,
        "image_output": 800000
      },
      "flex": {
        "input": 50000,
        "output": 200000,
        "cache_read": 12500,
        "cache_write": 0,
        "image_input": 50000,
        "image_output": 200000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 4250000,
        "output": 17000000,
        "cache_read": 2125000,
        "cache_write": 0,
        "image_input": 4250000,
        "image_output": 17000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 625000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-2024-05-13",
      "standard": {
        "input": 5000000,
        "output": 15000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 15000000
      },
      "priority": {
        "input": 8750000,
        "output": 26250000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 8750000,
        "image_output": 26250000
      },
      "flex": {
        "input": 2500000,
        "output": 7500000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 7500000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-2024-08-06",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 2500000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 625000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-2024-11-20",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 2500000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 625000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-audio-preview",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-audio-preview-2024-12-17",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-audio-preview-2025-06-03",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini",
      "standard": {
        "input": 150000,
        "output": 600000,
        "cache_read": 75000,
        "cache_write": 0,
        "image_input": 150000,
        "image_output": 600000
      },
      "priority": {
        "input": 250000,
        "output": 1000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 250000,
        "image_output": 1000000
      },
      "flex": {
        "input": 75000,
        "output": 300000,
        "cache_read": 37500,
        "cache_write": 0,
        "image_input": 75000,
        "image_output": 300000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-2024-07-18",
      "standard": {
        "input": 150000,
        "output": 600000,
        "cache_read": 75000,
        "cache_write": 0,
        "image_input": 150000,
        "image_output": 600000
      },
      "priority": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 150000,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "flex": {
        "input": 75000,
        "output": 300000,
        "cache_read": 37500,
        "cache_write": 0,
        "image_input": 75000,
        "image_output": 300000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-audio-preview",
      "standard": {
        "input": 150000,
        "output": 600000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 150000,
        "image_output": 600000
      },
      "priority": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "flex": {
        "input": 75000,
        "output": 300000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 75000,
        "image_output": 300000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-audio-preview-2024-12-17",
      "standard": {
        "input": 150000,
        "output": 600000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 150000,
        "image_output": 600000
      },
      "priority": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "flex": {
        "input": 75000,
        "output": 300000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 75000,
        "image_output": 300000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-realtime-preview",
      "standard": {
        "input": 600000,
        "output": 2400000,
        "cache_read": 300000,
        "cache_write": 0,
        "image_input": 600000,
        "image_output": 2400000
      },
      "priority": {
        "input": 1200000,
        "output": 4800000,
        "cache_read": 600000,
        "cache_write": 0,
        "image_input": 1200000,
        "image_output": 4800000
      },
      "flex": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 150000,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-realtime-preview-2024-12-17",
      "standard": {
        "input": 600000,
        "output": 2400000,
        "cache_read": 300000,
        "cache_write": 0,
        "image_input": 600000,
        "image_output": 2400000
      },
      "priority": {
        "input": 1200000,
        "output": 4800000,
        "cache_read": 600000,
        "cache_write": 0,
        "image_input": 1200000,
        "image_output": 4800000
      },
      "flex": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 150000,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-search-preview",
      "standard": {
        "input": 150000,
        "output": 600000,
        "cache_read": 75000,
        "cache_write": 0,
        "image_input": 150000,
        "image_output": 600000
      },
      "priority": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 150000,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "flex": {
        "input": 75000,
        "output": 300000,
        "cache_read": 37500,
        "cache_write": 0,
        "image_input": 75000,
        "image_output": 300000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-search-preview-2025-03-11",
      "standard": {
        "input": 150000,
        "output": 600000,
        "cache_read": 75000,
        "cache_write": 0,
        "image_input": 150000,
        "image_output": 600000
      },
      "priority": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 150000,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "flex": {
        "input": 75000,
        "output": 300000,
        "cache_read": 37500,
        "cache_write": 0,
        "image_input": 75000,
        "image_output": 300000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-transcribe",
      "standard": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "priority": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "flex": {
        "input": 625000,
        "output": 2500000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 2500000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-transcribe-2025-03-20",
      "standard": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "priority": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "flex": {
        "input": 625000,
        "output": 2500000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 2500000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-transcribe-2025-12-15",
      "standard": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "priority": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "flex": {
        "input": 625000,
        "output": 2500000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 2500000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-tts",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-tts-2025-03-20",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-mini-tts-2025-12-15",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-realtime-preview",
      "standard": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 2500000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "priority": {
        "input": 10000000,
        "output": 40000000,
        "cache_read": 5000000,
        "cache_write": 0,
        "image_input": 10000000,
        "image_output": 40000000
      },
      "flex": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-realtime-preview-2024-12-17",
      "standard": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 2500000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "priority": {
        "input": 10000000,
        "output": 40000000,
        "cache_read": 5000000,
        "cache_write": 0,
        "image_input": 10000000,
        "image_output": 40000000
      },
      "flex": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-realtime-preview-2025-06-03",
      "standard": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 2500000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "priority": {
        "input": 10000000,
        "output": 40000000,
        "cache_read": 5000000,
        "cache_write": 0,
        "image_input": 10000000,
        "image_output": 40000000
      },
      "flex": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-search-preview",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 2500000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 625000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-search-preview-2025-03-11",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 2500000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 625000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-transcribe",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-4o-transcribe-diarize",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5",
      "standard": {
        "input": 1250000,
        "output": 10000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 10000000
      },
      "priority": {
        "input": 2500000,
        "output": 20000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 20000000
      },
      "flex": {
        "input": 625000,
        "output": 5000000,
        "cache_read": 62500,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5-2025-08-07",
      "standard": {
        "input": 1250000,
        "output": 10000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 10000000
      },
      "priority": {
        "input": 2500000,
        "output": 20000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 20000000
      },
      "flex": {
        "input": 625000,
        "output": 5000000,
        "cache_read": 62500,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5-chat",
      "standard": {
        "input": 1250000,
        "output": 10000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 10000000
      },
      "priority": {
        "input": 2500000,
        "output": 20000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 20000000
      },
      "flex": {
        "input": 625000,
        "output": 5000000,
        "cache_read": 62500,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5-chat-latest",
      "standard": {
        "input": 1250000,
        "output": 10000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 10000000
      },
      "priority": {
        "input": 2500000,
        "output": 20000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 20000000
      },
      "flex": {
        "input": 625000,
        "output": 5000000,
        "cache_read": 62500,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5-codex",
      "standard": {
        "input": 1250000,
        "output": 10000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 10000000
      },
      "priority": {
        "input": 2500000,
        "output": 20000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 20000000
      },
      "flex": {
        "input": 625000,
        "output": 5000000,
        "cache_read": 62500,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5-mini",
      "standard": {
        "input": 250000,
        "output": 2000000,
        "cache_read": 25000,
        "cache_write": 0,
        "image_input": 250000,
        "image_output": 2000000
      },
      "priority": {
        "input": 450000,
        "output": 3600000,
        "cache_read": 45000,
        "cache_write": 0,
        "image_input": 450000,
        "image_output": 3600000
      },
      "flex": {
        "input": 125000,
        "output": 1000000,
        "cache_read": 12500,
        "cache_write": 0,
        "image_input": 125000,
        "image_output": 1000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5-mini-2025-08-07",
      "standard": {
        "input": 250000,
        "output": 2000000,
        "cache_read": 25000,
        "cache_write": 0,
        "image_input": 250000,
        "image_output": 2000000
      },
      "priority": {
        "input": 450000,
        "output": 3600000,
        "cache_read": 45000,
        "cache_write": 0,
        "image_input": 450000,
        "image_output": 3600000
      },
      "flex": {
        "input": 125000,
        "output": 1000000,
        "cache_read": 12500,
        "cache_write": 0,
        "image_input": 125000,
        "image_output": 1000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5-nano",
      "standard": {
        "input": 50000,
        "output": 400000,
        "cache_read": 5000,
        "cache_write": 0,
        "image_input": 50000,
        "image_output": 400000
      },
      "priority": {
        "input": 2500000,
        "output": 400000,
        "cache_read": 5000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 400000
      },
      "flex": {
        "input": 25000,
        "output": 200000,
        "cache_read": 2500,
        "cache_write": 0,
        "image_input": 25000,
        "image_output": 200000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5-nano-2025-08-07",
      "standard": {
        "input": 50000,
        "output": 400000,
        "cache_read": 5000,
        "cache_write": 0,
        "image_input": 50000,
        "image_output": 400000
      },
      "priority": {
        "input": 100000,
        "output": 800000,
        "cache_read": 10000,
        "cache_write": 0,
        "image_input": 100000,
        "image_output": 800000
      },
      "flex": {
        "input": 25000,
        "output": 200000,
        "cache_read": 2500,
        "cache_write": 0,
        "image_input": 25000,
        "image_output": 200000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5-pro",
      "standard": {
        "input": 15000000,
        "output": 120000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 15000000,
        "image_output": 120000000
      },
      "priority": {
        "input": 30000000,
        "output": 240000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 30000000,
        "image_output": 240000000
      },
      "flex": {
        "input": 7500000,
        "output": 60000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 7500000,
        "image_output": 60000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5-pro-2025-10-06",
      "standard": {
        "input": 15000000,
        "output": 120000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 15000000,
        "image_output": 120000000
      },
      "priority": {
        "input": 30000000,
        "output": 240000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 30000000,
        "image_output": 240000000
      },
      "flex": {
        "input": 7500000,
        "output": 60000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 7500000,
        "image_output": 60000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5-search-api",
      "standard": {
        "input": 1250000,
        "output": 10000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 10000000
      },
      "priority": {
        "input": 2500000,
        "output": 20000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 20000000
      },
      "flex": {
        "input": 625000,
        "output": 5000000,
        "cache_read": 62500,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5-search-api-2025-10-14",
      "standard": {
        "input": 1250000,
        "output": 10000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 10000000
      },
      "priority": {
        "input": 2500000,
        "output": 20000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 20000000
      },
      "flex": {
        "input": 625000,
        "output": 5000000,
        "cache_read": 62500,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.1",
      "standard": {
        "input": 1250000,
        "output": 10000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 10000000
      },
      "priority": {
        "input": 2500000,
        "output": 20000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 20000000
      },
      "flex": {
        "input": 625000,
        "output": 5000000,
        "cache_read": 62500,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.1-2025-11-13",
      "standard": {
        "input": 1250000,
        "output": 10000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 10000000
      },
      "priority": {
        "input": 2500000,
        "output": 20000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 20000000
      },
      "flex": {
        "input": 625000,
        "output": 5000000,
        "cache_read": 62500,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.1-chat-latest",
      "standard": {
        "input": 1250000,
        "output": 10000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 10000000
      },
      "priority": {
        "input": 2500000,
        "output": 20000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 20000000
      },
      "flex": {
        "input": 625000,
        "output": 5000000,
        "cache_read": 62500,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.1-codex",
      "standard": {
        "input": 1250000,
        "output": 10000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 10000000
      },
      "priority": {
        "input": 2500000,
        "output": 20000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 20000000
      },
      "flex": {
        "input": 625000,
        "output": 5000000,
        "cache_read": 62500,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.1-codex-max",
      "standard": {
        "input": 1250000,
        "output": 10000000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 10000000
      },
      "priority": {
        "input": 2500000,
        "output": 20000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 20000000
      },
      "flex": {
        "input": 625000,
        "output": 5000000,
        "cache_read": 62500,
        "cache_write": 0,
        "image_input": 625000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.1-codex-mini",
      "standard": {
        "input": 250000,
        "output": 2000000,
        "cache_read": 25000,
        "cache_write": 0,
        "image_input": 250000,
        "image_output": 2000000
      },
      "priority": {
        "input": 450000,
        "output": 3600000,
        "cache_read": 45000,
        "cache_write": 0,
        "image_input": 450000,
        "image_output": 3600000
      },
      "flex": {
        "input": 125000,
        "output": 1000000,
        "cache_read": 12500,
        "cache_write": 0,
        "image_input": 125000,
        "image_output": 1000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.2",
      "standard": {
        "input": 1750000,
        "output": 14000000,
        "cache_read": 175000,
        "cache_write": 0,
        "image_input": 1750000,
        "image_output": 14000000
      },
      "priority": {
        "input": 3500000,
        "output": 28000000,
        "cache_read": 350000,
        "cache_write": 0,
        "image_input": 3500000,
        "image_output": 28000000
      },
      "flex": {
        "input": 875000,
        "output": 7000000,
        "cache_read": 87500,
        "cache_write": 0,
        "image_input": 875000,
        "image_output": 7000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.2-2025-12-11",
      "standard": {
        "input": 1750000,
        "output": 14000000,
        "cache_read": 175000,
        "cache_write": 0,
        "image_input": 1750000,
        "image_output": 14000000
      },
      "priority": {
        "input": 3500000,
        "output": 28000000,
        "cache_read": 350000,
        "cache_write": 0,
        "image_input": 3500000,
        "image_output": 28000000
      },
      "flex": {
        "input": 875000,
        "output": 7000000,
        "cache_read": 87500,
        "cache_write": 0,
        "image_input": 875000,
        "image_output": 7000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.2-chat-latest",
      "standard": {
        "input": 1750000,
        "output": 14000000,
        "cache_read": 175000,
        "cache_write": 0,
        "image_input": 1750000,
        "image_output": 14000000
      },
      "priority": {
        "input": 3500000,
        "output": 28000000,
        "cache_read": 350000,
        "cache_write": 0,
        "image_input": 3500000,
        "image_output": 28000000
      },
      "flex": {
        "input": 875000,
        "output": 7000000,
        "cache_read": 87500,
        "cache_write": 0,
        "image_input": 875000,
        "image_output": 7000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.2-codex",
      "standard": {
        "input": 1750000,
        "output": 14000000,
        "cache_read": 175000,
        "cache_write": 0,
        "image_input": 1750000,
        "image_output": 14000000
      },
      "priority": {
        "input": 3500000,
        "output": 28000000,
        "cache_read": 350000,
        "cache_write": 0,
        "image_input": 3500000,
        "image_output": 28000000
      },
      "flex": {
        "input": 875000,
        "output": 7000000,
        "cache_read": 87500,
        "cache_write": 0,
        "image_input": 875000,
        "image_output": 7000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.2-pro",
      "standard": {
        "input": 21000000,
        "output": 168000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 21000000,
        "image_output": 168000000
      },
      "priority": {
        "input": 42000000,
        "output": 336000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 42000000,
        "image_output": 336000000
      },
      "flex": {
        "input": 10500000,
        "output": 84000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 10500000,
        "image_output": 84000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.2-pro-2025-12-11",
      "standard": {
        "input": 21000000,
        "output": 168000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 21000000,
        "image_output": 168000000
      },
      "priority": {
        "input": 42000000,
        "output": 336000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 42000000,
        "image_output": 336000000
      },
      "flex": {
        "input": 10500000,
        "output": 84000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 10500000,
        "image_output": 84000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.3-chat-latest",
      "standard": {
        "input": 1750000,
        "output": 14000000,
        "cache_read": 175000,
        "cache_write": 0,
        "image_input": 1750000,
        "image_output": 14000000
      },
      "priority": {
        "input": 3500000,
        "output": 28000000,
        "cache_read": 350000,
        "cache_write": 0,
        "image_input": 3500000,
        "image_output": 28000000
      },
      "flex": {
        "input": 875000,
        "output": 7000000,
        "cache_read": 87500,
        "cache_write": 0,
        "image_input": 875000,
        "image_output": 7000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.3-codex",
      "standard": {
        "input": 1750000,
        "output": 14000000,
        "cache_read": 175000,
        "cache_write": 0,
        "image_input": 1750000,
        "image_output": 14000000
      },
      "priority": {
        "input": 3500000,
        "output": 28000000,
        "cache_read": 350000,
        "cache_write": 0,
        "image_input": 3500000,
        "image_output": 28000000
      },
      "flex": {
        "input": 875000,
        "output": 7000000,
        "cache_read": 87500,
        "cache_write": 0,
        "image_input": 875000,
        "image_output": 7000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.3-codex-spark",
      "standard": {
        "input": 1750000,
        "output": 14000000,
        "cache_read": 175000,
        "cache_write": 0,
        "image_input": 1750000,
        "image_output": 14000000
      },
      "priority": {
        "input": 3500000,
        "output": 28000000,
        "cache_read": 350000,
        "cache_write": 0,
        "image_input": 3500000,
        "image_output": 28000000
      },
      "flex": {
        "input": 875000,
        "output": 7000000,
        "cache_read": 87500,
        "cache_write": 0,
        "image_input": 875000,
        "image_output": 7000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.4",
      "standard": {
        "input": 2500000,
        "output": 15000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 15000000
      },
      "priority": {
        "input": 5000000,
        "output": 30000000,
        "cache_read": 500000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 30000000
      },
      "flex": {
        "input": 1250000,
        "output": 7500000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 7500000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.4-2026-03-05",
      "standard": {
        "input": 2500000,
        "output": 15000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 15000000
      },
      "priority": {
        "input": 5000000,
        "output": 30000000,
        "cache_read": 500000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 30000000
      },
      "flex": {
        "input": 1250000,
        "output": 7500000,
        "cache_read": 125000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 7500000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.4-mini",
      "standard": {
        "input": 750000,
        "output": 4500000,
        "cache_read": 75000,
        "cache_write": 0,
        "image_input": 750000,
        "image_output": 4500000
      },
      "priority": {
        "input": 1500000,
        "output": 9000000,
        "cache_read": 150000,
        "cache_write": 0,
        "image_input": 1500000,
        "image_output": 9000000
      },
      "flex": {
        "input": 375000,
        "output": 2250000,
        "cache_read": 37500,
        "cache_write": 0,
        "image_input": 375000,
        "image_output": 2250000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.4-mini-2026-03-17",
      "standard": {
        "input": 750000,
        "output": 4500000,
        "cache_read": 75000,
        "cache_write": 0,
        "image_input": 750000,
        "image_output": 4500000
      },
      "priority": {
        "input": 1500000,
        "output": 9000000,
        "cache_read": 150000,
        "cache_write": 0,
        "image_input": 1500000,
        "image_output": 9000000
      },
      "flex": {
        "input": 375000,
        "output": 2250000,
        "cache_read": 37500,
        "cache_write": 0,
        "image_input": 375000,
        "image_output": 2250000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.4-nano",
      "standard": {
        "input": 200000,
        "output": 1250000,
        "cache_read": 20000,
        "cache_write": 0,
        "image_input": 200000,
        "image_output": 1250000
      },
      "priority": {
        "input": 400000,
        "output": 2500000,
        "cache_read": 40000,
        "cache_write": 0,
        "image_input": 400000,
        "image_output": 2500000
      },
      "flex": {
        "input": 100000,
        "output": 625000,
        "cache_read": 10000,
        "cache_write": 0,
        "image_input": 100000,
        "image_output": 625000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.4-nano-2026-03-17",
      "standard": {
        "input": 200000,
        "output": 1250000,
        "cache_read": 20000,
        "cache_write": 0,
        "image_input": 200000,
        "image_output": 1250000
      },
      "priority": {
        "input": 400000,
        "output": 2500000,
        "cache_read": 40000,
        "cache_write": 0,
        "image_input": 400000,
        "image_output": 2500000
      },
      "flex": {
        "input": 100000,
        "output": 625000,
        "cache_read": 10000,
        "cache_write": 0,
        "image_input": 100000,
        "image_output": 625000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.4-pro",
      "standard": {
        "input": 30000000,
        "output": 180000000,
        "cache_read": 3000000,
        "cache_write": 0,
        "image_input": 30000000,
        "image_output": 180000000
      },
      "priority": {
        "input": 60000000,
        "output": 360000000,
        "cache_read": 6000000,
        "cache_write": 0,
        "image_input": 60000000,
        "image_output": 360000000
      },
      "flex": {
        "input": 15000000,
        "output": 90000000,
        "cache_read": 1500000,
        "cache_write": 0,
        "image_input": 15000000,
        "image_output": 90000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.4-pro-2026-03-05",
      "standard": {
        "input": 30000000,
        "output": 180000000,
        "cache_read": 3000000,
        "cache_write": 0,
        "image_input": 30000000,
        "image_output": 180000000
      },
      "priority": {
        "input": 60000000,
        "output": 360000000,
        "cache_read": 6000000,
        "cache_write": 0,
        "image_input": 60000000,
        "image_output": 360000000
      },
      "flex": {
        "input": 15000000,
        "output": 90000000,
        "cache_read": 1500000,
        "cache_write": 0,
        "image_input": 15000000,
        "image_output": 90000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.5",
      "standard": {
        "input": 5000000,
        "output": 30000000,
        "cache_read": 500000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 30000000
      },
      "priority": {
        "input": 12500000,
        "output": 75000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 12500000,
        "image_output": 75000000
      },
      "flex": {
        "input": 2500000,
        "output": 15000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 15000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.5-2026-04-23",
      "standard": {
        "input": 5000000,
        "output": 30000000,
        "cache_read": 500000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 30000000
      },
      "priority": {
        "input": 10000000,
        "output": 60000000,
        "cache_read": 1000000,
        "cache_write": 0,
        "image_input": 10000000,
        "image_output": 60000000
      },
      "flex": {
        "input": 2500000,
        "output": 15000000,
        "cache_read": 250000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 15000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.5-pro",
      "standard": {
        "input": 30000000,
        "output": 180000000,
        "cache_read": 3000000,
        "cache_write": 0,
        "image_input": 30000000,
        "image_output": 180000000
      },
      "priority": {
        "input": 60000000,
        "output": 360000000,
        "cache_read": 6000000,
        "cache_write": 0,
        "image_input": 60000000,
        "image_output": 360000000
      },
      "flex": {
        "input": 15000000,
        "output": 90000000,
        "cache_read": 1500000,
        "cache_write": 0,
        "image_input": 15000000,
        "image_output": 90000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.5-pro-2026-04-23",
      "standard": {
        "input": 30000000,
        "output": 180000000,
        "cache_read": 3000000,
        "cache_write": 0,
        "image_input": 30000000,
        "image_output": 180000000
      },
      "priority": {
        "input": 60000000,
        "output": 360000000,
        "cache_read": 6000000,
        "cache_write": 0,
        "image_input": 60000000,
        "image_output": 360000000
      },
      "flex": {
        "input": 15000000,
        "output": 90000000,
        "cache_read": 1500000,
        "cache_write": 0,
        "image_input": 15000000,
        "image_output": 90000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-5.6-luna",
      "standard": {
        "input": 200000,
        "output": 1200000,
        "cache_read": 20000,
        "cache_write": 250000,
        "image_input": 200000,
        "image_output": 1200000
      },
      "priority": {
        "input": 400000,
        "output": 2400000,
        "cache_read": 40000,
        "cache_write": 500000,
        "image_input": 400000,
        "image_output": 2400000
      },
      "flex": {
        "input": 100000,
        "output": 600000,
        "cache_read": 10000,
        "cache_write": 125000,
        "image_input": 100000,
        "image_output": 600000
      },
      "long_context_tokens": 272000,
      "long_input_bps": 20000,
      "long_output_bps": 15000
    },
    {
      "model": "gpt-5.6-sol",
      "standard": {
        "input": 5000000,
        "output": 30000000,
        "cache_read": 500000,
        "cache_write": 6250000,
        "image_input": 5000000,
        "image_output": 30000000
      },
      "priority": {
        "input": 10000000,
        "output": 60000000,
        "cache_read": 1000000,
        "cache_write": 12500000,
        "image_input": 10000000,
        "image_output": 60000000
      },
      "flex": {
        "input": 2500000,
        "output": 15000000,
        "cache_read": 250000,
        "cache_write": 3125000,
        "image_input": 2500000,
        "image_output": 15000000
      },
      "long_context_tokens": 272000,
      "long_input_bps": 20000,
      "long_output_bps": 15000
    },
    {
      "model": "gpt-5.6-terra",
      "standard": {
        "input": 2000000,
        "output": 12000000,
        "cache_read": 200000,
        "cache_write": 2500000,
        "image_input": 2000000,
        "image_output": 12000000
      },
      "priority": {
        "input": 4000000,
        "output": 24000000,
        "cache_read": 400000,
        "cache_write": 5000000,
        "image_input": 4000000,
        "image_output": 24000000
      },
      "flex": {
        "input": 1000000,
        "output": 6000000,
        "cache_read": 100000,
        "cache_write": 1250000,
        "image_input": 1000000,
        "image_output": 6000000
      },
      "long_context_tokens": 272000,
      "long_input_bps": 20000,
      "long_output_bps": 15000
    },
    {
      "model": "gpt-6-astra",
      "standard": {
        "input": 10000000,
        "output": 50000000,
        "cache_read": 1000000,
        "cache_write": 12500000,
        "image_input": 10000000,
        "image_output": 50000000
      },
      "priority": {
        "input": 20000000,
        "output": 100000000,
        "cache_read": 2000000,
        "cache_write": 25000000,
        "image_input": 20000000,
        "image_output": 100000000
      },
      "flex": {
        "input": 5000000,
        "output": 25000000,
        "cache_read": 500000,
        "cache_write": 6250000,
        "image_input": 5000000,
        "image_output": 25000000
      },
      "long_context_tokens": 272000,
      "long_input_bps": 20000,
      "long_output_bps": 15000
    },
    {
      "model": "gpt-audio",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-audio-1.5",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-audio-2025-08-28",
      "standard": {
        "input": 2500000,
        "output": 10000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 10000000
      },
      "priority": {
        "input": 5000000,
        "output": 20000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "flex": {
        "input": 1250000,
        "output": 5000000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 5000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-audio-mini",
      "standard": {
        "input": 600000,
        "output": 2400000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 600000,
        "image_output": 2400000
      },
      "priority": {
        "input": 1200000,
        "output": 4800000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1200000,
        "image_output": 4800000
      },
      "flex": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-audio-mini-2025-10-06",
      "standard": {
        "input": 600000,
        "output": 2400000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 600000,
        "image_output": 2400000
      },
      "priority": {
        "input": 1200000,
        "output": 4800000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1200000,
        "image_output": 4800000
      },
      "flex": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-audio-mini-2025-12-15",
      "standard": {
        "input": 600000,
        "output": 2400000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 600000,
        "image_output": 2400000
      },
      "priority": {
        "input": 1200000,
        "output": 4800000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1200000,
        "image_output": 4800000
      },
      "flex": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-image-1",
      "standard": {
        "input": 5000000,
        "output": 0,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 10000000,
        "image_output": 40000000
      },
      "priority": {
        "input": 10000000,
        "output": 0,
        "cache_read": 2500000,
        "cache_write": 0,
        "image_input": 20000000,
        "image_output": 80000000
      },
      "flex": {
        "input": 2500000,
        "output": 0,
        "cache_read": 625000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 20000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-image-1-mini",
      "standard": {
        "input": 2000000,
        "output": 0,
        "cache_read": 200000,
        "cache_write": 0,
        "image_input": 2500000,
        "image_output": 8000000
      },
      "priority": {
        "input": 4000000,
        "output": 0,
        "cache_read": 400000,
        "cache_write": 0,
        "image_input": 5000000,
        "image_output": 16000000
      },
      "flex": {
        "input": 1000000,
        "output": 0,
        "cache_read": 100000,
        "cache_write": 0,
        "image_input": 1250000,
        "image_output": 4000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-image-1.5",
      "standard": {
        "input": 5000000,
        "output": 10000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 8000000,
        "image_output": 32000000
      },
      "priority": {
        "input": 10000000,
        "output": 20000000,
        "cache_read": 2500000,
        "cache_write": 0,
        "image_input": 16000000,
        "image_output": 64000000
      },
      "flex": {
        "input": 2500000,
        "output": 5000000,
        "cache_read": 625000,
        "cache_write": 0,
        "image_input": 4000000,
        "image_output": 16000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-image-1.5-2025-12-16",
      "standard": {
        "input": 5000000,
        "output": 10000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 8000000,
        "image_output": 32000000
      },
      "priority": {
        "input": 10000000,
        "output": 20000000,
        "cache_read": 2500000,
        "cache_write": 0,
        "image_input": 16000000,
        "image_output": 64000000
      },
      "flex": {
        "input": 2500000,
        "output": 5000000,
        "cache_read": 625000,
        "cache_write": 0,
        "image_input": 4000000,
        "image_output": 16000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-image-2",
      "standard": {
        "input": 5000000,
        "output": 10000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 8000000,
        "image_output": 30000000
      },
      "priority": {
        "input": 10000000,
        "output": 20000000,
        "cache_read": 2500000,
        "cache_write": 0,
        "image_input": 16000000,
        "image_output": 60000000
      },
      "flex": {
        "input": 2500000,
        "output": 5000000,
        "cache_read": 625000,
        "cache_write": 0,
        "image_input": 4000000,
        "image_output": 15000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-image-2-2026-04-21",
      "standard": {
        "input": 5000000,
        "output": 10000000,
        "cache_read": 1250000,
        "cache_write": 0,
        "image_input": 8000000,
        "image_output": 30000000
      },
      "priority": {
        "input": 10000000,
        "output": 20000000,
        "cache_read": 2500000,
        "cache_write": 0,
        "image_input": 16000000,
        "image_output": 60000000
      },
      "flex": {
        "input": 2500000,
        "output": 5000000,
        "cache_read": 625000,
        "cache_write": 0,
        "image_input": 4000000,
        "image_output": 15000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-realtime",
      "standard": {
        "input": 4000000,
        "output": 16000000,
        "cache_read": 400000,
        "cache_write": 0,
        "image_input": 4000000,
        "image_output": 16000000
      },
      "priority": {
        "input": 8000000,
        "output": 32000000,
        "cache_read": 800000,
        "cache_write": 0,
        "image_input": 8000000,
        "image_output": 32000000
      },
      "flex": {
        "input": 2000000,
        "output": 8000000,
        "cache_read": 200000,
        "cache_write": 0,
        "image_input": 2000000,
        "image_output": 8000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-realtime-1.5",
      "standard": {
        "input": 4000000,
        "output": 16000000,
        "cache_read": 400000,
        "cache_write": 0,
        "image_input": 4000000,
        "image_output": 16000000
      },
      "priority": {
        "input": 8000000,
        "output": 32000000,
        "cache_read": 800000,
        "cache_write": 0,
        "image_input": 8000000,
        "image_output": 32000000
      },
      "flex": {
        "input": 2000000,
        "output": 8000000,
        "cache_read": 200000,
        "cache_write": 0,
        "image_input": 2000000,
        "image_output": 8000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-realtime-2",
      "standard": {
        "input": 4000000,
        "output": 16000000,
        "cache_read": 400000,
        "cache_write": 0,
        "image_input": 4000000,
        "image_output": 16000000
      },
      "priority": {
        "input": 8000000,
        "output": 32000000,
        "cache_read": 800000,
        "cache_write": 0,
        "image_input": 8000000,
        "image_output": 32000000
      },
      "flex": {
        "input": 2000000,
        "output": 8000000,
        "cache_read": 200000,
        "cache_write": 0,
        "image_input": 2000000,
        "image_output": 8000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-realtime-2025-08-28",
      "standard": {
        "input": 4000000,
        "output": 16000000,
        "cache_read": 400000,
        "cache_write": 0,
        "image_input": 4000000,
        "image_output": 16000000
      },
      "priority": {
        "input": 8000000,
        "output": 32000000,
        "cache_read": 800000,
        "cache_write": 0,
        "image_input": 8000000,
        "image_output": 32000000
      },
      "flex": {
        "input": 2000000,
        "output": 8000000,
        "cache_read": 200000,
        "cache_write": 0,
        "image_input": 2000000,
        "image_output": 8000000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-realtime-mini",
      "standard": {
        "input": 600000,
        "output": 2400000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 600000,
        "image_output": 2400000
      },
      "priority": {
        "input": 1200000,
        "output": 4800000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 1200000,
        "image_output": 4800000
      },
      "flex": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 0,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-realtime-mini-2025-10-06",
      "standard": {
        "input": 600000,
        "output": 2400000,
        "cache_read": 60000,
        "cache_write": 0,
        "image_input": 600000,
        "image_output": 2400000
      },
      "priority": {
        "input": 1200000,
        "output": 4800000,
        "cache_read": 120000,
        "cache_write": 0,
        "image_input": 1200000,
        "image_output": 4800000
      },
      "flex": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 30000,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    },
    {
      "model": "gpt-realtime-mini-2025-12-15",
      "standard": {
        "input": 600000,
        "output": 2400000,
        "cache_read": 60000,
        "cache_write": 0,
        "image_input": 600000,
        "image_output": 2400000
      },
      "priority": {
        "input": 1200000,
        "output": 4800000,
        "cache_read": 120000,
        "cache_write": 0,
        "image_input": 1200000,
        "image_output": 4800000
      },
      "flex": {
        "input": 300000,
        "output": 1200000,
        "cache_read": 30000,
        "cache_write": 0,
        "image_input": 300000,
        "image_output": 1200000
      },
      "long_context_tokens": 0,
      "long_input_bps": 10000,
      "long_output_bps": 10000
    }
  ],
  "web_search_micros": 10000,
  "image_1k_micros": 134000,
  "image_2k_micros": 201000,
  "image_4k_micros": 268000
}
$pricing$::jsonb);
INSERT INTO current_pricing(singleton,version_id) SELECT true,id FROM pricing_versions;
