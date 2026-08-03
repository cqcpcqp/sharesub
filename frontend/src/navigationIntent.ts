export interface InviteIntent {
  kind: 'invite'
  token: string
}

const invitePrefix = '#/invite/'

export function parseNavigationIntent(hash: string): InviteIntent | null {
  if (!hash.startsWith(invitePrefix)) return null
  const encodedToken = hash.slice(invitePrefix.length)
  if (!encodedToken || encodedToken.includes('/')) return null
  try {
    const token = decodeURIComponent(encodedToken)
    return /^ss_invite_[A-Za-z0-9_-]{43}$/.test(token) ? { kind: 'invite', token } : null
  } catch {
    return null
  }
}

export function inviteIntentHash(token: string): string {
  return `${invitePrefix}${encodeURIComponent(token)}`
}

export function locationWithoutHash(pathname: string, search: string): string {
  return `${pathname}${search}`
}
