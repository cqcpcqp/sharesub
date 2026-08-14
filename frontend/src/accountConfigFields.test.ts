// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { updateAccountText } from './accountConfigForm'
import type { AccountConfigInput } from './types'
import AccountConfigFields from './components/AccountConfigFields.vue'

const modelValue: AccountConfigInput = {
  name: '团队主账号', notes: '共享账号', proxy_url: 'http://127.0.0.1:7890',
  max_concurrency: 2, rpm_limit: 60, fast_policy: [], codex_fingerprint_mode: 'session', status: 'active',
}

describe('AccountConfigFields', () => {
  it.each(['name', 'notes', 'proxy_url'] as const)('emits an empty %s value', field => {
    expect(updateAccountText(modelValue, field, '')).toEqual({ ...modelValue, [field]: '' })
  })

  it('explains the default, Fast, and Flex processing modes', () => {
    const wrapper = mount(AccountConfigFields, { props: { modelValue } })
    expect(wrapper.text()).toContain('命中“强制 Fast”时主动添加 service_tier')
    expect(wrapper.text()).toContain('发送兼容值 priority')
    expect(wrapper.text()).toContain('响应更慢且可能暂时无可用资源')
    expect(wrapper.text()).toContain('继续执行成员 Key 的规则')
    expect(wrapper.text()).toContain('设备 + 会话（推荐）')
    expect(wrapper.text()).toContain('按 API Key 与客户端会话隔离对话')
    expect(wrapper.text()).toContain('查看四种模式区别')
    expect(wrapper.text()).toContain('不改写标识')
    expect(wrapper.text()).toContain('不同客户端会共用对话标识')
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
