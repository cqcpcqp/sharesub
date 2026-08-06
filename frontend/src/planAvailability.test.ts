import { describe, expect, it } from 'vitest'
import { isPlanRoutable } from './planAvailability'
import type { Plan } from './types'

const plan: Plan = {
  id: 'plan',
  owner_user_id: 'owner',
  account_id: 'account',
  name: '共享 Plan',
  description: '',
  status: 'active',
  visibility: 'private',
  public_slots: 0,
  public_share_basis_points: 0,
  allocation_mode: 'shared',
  created_at: '2026-08-06T00:00:00Z',
}

describe('Plan route availability', () => {
  it('requires both an active Plan and a bound account', () => {
    expect(isPlanRoutable(plan)).toBe(true)
    expect(isPlanRoutable({ ...plan, account_id: '' })).toBe(false)
    expect(isPlanRoutable({ ...plan, status: 'archived' })).toBe(false)
  })
})
