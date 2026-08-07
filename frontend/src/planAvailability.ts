import type { Plan } from './types'

export function isPlanRoutable(plan: Plan): boolean {
  return plan.status === 'active' && plan.account_id !== ''
}

export function canMemberRoutePlan(plan: Plan, shareBasisPoints: number): boolean {
  return isPlanRoutable(plan) && (plan.allocation_mode === 'shared' || shareBasisPoints > 0)
}
