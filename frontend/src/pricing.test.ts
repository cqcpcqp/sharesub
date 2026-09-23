import { describe, expect, it } from 'vitest'
import { pricingChanges, restorePricingConfig, type ModelPrice, type PricingConfig } from './pricing'

function modelPrice(model: string, input: number): ModelPrice {
  return {
    model,
    standard: { input, output: input * 5, cache_read: input / 10, cache_write: input * 1.25, image_input: input, image_output: input * 5 },
    long_context_tokens: 272000, long_input_bps: 20000, long_output_bps: 15000,
  }
}

function config(models: ModelPrice[]): PricingConfig {
  return { models, fast_multiplier_bps: 20000, flex_multiplier_bps: 5000, web_search_micros: 10000, image_1k_micros: 134000, image_2k_micros: 201000, image_4k_micros: 268000 }
}

describe('restorePricingConfig', () => {
  it('restores historical rates and global settings while preserving new model prices', () => {
    const historical = config([modelPrice('gpt-6-astra', 8000000)])
    const current = config([modelPrice('gpt-6-astra', 10000000), modelPrice('gpt-6-sol', 3000000), modelPrice('gpt-6-luna', 150000)])
    current.fast_multiplier_bps = 30000
    current.flex_multiplier_bps = 7000
    current.web_search_micros = 20000
    current.image_1k_micros = 140000
    current.image_2k_micros = 210000
    current.image_4k_micros = 280000
    const restored = restorePricingConfig(current, historical)
    expect(restored).toEqual({ ...historical, models: [historical.models[0], current.models[1], current.models[2]] })
    expect(restored.models.map(model => model.model)).toEqual(current.models.map(model => model.model))
    expect(pricingChanges(current, restored).some(change => change.item === 'gpt-6-astra')).toBe(true)
    expect(pricingChanges(current, restored).some(change => ['gpt-6-sol', 'gpt-6-luna'].includes(change.item))).toBe(false)
    restored.models[0].standard.input = 1
    restored.models[1].standard.input = 1
    restored.models[2].long_context_tokens = 1
    expect(historical.models[0].standard.input).toBe(8000000)
    expect(current.models[1].standard.input).toBe(3000000)
    expect(current.models[2].long_context_tokens).toBe(272000)
  })

  it('restores all settings when the model catalog is unchanged', () => {
    const historical = config([modelPrice('gpt-6-sol', 2000000)])
    const current = config([modelPrice('gpt-6-sol', 3000000)])
    expect(restorePricingConfig(current, historical)).toEqual(historical)
  })

  it('matches models by ID and does not reintroduce removed models', () => {
    const historical = config([modelPrice('removed-model', 100), modelPrice('gpt-6-luna', 100000), modelPrice('gpt-6-sol', 2000000)])
    const current = config([modelPrice('gpt-6-sol', 3000000), modelPrice('gpt-6-luna', 150000)])
    expect(restorePricingConfig(current, historical).models).toEqual([historical.models[2], historical.models[1]])
  })
})
