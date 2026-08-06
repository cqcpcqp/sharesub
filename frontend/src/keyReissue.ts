import type { APIKey, APIKeyRoute, Plan } from './types'
import { isPlanRoutable } from './planAvailability'

export function availableKeyRoutes(key: APIKey, plans: Plan[]): APIKeyRoute[] {
  const planIDs = new Set(plans.filter(isPlanRoutable).map(plan => plan.id))
  return key.routes.filter(route => route.enabled && planIDs.has(route.plan_id))
}

export function canUpgradeAPIKey(key: APIKey, plans: Plan[]): boolean {
  return key.status === 'active' && !key.key_available && availableKeyRoutes(key, plans).length > 0
}
