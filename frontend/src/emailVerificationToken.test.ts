import { describe, expect, it } from 'vitest'
import { emailVerificationHash, parseEmailVerificationToken } from './emailVerificationToken'

const token = `ss_verify_${'a'.repeat(43)}`

describe('email verification token fragments', () => {
  it('round trips a valid opaque token', () => {
    expect(parseEmailVerificationToken(emailVerificationHash(token))).toBe(token)
  })

  it('rejects missing, malformed, and unrelated fragments', () => {
    expect(parseEmailVerificationToken('')).toBe('')
    expect(parseEmailVerificationToken('#token=short')).toBe('')
    expect(parseEmailVerificationToken(`#/invite/${token}`)).toBe('')
  })
})
