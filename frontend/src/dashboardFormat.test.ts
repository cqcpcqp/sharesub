import { describe, expect, it } from 'vitest'
import { formatDuration, formatPercent, formatTokens } from './dashboardFormat'

describe('dashboard formatting', () => {
  it('formats token counts with compact units', () => {
    expect(formatTokens(0)).toBe('0')
    expect(formatTokens(1_250)).toBe('1.25K')
    expect(formatTokens(12_030_000)).toBe('12.03M')
    expect(formatTokens(6_010_000_000)).toBe('6.01B')
  })

  it('formats latency and success rate without noisy zeroes', () => {
    expect(formatDuration(428)).toBe('428 ms')
    expect(formatDuration(15_310)).toBe('15.31 s')
    expect(formatPercent(99.5)).toBe('99.5%')
  })
})
