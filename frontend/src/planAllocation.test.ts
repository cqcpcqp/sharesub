import { describe, expect, it } from 'vitest'
import { allocationModeLabel, allocationShareBasisPoints, formatShareBasisPoints, planPublicationShareBasisPoints, planReservedShareBasisPoints } from './planAllocation'
import type { PlanDetail } from './types'

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

  it('calculates member, pending invite, and public seat reservations', () => {
    const detail = {
      plan: { public_slots: 3, public_share_basis_points: 500 },
      members: [
        { id: 'active-member', status: 'active', share_basis_points: 2500 },
        { id: 'removed-member', status: 'removed', share_basis_points: 900 },
      ],
      invites: [
        { status: 'pending', share_basis_points: 1000, expires_at: '2026-08-08T00:00:00Z' },
        { status: 'pending', share_basis_points: 800, expires_at: '2026-08-06T00:00:00Z' },
        { status: 'accepted', share_basis_points: 700, expires_at: '2026-08-08T00:00:00Z' },
      ],
      applications: [
        { status: 'approved', member_id: 'active-member' },
        { status: 'approved', member_id: 'removed-member' },
      ],
    } as unknown as PlanDetail

    expect(planReservedShareBasisPoints(detail, new Date('2026-08-07T00:00:00Z').getTime())).toEqual({
      members: 2500,
      pendingInvites: 1000,
      publicSlots: 1000,
      total: 4500,
      remaining: 5500,
    })
  })

  it('calculates a proposed public allocation independently from the saved publication', () => {
    const detail = {
      plan: { public_slots: 0, public_share_basis_points: 0 },
      members: [{ id: 'owner', status: 'active', share_basis_points: 100 }],
      invites: [],
      applications: [],
    } as unknown as PlanDetail

    expect(planPublicationShareBasisPoints(detail, 3, 2500)).toEqual({
      members: 100,
      pendingInvites: 0,
      publicSlots: 7500,
      total: 7600,
      remaining: 2400,
    })
  })
})
