// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import type { Account, Member } from '../types'
import AccountConfigSummary from './AccountConfigSummary.vue'

const featureStyles = readFileSync('src/featureStyles.css', 'utf8')

const account: Account = {
  id: 'account', owner_user_id: 'owner', name: '团队账号', notes: '', email: 'openai@example.com',
  chatgpt_account_id: 'chatgpt-account', plan_type: 'pro', proxy_url: '', max_concurrency: 0, rpm_limit: 0,
  fast_policy: [{
    service_tier: 'priority', action: 'filter', user_ids: ['member'], error_message: '',
    model_whitelist: ['gpt-5.5*'], fallback_action: 'pass', fallback_error_message: '',
  }],
  subscription_expires_at: '2026-09-06T10:00:00Z', token_expires_at: '2026-08-13T02:46:00Z', status: 'active', created_at: '2026-08-01T00:00:00Z',
}

const member: Member = {
  id: 'plan-member', plan_id: 'plan', user_id: 'member', username: 'alice', email: 'alice@example.com',
  avatar_url: '', role: 'member', status: 'active', share_basis_points: 5000, created_at: '2026-08-01T00:00:00Z',
}

describe('AccountConfigSummary', () => {
  it('shows Fast/Flex policy details with resolved member identity', () => {
    const wrapper = mount(AccountConfigSummary, { props: { account, members: [member] } })
    expect(wrapper.text()).toContain('OpenAI Fast/Flex 策略')
    expect(wrapper.text()).toContain('Fast（含 priority）')
    expect(wrapper.text()).toContain('过滤 service_tier')
    expect(wrapper.text()).toContain('alice · alice@example.com')
    expect(wrapper.text()).toContain('gpt-5.5*')
    expect(wrapper.text()).toContain('订阅有效期至')
    expect(wrapper.text()).toContain('2026')
    expect(wrapper.text()).not.toContain('OAuth Token 到期')
    expect(wrapper.text()).not.toContain('ShareSub ID')
    expect(wrapper.text()).not.toContain('Account ID')
    expect(wrapper.get('.fast-policy-summary-action').text()).toBe('过滤 service_tier')
  })

  it('keeps rule number styles scoped away from the action tag', () => {
    expect(featureStyles).not.toContain('.fast-policy-summary-rule > header > div > span')
    expect(featureStyles).toContain('.fast-policy-summary-action.n-tag { flex: 0 0 auto;')
  })

  it('shows the passthrough state when no rules are configured', () => {
    const wrapper = mount(AccountConfigSummary, { props: { account: { ...account, fast_policy: [] }, members: [] } })
    expect(wrapper.text()).toContain('未配置账号策略')
    expect(wrapper.text()).toContain('交由成员 Key 规则处理')
    expect(wrapper.text()).toContain('保留请求原选择')
  })

  it('shows when the account has no recorded subscription expiry', () => {
    const wrapper = mount(AccountConfigSummary, { props: { account: { ...account, plan_type: 'free', subscription_expires_at: null } } })
    expect(wrapper.text()).toContain('暂无订阅有效期')
  })
})
