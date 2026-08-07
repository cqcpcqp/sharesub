import type { PlanAllocationMode, PlanDetail } from './types'

export const maxPlanShareBasisPoints = 10_000

export function allocationShareBasisPoints(mode: PlanAllocationMode, sharePercent: number): number {
  return mode === 'shared' ? 0 : Math.round(sharePercent) * 100
}

export function allocationModeLabel(mode: PlanAllocationMode): string {
  return mode === 'shared' ? '共享使用' : '固定分配'
}

export function formatShareBasisPoints(value: number): string {
  return `${Math.round(value / 100)}%`
}

export interface PlanShareReservation {
  members: number
  pendingInvites: number
  publicSlots: number
  total: number
  remaining: number
}

export function planPublicationShareBasisPoints(
  detail: PlanDetail,
  publicSlotCount: number,
  publicShareBasisPoints: number,
  now = Date.now(),
): PlanShareReservation {
  const members = detail.members
    .filter(member => member.status === 'active')
    .reduce((sum, member) => sum + member.share_basis_points, 0)
  const pendingInvites = detail.invites
    .filter(invite => invite.status === 'pending' && new Date(invite.expires_at).getTime() > now)
    .reduce((sum, invite) => sum + invite.share_basis_points, 0)
  const activeMemberIDs = new Set(detail.members
    .filter(member => member.status === 'active')
    .map(member => member.id))
  const approvedApplications = detail.applications
    .filter(application => application.status === 'approved' && activeMemberIDs.has(application.member_id!))
    .length
  const availablePublicSlots = Math.max(0, publicSlotCount - approvedApplications)
  const publicSlots = availablePublicSlots * publicShareBasisPoints
  const total = members + pendingInvites + publicSlots
  return { members, pendingInvites, publicSlots, total, remaining: Math.max(0, maxPlanShareBasisPoints - total) }
}

export function planReservedShareBasisPoints(detail: PlanDetail, now = Date.now()): PlanShareReservation {
  return planPublicationShareBasisPoints(
    detail,
    detail.plan.public_slots,
    detail.plan.public_share_basis_points,
    now,
  )
}
