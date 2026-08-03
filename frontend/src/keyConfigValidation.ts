export interface KeyRouteDraft {
  enabled: boolean
  priority: number | null
}

export function canSubmitKeyConfig(name: string, routes: KeyRouteDraft[]): boolean {
  const normalizedName = name.trim()
  const enabledRoutes = routes.filter(route => route.enabled)
  return normalizedName.length >= 1
    && normalizedName.length <= 100
    && enabledRoutes.length > 0
    && enabledRoutes.every(route => Number.isInteger(route.priority) && route.priority! >= 1 && route.priority! <= 10_000)
}
