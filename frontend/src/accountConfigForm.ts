import type { AccountConfigInput } from './types'

export type AccountTextField = 'name' | 'notes' | 'proxy_url'

export function updateAccountText(
  modelValue: AccountConfigInput,
  field: AccountTextField,
  value: string,
): AccountConfigInput {
  return { ...modelValue, [field]: value }
}
