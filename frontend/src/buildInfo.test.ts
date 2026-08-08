import { describe, expect, it } from 'vitest'
import { buildInfo, shortRevision } from './buildInfo'

describe('buildInfo', () => {
  it('exposes the injected release identity', () => {
    expect(buildInfo.version).toMatch(/^\d+\.\d+\.\d+/)
    expect(buildInfo.revision).toMatch(/^[0-9a-f]{40}$/)
  })

  it('uses twelve characters for compact revision labels', () => {
    expect(shortRevision('1234567890abcdef')).toBe('1234567890ab')
  })
})
