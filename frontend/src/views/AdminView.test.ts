// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api'
import type { AdminOverview, User } from '../types'
import AdminView from './AdminView.vue'

const admin: User = { id: 'admin', username: '管理员', email: 'admin@example.com', avatar_url: '', status: 'active', created_at: '2026-08-04T00:00:00Z', is_admin: true, role: 'admin', must_change_password: false }
const overview: AdminOverview = {
  user_count: 2, active_user_count: 2, account_count: 1, active_accounts: 1,
  plan_count: 1, active_plans: 1, api_key_count: 1, active_api_keys: 1,
  requests_24h: 12, tokens_24h: 3456, cost_micros_24h: 125000, success_rate_24h: 100,
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
})
