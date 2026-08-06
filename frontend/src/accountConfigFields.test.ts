// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { updateAccountText } from './accountConfigForm'
import type { AccountConfigInput } from './types'
import AccountConfigFields from './components/AccountConfigFields.vue'

const modelValue: AccountConfigInput = {
  name: '团队主账号', notes: '共享账号', proxy_url: 'http://127.0.0.1:7890',
  max_concurrency: 2, rpm_limit: 60, fast_policy: [], status: 'active',
}

describe('AccountConfigFields', () => {
  it.each(['name', 'notes', 'proxy_url'] as const)('emits an empty %s value', field => {
    expect(updateAccountText(modelValue, field, '')).toEqual({ ...modelValue, [field]: '' })
  })

  it('explains the default, Fast, and Flex processing modes', () => {
    const wrapper = mount(AccountConfigFields, { props: { modelValue } })
    expect(wrapper.text()).toContain('不会自动开启任何模式')
    expect(wrapper.text()).toContain('更高倍率消耗 Codex 额度')
    expect(wrapper.text()).toContain('响应更慢且可能暂时无可用资源')
    expect(wrapper.text()).toContain('OpenAI 默认处理')
  })
})
