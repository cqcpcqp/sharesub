import type { Plan } from './types'

export function isPlanRoutable(plan: Plan): boolean {
  return plan.status === 'active' && plan.account_id !== ''
}
