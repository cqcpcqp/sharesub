import { describe, expect, it } from 'vitest'
import { appRoutePath, parseAppRoute, viewPaths } from './appRoutes'

describe('app routes', () => {
  it('maps every first-level view to a stable path', () => {
    expect(viewPaths).toEqual({
      dashboard: '/dashboard',
      lobby: '/lobby',
      plans: '/plans',
      accounts: '/accounts',
      keys: '/keys',
      profile: '/profile',
    })
    for (const view of Object.keys(viewPaths) as (keyof typeof viewPaths)[]) {
      expect(parseAppRoute(appRoutePath({ kind: 'view', view }))).toEqual({ kind: 'view', view })
    }
  })

  it('recognizes the login route and harmless trailing slashes', () => {
    expect(parseAppRoute('/login')).toEqual({ kind: 'login' })
    expect(parseAppRoute('/plans/')).toEqual({ kind: 'view', view: 'plans' })
  })

  it('does not invent a route for unknown paths', () => {
    expect(parseAppRoute('/')).toBeNull()
    expect(parseAppRoute('/unknown')).toBeNull()
  })
})
