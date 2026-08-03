import { describe, expect, it } from 'vitest'
import { canSubmitKeyConfig } from './keyConfigValidation'

describe('API key configuration validation', () => {
  it('requires a name and at least one enabled route', () => {
    expect(canSubmitKeyConfig('', [{ enabled: true, priority: 1 }])).toBe(false)
    expect(canSubmitKeyConfig('Codex', [{ enabled: false, priority: 1 }])).toBe(false)
  })

  it('rejects invalid priorities on enabled routes', () => {
    expect(canSubmitKeyConfig('Codex', [{ enabled: true, priority: null }])).toBe(false)
    expect(canSubmitKeyConfig('Codex', [{ enabled: true, priority: 0 }])).toBe(false)
    expect(canSubmitKeyConfig('Codex', [{ enabled: true, priority: 10_001 }])).toBe(false)
  })

  it('accepts valid enabled routes and ignores disabled priorities', () => {
    expect(canSubmitKeyConfig('Codex', [
      { enabled: true, priority: 1 },
      { enabled: false, priority: null },
    ])).toBe(true)
  })
})
