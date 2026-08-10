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
    expect(wrapper.text()).toContain('命中“强制 Fast”时主动添加 service_tier')
    expect(wrapper.text()).toContain('使用最新官方值 fast')
    expect(wrapper.text()).toContain('响应更慢且可能暂时无可用资源')
    expect(wrapper.text()).toContain('继续执行成员 Key 的规则')
  })

  it('defaults new account rules to filtering Fast requests', async () => {
    const wrapper = mount(AccountConfigFields, { props: { modelValue } })

    await wrapper.get('button.fast-policy-add').trigger('click')

    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual({
      ...modelValue,
      fast_policy: [{
        service_tier: 'priority', action: 'filter', user_ids: [], error_message: '',
        model_whitelist: [], fallback_action: 'pass', fallback_error_message: '',
      }],
    })
  })
})
