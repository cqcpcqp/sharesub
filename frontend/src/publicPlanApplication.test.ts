import { describe, expect, it } from 'vitest'
import { canApplyToPublicPlan } from './publicPlanApplication'

describe('public Plan application state', () => {
  it('allows a first application and a retry after rejection', () => {
    expect(canApplyToPublicPlan('')).toBe(true)
    expect(canApplyToPublicPlan('rejected')).toBe(true)
  })

  it('does not allow duplicate pending or approved applications', () => {
    expect(canApplyToPublicPlan('pending')).toBe(false)
    expect(canApplyToPublicPlan('approved')).toBe(false)
  })
})
