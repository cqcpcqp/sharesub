import { describe, expect, it } from 'vitest'
import { updateAccountText } from './accountConfigForm'
import type { AccountConfigInput } from './types'

describe('AccountConfigFields', () => {
  it.each(['name', 'notes', 'proxy_url'] as const)('emits an empty %s value', field => {
    const modelValue: AccountConfigInput = {
      name: '团队主账号',
      notes: '共享账号',
      proxy_url: 'http://127.0.0.1:7890',
      max_concurrency: 2,
      rpm_limit: 60,
      fast_policy: [],
      status: 'active',
    }
    expect(updateAccountText(modelValue, field, '')).toEqual({ ...modelValue, [field]: '' })
  })
})
