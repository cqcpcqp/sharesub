// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api'
import type { AdminAccount, AdminOverview, User } from '../types'
import AdminView from './AdminView.vue'

const admin: User = { id: 'admin', username: '管理员', email: 'admin@example.com', avatar_url: '', status: 'active', created_at: '2026-08-04T00:00:00Z', is_admin: true, role: 'admin', must_change_password: false }
const overview: AdminOverview = {
  user_count: 2, active_user_count: 2, account_count: 1, active_accounts: 1,
  plan_count: 1, active_plans: 1, api_key_count: 1, active_api_keys: 1,
  requests_24h: 12, tokens_24h: 3456, cost_micros_24h: 125000, success_rate_24h: 100,
}
const account: AdminAccount = {
  id: 'account', owner_user_id: admin.id, owner_username: admin.username, owner_email: admin.email,
  name: '团队账号', notes: '', email: 'openai@example.com', chatgpt_account_id: 'chatgpt-account',
  plan_type: 'plus', subscription_expires_at: '2026-09-06T10:00:00Z', proxy_url: '', max_concurrency: 0,
  rpm_limit: 0, fast_policy: [], token_expires_at: '2026-08-13T02:46:00Z', status: 'active',
  last_error: '', created_at: '2026-08-01T00:00:00Z', plan_id: 'plan', plan_name: '团队 Plan',
}

afterEach(() => vi.restoreAllMocks())

describe('AdminView', () => {
  it('loads platform resources and protects the current administrator', async () => {
    vi.spyOn(api, 'adminOverview').mockResolvedValue(overview)
    vi.spyOn(api, 'adminUsers').mockResolvedValue([{ ...admin, account_count: 1, plan_count: 1, api_key_count: 1 }])
    vi.spyOn(api, 'adminAccounts').mockResolvedValue([])
    vi.spyOn(api, 'adminPlans').mockResolvedValue([])
    vi.spyOn(api, 'adminKeys').mockResolvedValue([])
    const wrapper = mount(AdminView, { props: { currentUser: admin }, global: { stubs: { teleport: true } } })
    await flushPromises()
    expect(api.adminOverview).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('后台管理')
    expect(wrapper.text()).toContain('3.46K')
    expect(wrapper.text()).toContain('管理员')
    const currentAccountButton = wrapper.findAll('button').find(button => button.text() === '当前账号')!
    expect(currentAccountButton.attributes('disabled')).toBeDefined()
  })

  it('does not expose OAuth token expiry in the account table', async () => {
    vi.spyOn(api, 'adminOverview').mockResolvedValue(overview)
    vi.spyOn(api, 'adminUsers').mockResolvedValue([])
    vi.spyOn(api, 'adminAccounts').mockResolvedValue([account])
    vi.spyOn(api, 'adminPlans').mockResolvedValue([])
    vi.spyOn(api, 'adminKeys').mockResolvedValue([])
    const wrapper = mount(AdminView, { props: { currentUser: admin }, global: { stubs: { teleport: true } } })
    await flushPromises()
    await wrapper.findAll('.n-tabs-tab').find(tab => tab.text().includes('账号'))!.trigger('click')
    expect(wrapper.text()).toContain('团队账号')
    expect(wrapper.text()).not.toContain('Token 到期')
    expect(wrapper.text()).not.toContain('2026/08/13')
  })
})
