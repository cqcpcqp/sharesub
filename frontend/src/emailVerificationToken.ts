const verificationTokenPattern = /^ss_verify_[A-Za-z0-9_-]{43}$/

export function parseEmailVerificationToken(hash: string): string {
  if (!hash.startsWith('#')) return ''
  const token = new URLSearchParams(hash.slice(1)).get('token') ?? ''
  return verificationTokenPattern.test(token) ? token : ''
}

export function emailVerificationHash(token: string): string {
  if (!verificationTokenPattern.test(token)) throw new Error('invalid email verification token')
  return `#token=${encodeURIComponent(token)}`
}
