import type { AccountConfigInput } from './types'

export type CodexFingerprintMode = AccountConfigInput['codex_fingerprint_mode']

export const codexFingerprintModes: Array<{
  value: CodexFingerprintMode
  label: string
  description: string
}> = [
  {
    value: 'off',
    label: '关闭（透传）',
    description: '不改写标识，保留各客户端原本的设备、会话和对话身份。',
  },
  {
    value: 'device',
    label: '仅设备',
    description: '统一为同一设备；各客户端的会话和对话标识保持独立。',
  },
  {
    value: 'session',
    label: '设备 + 会话（推荐）',
    description: '统一设备和会话；按 API Key 与客户端会话隔离对话，适合共享账号。',
  },
  {
    value: 'full',
    label: '完全收敛',
    description: '设备、会话和对话全部统一；不同客户端会共用对话标识，仅建议特殊场景使用。',
  },
]

export const codexFingerprintOptions = codexFingerprintModes.map(({ value, label }) => ({ value, label }))

export function codexFingerprintMode(mode: CodexFingerprintMode) {
  return codexFingerprintModes.find(candidate => candidate.value === mode)!
}
