export function formatTokens(value: number): string {
  if (value >= 1_000_000_000) return `${trimZeros(value / 1_000_000_000)}B`
  if (value >= 1_000_000) return `${trimZeros(value / 1_000_000)}M`
  if (value >= 1_000) return `${trimZeros(value / 1_000)}K`
  return value.toLocaleString('zh-CN')
}

export function formatDuration(milliseconds: number): string {
  if (milliseconds < 1_000) return `${Math.round(milliseconds)} ms`
  return `${trimZeros(milliseconds / 1_000)} s`
}

export function formatPercent(value: number): string {
  return `${trimZeros(value)}%`
}

function trimZeros(value: number): string {
  return value.toFixed(2).replace(/\.00$/, '').replace(/(\.\d)0$/, '$1')
}
