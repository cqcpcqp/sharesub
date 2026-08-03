import type { PublicPlan } from './types'

export function canApplyToPublicPlan(status: PublicPlan['application_status']): boolean {
  return status === '' || status === 'rejected'
}
