export type ViewID = 'dashboard' | 'lobby' | 'plans' | 'accounts' | 'keys' | 'admin' | 'profile'
export type PublicPageID = 'home' | 'terms' | 'privacy' | 'acceptable-use'

export type AppRoute =
  | { kind: 'login' }
  | { kind: 'public'; page: PublicPageID }
  | { kind: 'view'; view: ViewID }

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

export function parseAppRoute(pathname: string): AppRoute | null {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, '') : pathname
  if (path === '/login') return { kind: 'login' }
  const publicEntry = Object.entries(publicPagePaths).find(([, routePath]) => routePath === path)
  if (publicEntry) return { kind: 'public', page: publicEntry[0] as PublicPageID }
  const entry = Object.entries(viewPaths).find(([, routePath]) => routePath === path)
  return entry ? { kind: 'view', view: entry[0] as ViewID } : null
}

export function appRoutePath(route: AppRoute): string {
  if (route.kind === 'login') return '/login'
  return route.kind === 'public' ? publicPagePaths[route.page] : viewPaths[route.view]
}
