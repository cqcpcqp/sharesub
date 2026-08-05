import { describe, expect, it } from 'vitest'
import { appRoutePath, parseAppRoute, publicPagePaths, viewPaths } from './appRoutes'

describe('app routes', () => {
  it('maps every first-level view to a stable path', () => {
    expect(viewPaths).toEqual({
      dashboard: '/dashboard',
      lobby: '/lobby',
      plans: '/plans',
      accounts: '/accounts',
      keys: '/keys',
      admin: '/admin',
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

  it('maps public pages without requiring an authenticated view', () => {
    expect(publicPagePaths).toEqual({
      home: '/',
      terms: '/terms',
      privacy: '/privacy',
      'acceptable-use': '/acceptable-use',
    })
    for (const page of Object.keys(publicPagePaths) as (keyof typeof publicPagePaths)[]) {
      expect(parseAppRoute(appRoutePath({ kind: 'public', page }))).toEqual({ kind: 'public', page })
    }
  })

  it('does not invent a route for unknown paths', () => {
    expect(parseAppRoute('/unknown')).toBeNull()
  })
})
