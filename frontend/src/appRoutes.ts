export type ViewID = 'dashboard' | 'lobby' | 'plans' | 'accounts' | 'keys' | 'admin' | 'profile'
export type PublicPageID = 'home' | 'terms' | 'privacy' | 'acceptable-use'

export type AppRoute =
  | { kind: 'login' }
  | { kind: 'public'; page: PublicPageID }
  | { kind: 'view'; view: ViewID }
  | { kind: 'admin-plan'; id: string }
  | { kind: 'admin-account'; id: string }

export const publicPagePaths: Record<PublicPageID, string> = {
  home: '/',
  terms: '/terms',
  privacy: '/privacy',
  'acceptable-use': '/acceptable-use',
}

export const viewPaths: Record<ViewID, string> = {
  dashboard: '/dashboard',
  lobby: '/lobby',
  plans: '/plans',
  accounts: '/accounts',
  keys: '/keys',
  admin: '/admin',
  profile: '/profile',
}

function decodeRouteID(value: string) {
  try {
    return decodeURIComponent(value)
  } catch {
    return null
  }
}

export function parseAppRoute(pathname: string): AppRoute | null {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, '') : pathname
  if (path === '/login') return { kind: 'login' }
  const adminPlanMatch = path.match(/^\/admin\/plans\/([^/]+)$/)
  if (adminPlanMatch) {
    const id = decodeRouteID(adminPlanMatch[1])
    return id === null ? null : { kind: 'admin-plan', id }
  }
  const adminAccountMatch = path.match(/^\/admin\/accounts\/([^/]+)$/)
  if (adminAccountMatch) {
    const id = decodeRouteID(adminAccountMatch[1])
    return id === null ? null : { kind: 'admin-account', id }
  }
  const publicEntry = Object.entries(publicPagePaths).find(([, routePath]) => routePath === path)
  if (publicEntry) return { kind: 'public', page: publicEntry[0] as PublicPageID }
  const entry = Object.entries(viewPaths).find(([, routePath]) => routePath === path)
  return entry ? { kind: 'view', view: entry[0] as ViewID } : null
}

export function appRoutePath(route: AppRoute): string {
  if (route.kind === 'login') return '/login'
  if (route.kind === 'admin-plan') return `/admin/plans/${encodeURIComponent(route.id)}`
  if (route.kind === 'admin-account') return `/admin/accounts/${encodeURIComponent(route.id)}`
  return route.kind === 'public' ? publicPagePaths[route.page] : viewPaths[route.view]
}
