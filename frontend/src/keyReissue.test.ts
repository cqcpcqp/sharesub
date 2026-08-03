import { describe, expect, it } from 'vitest'
import { availableKeyRoutes, canUpgradeAPIKey } from './keyReissue'
import type { APIKey, Plan } from './types'

const route = { plan_id: 'plan-active', plan_name: '共享 Plan', priority: 20, enabled: true }
const plan = { id: 'plan-active' } as Plan

function apiKey(overrides: Partial<APIKey> = {}): APIKey {
  return {
    id: 'key',
    user_id: 'user',
    name: '我的 Codex',
    key: '',
    key_available: false,
    key_prefix: 'sk-sharesub-old',
    strategy: 'balanced',
    status: 'active',
    created_at: '2026-08-03T00:00:00Z',
    routes: [route],
    ...overrides,
  }
}

describe('legacy API key upgrade availability', () => {
  it('keeps the current enabled route configuration for a historical key', () => {
    expect(availableKeyRoutes(apiKey(), [plan])).toEqual([route])
    expect(canUpgradeAPIKey(apiKey(), [plan])).toBe(true)
  })

  it('does not offer upgrade for an already recoverable key', () => {
    expect(canUpgradeAPIKey(apiKey({ key: 'sk-sharesub-new', key_available: true }), [plan])).toBe(false)
  })

  it('rejects revoked keys and historical keys without an available plan', () => {
    expect(canUpgradeAPIKey(apiKey({ status: 'revoked' }), [plan])).toBe(false)
    expect(canUpgradeAPIKey(apiKey(), [])).toBe(false)
  })
})
