import type { Member } from '../types'

export function restoreMemberQuotaDraft(member: Member, shares: Record<string, number>, usdLimits: Record<string, number | null>) {
  shares[member.id] = Math.round(member.share_basis_points / 100)
  usdLimits[member.id] = member.usd_limit_micros === null ? null : member.usd_limit_micros / 1_000_000
}

export function syncMemberQuotaDrafts(members: Member[], shares: Record<string, number>, usdLimits: Record<string, number | null>) {
  for (const memberID of Object.keys(shares)) delete shares[memberID]
  for (const memberID of Object.keys(usdLimits)) delete usdLimits[memberID]
  for (const member of members) restoreMemberQuotaDraft(member, shares, usdLimits)
}

export function memberUSDLimitMicros(value: number | null): number | null {
  if (value === null) return null
  const micros = Math.round(value * 1_000_000)
  if (!Number.isSafeInteger(micros) || micros <= 0) {
    throw new Error('美元额度上限必须为大于 0 的有效金额；清空表示不限制')
  }
  return micros
}
