// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api'
import { adminAPI } from '../api/admin'
import type { AdminAccount, AdminOverview, AdminPlan, User } from '../types'
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
  rpm_limit: 0, fast_policy: [], codex_fingerprint_mode: 'session', token_expires_at: '2026-08-13T02:46:00Z', status: 'active',
  last_error: '', created_at: '2026-08-01T00:00:00Z', plan_id: 'plan', plan_name: '团队 Plan',
}
const plan: AdminPlan = {
  id: 'plan', owner_user_id: admin.id, owner_username: admin.username, account_id: '', account_email: '', name: '团队 Plan', description: '',
  status: 'active', visibility: 'private', public_slots: 0, public_share_basis_points: 0, allocation_mode: 'shared', created_at: '2026-08-01T00:00:00Z',
  member_count: 1, requests_24h: 12, total_tokens_24h: 3456,
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

  it('opens the shared account editor instead of a dedicated detail page', async () => {
    const unboundAccount = { ...account, plan_id: '', plan_name: '' }
    vi.spyOn(api, 'adminOverview').mockResolvedValue(overview)
    vi.spyOn(api, 'adminUsers').mockResolvedValue([])
    vi.spyOn(api, 'adminAccounts').mockResolvedValue([unboundAccount])
    vi.spyOn(api, 'adminPlans').mockResolvedValue([])
    vi.spyOn(api, 'adminKeys').mockResolvedValue([])
    const update = vi.spyOn(adminAPI, 'adminUpdateAccount').mockResolvedValue(unboundAccount)
    const wrapper = mount(AdminView, { props: { currentUser: admin }, global: { stubs: { teleport: true } } })
    await flushPromises()
    await wrapper.findAll('.n-tabs-tab').find(tab => tab.text().includes('账号'))!.trigger('click')
    await wrapper.findAll('button').find(button => button.text() === '编辑')!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('openAccount')).toEqual([[account.id]])
    expect(wrapper.text()).toContain('编辑 OpenAI 账号')
    expect(wrapper.text()).toContain('所有者 管理员')
    await wrapper.findAll('button').find(button => button.text().includes('保存配置'))!.trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(account.id, expect.objectContaining({ name: account.name, status: account.status }))
  })

  it('starts administrator reauthorization from the account list', async () => {
    vi.spyOn(api, 'adminOverview').mockResolvedValue(overview)
    vi.spyOn(api, 'adminUsers').mockResolvedValue([])
    vi.spyOn(api, 'adminAccounts').mockResolvedValue([account])
    vi.spyOn(api, 'adminPlans').mockResolvedValue([])
    vi.spyOn(api, 'adminKeys').mockResolvedValue([])
    const start = vi.spyOn(adminAPI, 'adminOAuthReauthorizeStart').mockResolvedValue({ authorization_url: 'https://auth.example/authorize', flow_id: 'flow' })
    const wrapper = mount(AdminView, { props: { currentUser: admin, activeTab: 'accounts' }, global: { stubs: { teleport: true } } })
    await flushPromises()

    await wrapper.findAll('button').find(button => button.text().includes('重新授权'))!.trigger('click')
    await flushPromises()

    expect(start).toHaveBeenCalledWith(account.id)
    expect(wrapper.emitted('openAccount')).toEqual([[account.id]])
    expect(wrapper.text()).toContain('请登录同一个 OpenAI 账号')
  })

  it('lets an administrator manually refresh one account token', async () => {
    vi.spyOn(api, 'adminOverview').mockResolvedValue(overview)
    vi.spyOn(api, 'adminUsers').mockResolvedValue([])
    vi.spyOn(api, 'adminAccounts').mockResolvedValue([account])
    vi.spyOn(api, 'adminPlans').mockResolvedValue([])
    vi.spyOn(api, 'adminKeys').mockResolvedValue([])
    const refresh = vi.spyOn(adminAPI, 'adminRefreshAccountToken').mockResolvedValue({ ...account, token_expires_at: '2026-08-14T00:00:00Z' })
    const wrapper = mount(AdminView, { props: { currentUser: admin, activeTab: 'accounts' }, global: { stubs: { teleport: true } } })
    await flushPromises()

    await wrapper.findAll('button').find(button => button.text().includes('刷新令牌'))!.trigger('click')
    await flushPromises()

    expect(refresh).toHaveBeenCalledWith(account.id)
    expect(wrapper.emitted('message')).toContainEqual(['success', 'OpenAI 账号令牌已刷新'])
  })

  it('opens the shared Plan workspace instead of a management modal', async () => {
    vi.spyOn(api, 'adminOverview').mockResolvedValue(overview)
    vi.spyOn(api, 'adminUsers').mockResolvedValue([])
    vi.spyOn(api, 'adminAccounts').mockResolvedValue([account])
    vi.spyOn(api, 'adminPlans').mockResolvedValue([plan])
    vi.spyOn(api, 'adminKeys').mockResolvedValue([])
    const wrapper = mount(AdminView, { props: { currentUser: admin }, global: { stubs: { teleport: true } } })
    await flushPromises()
    await wrapper.findAll('.n-tabs-tab').find(tab => tab.text().includes('Plans'))!.trigger('click')
    await wrapper.findAll('button').find(button => button.text().includes('查看详情'))!.trigger('click')
    expect(wrapper.emitted('openPlan')).toEqual([[plan.id]])
    expect(wrapper.text()).not.toContain('管理 Plan')
  })

  it('restores the list tab, search query, and table scroll position', async () => {
    vi.spyOn(api, 'adminOverview').mockResolvedValue(overview)
    vi.spyOn(api, 'adminUsers').mockResolvedValue([])
    vi.spyOn(api, 'adminAccounts').mockResolvedValue([account])
    vi.spyOn(api, 'adminPlans').mockResolvedValue([])
    vi.spyOn(api, 'adminKeys').mockResolvedValue([])
    const wrapper = mount(AdminView, {
      props: { currentUser: admin, activeTab: 'accounts', query: '团队', scrollTop: 18 },
      global: { stubs: { teleport: true } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('团队账号')
    expect((wrapper.find('input').element as HTMLInputElement).value).toBe('团队')
    expect((wrapper.find('.admin-table-scroll').element as HTMLElement).scrollTop).toBe(18)
  })
})
