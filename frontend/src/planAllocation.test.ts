import { describe, expect, it } from 'vitest'
import { allocationModeLabel, allocationShareBasisPoints, formatShareBasisPoints } from './planAllocation'

describe('plan allocation', () => {
  it('stores fixed allocation as a whole percentage', () => {
    expect(allocationShareBasisPoints('fixed', 12.34)).toBe(1200)
    expect(allocationShareBasisPoints('fixed', 12.67)).toBe(1300)
  })

  it('always sends zero share for shared allocation', () => {
    expect(allocationShareBasisPoints('shared', 12.34)).toBe(0)
  })

  it('provides concise mode labels', () => {
    expect(allocationModeLabel('fixed')).toBe('固定分配')
    expect(allocationModeLabel('shared')).toBe('共享使用')
  })

  it('formats basis points without decimal noise', () => {
    expect(formatShareBasisPoints(2500)).toBe('25%')
    expect(formatShareBasisPoints(3333)).toBe('33%')
  })
})
