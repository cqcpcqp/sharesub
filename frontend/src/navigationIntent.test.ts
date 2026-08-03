import { describe, expect, it } from 'vitest'
import { inviteIntentHash, locationWithoutHash, parseNavigationIntent } from './navigationIntent'

describe('navigation intent', () => {
  it('round trips an invitation token through the hash route', () => {
    const token = `ss_invite_${'aB_9-'.repeat(8)}xyz`
    expect(parseNavigationIntent(inviteIntentHash(token))).toEqual({ kind: 'invite', token })
  })

  it('rejects unrelated, malformed, and non-invitation hashes', () => {
    expect(parseNavigationIntent('#/plans/plan-id')).toBeNull()
    expect(parseNavigationIntent('#/invite/not-an-invite')).toBeNull()
    expect(parseNavigationIntent('#/invite/%E0%A4%A')).toBeNull()
    expect(parseNavigationIntent('#/invite/ss_invite_one/more')).toBeNull()
    expect(parseNavigationIntent('#/invite/ss_invite_one?source=chat')).toBeNull()
  })

  it('removes only the hash when an invitation is consumed', () => {
    expect(locationWithoutHash('/app', '?source=friend')).toBe('/app?source=friend')
  })
})
