export type ViewID = 'dashboard' | 'lobby' | 'plans' | 'accounts' | 'keys' | 'admin' | 'profile'

export type AppRoute =
  | { kind: 'login' }
  | { kind: 'view'; view: ViewID }

export const viewPaths: Record<ViewID, string> = {
  dashboard: '/dashboard',
  lobby: '/lobby',
  plans: '/plans',
  accounts: '/accounts',
  keys: '/keys',
  admin: '/admin',
  profile: '/profile',
}

export function parseAppRoute(pathname: string): AppRoute | null {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, '') : pathname
  if (path === '/login') return { kind: 'login' }
  const entry = Object.entries(viewPaths).find(([, routePath]) => routePath === path)
  return entry ? { kind: 'view', view: entry[0] as ViewID } : null
}

export function appRoutePath(route: AppRoute): string {
  return route.kind === 'login' ? '/login' : viewPaths[route.view]
}
