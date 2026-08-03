// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { Account, Member } from '../types'
import AccountConfigSummary from './AccountConfigSummary.vue'

const account: Account = {
  id: 'account', owner_user_id: 'owner', name: '团队账号', notes: '', email: 'openai@example.com',
  chatgpt_account_id: 'chatgpt-account', plan_type: 'pro', proxy_url: '', max_concurrency: 0, rpm_limit: 0,
  fast_policy: [{
    service_tier: 'priority', action: 'filter', user_ids: ['member'], error_message: '',
    model_whitelist: ['gpt-5.5*'], fallback_action: 'pass', fallback_error_message: '',
  }],
  token_expires_at: '2026-08-13T02:46:00Z', status: 'active', created_at: '2026-08-01T00:00:00Z',
}

const member: Member = {
  id: 'plan-member', plan_id: 'plan', user_id: 'member', username: 'alice', email: 'alice@example.com',
  avatar_url: '', role: 'member', status: 'active', share_basis_points: 5000, created_at: '2026-08-01T00:00:00Z',
}

describe('AccountConfigSummary', () => {
  it('shows Fast/Flex policy details with resolved member identity', () => {
    const wrapper = mount(AccountConfigSummary, { props: { account, members: [member] } })
    expect(wrapper.text()).toContain('OpenAI Fast/Flex 策略')
    expect(wrapper.text()).toContain('priority（fast）')
    expect(wrapper.text()).toContain('过滤 service_tier')
    expect(wrapper.text()).toContain('alice · alice@example.com')
    expect(wrapper.text()).toContain('gpt-5.5*')
  })

  it('shows the passthrough state when no rules are configured', () => {
    const wrapper = mount(AccountConfigSummary, { props: { account: { ...account, fast_policy: [] }, members: [] } })
    expect(wrapper.text()).toContain('未配置策略')
    expect(wrapper.text()).toContain('原样透传')
  })
})
