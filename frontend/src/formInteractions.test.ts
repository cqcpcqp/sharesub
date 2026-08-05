// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { effectScope, reactive } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { agreementVersions } from './agreements'
import type { APIKey, Account, Plan, PlanDetail, PlanPerformance, PublicPlan, User } from './types'
import APIKeySetupWizard from './components/APIKeySetupWizard.vue'
import AccountsView from './views/AccountsView.vue'
import AuthView from './views/AuthView.vue'
import KeysView from './views/KeysView.vue'
import LobbyView from './views/LobbyView.vue'
import PlansView from './views/PlansView.vue'
import ProfileView from './views/ProfileView.vue'
import { usePlansView } from './views/usePlansView'

const createdAt = '2026-08-03T00:00:00Z'
const owner: User = {
  id: 'owner',
  username: '房主',
  email: 'owner@example.com',
  avatar_url: '',
  status: 'active',
  created_at: createdAt,
  is_admin: false,
  role: 'user',
  must_change_password: false,
}
const member: User = {
  id: 'member-user',
  username: '成员',
  email: 'member@example.com',
  avatar_url: '',
  status: 'active',
  created_at: createdAt,
  is_admin: false,
  role: 'user',
  must_change_password: false,
}
const account: Account = {
  id: 'account',
  owner_user_id: owner.id,
  name: '共享账号',
  notes: '',
  email: 'openai@example.com',
  chatgpt_account_id: 'chatgpt-account',
  plan_type: 'plus',
  proxy_url: '',
  max_concurrency: 0,
    rpm_limit: 0,
    fast_policy: [],
  token_expires_at: createdAt,
  status: 'active',
  created_at: createdAt,
}
const archivedPlan: Plan = {
  id: 'plan',
  owner_user_id: owner.id,
  account_id: account.id,
  name: '本地功能测试',
  status: 'archived',
  visibility: 'private',
  public_slots: 0,
  public_share_basis_points: 0,
  allocation_mode: 'shared',
  created_at: createdAt,
  archived_at: createdAt,
}
const detail: PlanDetail = {
  plan: archivedPlan,
  account,
  members: [{
    id: 'member',
    plan_id: archivedPlan.id,
    user_id: owner.id,
    username: owner.username,
    avatar_url: '',
    email: owner.email,
    role: 'owner',
    status: 'active',
    share_basis_points: 0,
    created_at: createdAt,
  }],
  invites: [],
  applications: [],
  insights: {
    account_windows: [],
    member_quotas: [],
    performance: {
      request_count: 0,
      success_count: 0,
      average_ttft_ms: 0,
      p95_ttft_ms: 0,
      average_duration_ms: 0,
      p95_duration_ms: 0,
    },
    window_usage: [],
    member_ranking: [],
    member_rankings: [
      { period: 'today', window_start: '2026-08-03T00:00:00Z', window_end: '2026-08-03T12:00:00Z', members: [] },
      { period: 'last_7_days', window_start: '2026-07-27T12:00:00Z', window_end: '2026-08-03T12:00:00Z', members: [] },
      { period: 'account_lifecycle', window_start: '2026-08-01T00:00:00Z', window_end: '2026-08-03T12:00:00Z', members: [] },
    ],
    model_usage: [],
    token_trend: [],
    recent_usage: [],
  },
}
const activePlan: Plan = { ...archivedPlan, id: 'plan-active', name: '共享 Plan', status: 'active', archived_at: undefined }
const publicPlan: PublicPlan = {
  plan: { ...activePlan, owner_user_id: 'other-owner', visibility: 'public', public_slots: 2 },
  owner_username: '另一位房主',
  owner_avatar_url: '',
  plan_type: 'plus',
  member_count: 1,
  available_slots: 1,
  application_status: '',
}
const apiKey: APIKey = {
  id: 'key',
  user_id: owner.id,
  name: '我的 Codex',
  key: 'sk-sharesub-test',
  key_available: true,
  key_prefix: 'sk-sharesub-test',
  strategy: 'balanced',
  status: 'active',
  created_at: createdAt,
  routes: [{ plan_id: activePlan.id, plan_name: activePlan.name, priority: 100, enabled: true }],
}
const activeDetail: PlanDetail = {
  ...detail,
  plan: activePlan,
  members: detail.members.map(member => ({ ...member, plan_id: activePlan.id })),
}
const memberPlan: Plan = { ...activePlan, id: 'plan-member-auto', owner_user_id: owner.id }
const memberDetail: PlanDetail = {
  ...activeDetail,
  plan: memberPlan,
  members: [
    { ...activeDetail.members[0], plan_id: memberPlan.id },
    {
      id: 'member-record',
      plan_id: memberPlan.id,
      user_id: member.id,
      username: member.username,
      avatar_url: '',
      email: member.email,
      role: 'member',
      status: 'active',
      share_basis_points: 0,
      created_at: createdAt,
    },
  ],
}

function findButton(wrapper: ReturnType<typeof mount>, text: string) {
  return wrapper.findAll('button').find(button => button.text().trim() === text)
}

afterEach(() => {
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('form interactions', () => {
  it('accepts the archived Plan name and enables permanent deletion', async () => {
    vi.spyOn(api, 'plan').mockResolvedValue(detail)
    const wrapper = mount(PlansView, {
      attachTo: document.body,
      props: { accounts: [account], plans: [archivedPlan], user: owner, theme: 'light' },
      global: { stubs: { teleport: true } },
    })
    await flushPromises()

    const settings = wrapper.findAll('.n-tabs-tab').find(tab => tab.text().includes('设置'))
    expect(settings).toBeDefined()
    await settings!.trigger('click')

    const openDelete = wrapper.findAll('button').find(button => button.text().trim() === '永久删除')
    expect(openDelete).toBeDefined()
    await openDelete!.trigger('click')

    const continueButton = wrapper.findAll('button').find(button => button.text().trim() === '继续')
    expect(continueButton).toBeDefined()
    await continueButton!.trigger('click')

    const input = wrapper.get('input[placeholder="手动输入上方显示的 Plan 名称"]')
    const originalInput = input.element
    await input.setValue('本')
    expect(wrapper.get('input[placeholder="手动输入上方显示的 Plan 名称"]').element).toBe(originalInput)
    await input.setValue(archivedPlan.name)
    expect(input.element).toHaveProperty('value', archivedPlan.name)

    const deleteButton = wrapper.findAll('button').find(button => button.text().trim() === '永久删除')
    expect(deleteButton).toBeDefined()
    expect(deleteButton!.attributes('disabled')).toBeUndefined()
  })

  it('allows public seat input to be cleared while preventing an invalid save', async () => {
    vi.spyOn(api, 'plan').mockResolvedValue(activeDetail)
    const refreshQuota = vi.spyOn(api, 'refreshPlanQuota').mockResolvedValue({ account_id: account.id, signals: [] })
    const wrapper = mount(PlansView, {
      attachTo: document.body,
      props: { accounts: [account], plans: [activePlan], user: owner, theme: 'light' },
      global: { stubs: { teleport: true } },
    })
    await flushPromises()
    expect(refreshQuota).toHaveBeenCalledWith(activePlan.id, true)
    const settings = wrapper.findAll('.n-tabs-tab').find(tab => tab.text().includes('设置'))!
    await settings.trigger('click')
    await wrapper.get('.publication-control .n-switch').trigger('click')

    const slots = wrapper.get('.publication-control .n-input-number input')
    await slots.setValue('')
    await slots.trigger('blur')
    expect(slots.element).toHaveProperty('value', '')
    expect(findButton(wrapper, '保存设置')!.attributes('disabled')).toBeDefined()
    await slots.setValue('3')
    await slots.trigger('blur')
    expect(slots.element).toHaveProperty('value', '3')
    expect(findButton(wrapper, '保存设置')!.attributes('disabled')).toBeUndefined()
  })

  it('automatically refreshes quota when a regular member enters an active Plan', async () => {
    vi.spyOn(api, 'plan').mockResolvedValue(memberDetail)
    const refreshQuota = vi.spyOn(api, 'refreshPlanQuota').mockResolvedValue({ account_id: account.id, signals: [] })
    mount(PlansView, {
      attachTo: document.body,
      props: { accounts: [], plans: [memberPlan], user: member, theme: 'light' },
      global: { stubs: { teleport: true } },
    })

    await flushPromises()
    expect(refreshQuota).toHaveBeenCalledWith(memberPlan.id, true)
  })

  it('keeps the selected performance period data after automatic quota refresh', async () => {
    const plan = { ...activePlan, id: 'plan-performance-refresh' }
    const planDetail = { ...activeDetail, plan }
    const selectedPerformance: PlanPerformance = {
      request_count: 6,
      success_count: 5,
      average_ttft_ms: 10,
      p95_ttft_ms: 20,
      average_duration_ms: 30,
      p95_duration_ms: 40,
      model_usage: [{
        model: 'gpt-5.6-sol',
        request_count: 6,
        token_usage: { input_tokens: 600, output_tokens: 60, cached_tokens: 30, cache_creation_tokens: 0, image_input_tokens: 0, image_output_tokens: 0, image_count: 0, total_tokens: 660 },
        web_search_calls: 0,
        estimated_cost_micros: 120,
      }],
      token_trend: [{ bucket_start: createdAt, input_tokens: 600, output_tokens: 60, cached_tokens: 30, cache_creation_tokens: 0, image_input_tokens: 0, image_output_tokens: 0, image_count: 0, web_search_calls: 0 }],
      recent_usage: [{ member_id: 'member', username: owner.username, trend: [{ bucket_start: createdAt, input_tokens: 600, output_tokens: 60, cached_tokens: 30, cache_creation_tokens: 0, image_input_tokens: 0, image_output_tokens: 0, image_count: 0, web_search_calls: 0 }] }],
    }
    let finishRefresh!: () => void
    const refreshPending = new Promise<void>(resolve => { finishRefresh = resolve })
    vi.spyOn(api, 'plan').mockImplementation(async () => structuredClone(planDetail))
    vi.spyOn(api, 'planPerformance').mockResolvedValue(selectedPerformance)
    vi.spyOn(api, 'refreshPlanQuota').mockImplementation(async () => {
      await refreshPending
      return { account_id: account.id, signals: [] }
    })

    const scope = effectScope()
    const view = scope.run(() => usePlansView(reactive({ accounts: [account], plans: [plan], user: owner, initialPlanId: '', invitePlanId: '' }), vi.fn()))!
    await flushPromises()
    await view.loadPerformance('6h')
    expect(view.detail.value?.insights.model_usage).toEqual(selectedPerformance.model_usage)

    finishRefresh()
    await flushPromises()
    expect(view.detail.value?.insights.performance).toEqual(selectedPerformance)
    expect(view.detail.value?.insights.model_usage).toEqual(selectedPerformance.model_usage)
    expect(view.detail.value?.insights.token_trend).toEqual(selectedPerformance.token_trend)
    expect(view.detail.value?.insights.recent_usage).toEqual(selectedPerformance.recent_usage)
    scope.stop()
  })

  it('edits and clears login credentials', async () => {
    const wrapper = mount(AuthView, {
      attachTo: document.body,
      props: { invitePending: false, invitation: null, inviteLoading: false, inviteError: '' },
    })
    const email = wrapper.get('input[placeholder="name@example.com"]')
    const password = wrapper.get('input[placeholder="至少 10 个字符"]')
    await email.setValue('person@example.com')
    await password.setValue('secret-value')
    expect(email.element).toHaveProperty('value', 'person@example.com')
    expect(password.element).toHaveProperty('value', 'secret-value')
    await email.setValue('')
    await password.setValue('')
    expect(email.element).toHaveProperty('value', '')
    expect(password.element).toHaveProperty('value', '')
  })

  it('requires every current agreement before registration', async () => {
    const register = vi.spyOn(api, 'register').mockResolvedValue({ user: member, token: 'ss_session_test' })
    const wrapper = mount(AuthView, {
      attachTo: document.body,
      props: { invitePending: false, invitation: null, inviteLoading: false, inviteError: '', initialMode: 'register' },
    })
    await wrapper.get('input[placeholder="你的公开昵称"]').setValue('成员')
    await wrapper.get('input[placeholder="name@example.com"]').setValue('member@example.com')
    await wrapper.get('input[placeholder="至少 10 个字符"]').setValue('strong-password')
    const submit = wrapper.findAll('button').find(button => button.text().includes('创建账号'))!
    expect(submit.attributes('disabled')).toBeDefined()

    await wrapper.get('.agreement-check').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(register).toHaveBeenCalledWith('成员', 'member@example.com', 'strong-password', {
      accepted: true,
      terms_version: agreementVersions.terms,
      privacy_policy_version: agreementVersions.privacy,
      acceptable_use_version: agreementVersions.acceptableUse,
    })
  })

  it('edits and clears lobby search and application message', async () => {
    const wrapper = mount(LobbyView, {
      attachTo: document.body,
      props: { plans: [publicPlan], user: owner },
      global: { stubs: { teleport: true } },
    })
    const search = wrapper.get('input[placeholder="搜索 Plan 或房主"]')
    await search.setValue('共享')
    expect(search.element).toHaveProperty('value', '共享')
    await search.setValue('')
    await findButton(wrapper, '申请加入')!.trigger('click')
    const message = wrapper.get('textarea[placeholder="向房主简单介绍你的使用需求（可选）"]')
    await message.setValue('一起使用')
    expect(message.element).toHaveProperty('value', '一起使用')
    await message.setValue('')
    expect(message.element).toHaveProperty('value', '')
  })

  it('edits and clears OAuth callback inputs', async () => {
    vi.spyOn(api, 'oauthStart').mockResolvedValue({ authorization_url: 'https://example.com/oauth', flow_id: 'flow' })
    const wrapper = mount(AccountsView, {
      attachTo: document.body,
      props: { accounts: [] },
      global: { stubs: { teleport: true } },
    })
    await findButton(wrapper, '接入账号')!.trigger('click')
    await flushPromises()
    const callback = wrapper.get('input[placeholder="http://localhost:1455/auth/callback?..."]')
    await callback.setValue('http://localhost:1455/auth/callback?code=test&state=flow')
    expect(callback.element).toHaveProperty('value', 'http://localhost:1455/auth/callback?code=test&state=flow')
    await callback.setValue('')
    expect(callback.element).toHaveProperty('value', '')
  })

  it('edits and clears the profile username', async () => {
    const wrapper = mount(ProfileView, {
      attachTo: document.body,
      props: { user: owner, themeMode: 'system' },
    })
    const username = wrapper.findAll('.n-input__input-el').find(input => !input.attributes('disabled'))!
    await username.setValue('新用户名')
    expect(username.element).toHaveProperty('value', '新用户名')
    await username.setValue('')
    expect(username.element).toHaveProperty('value', '')
  })

  it('updates API key creation and edit drafts, including empty values', async () => {
    const wizard = mount(APIKeySetupWizard, {
      attachTo: document.body,
      props: { show: true, plans: [activePlan] },
      global: { stubs: { teleport: true } },
    })
    const createName = wizard.get('input[placeholder="例如：我的 Codex"]')
    await createName.setValue('')
    expect(createName.element).toHaveProperty('value', '')
    expect(findButton(wizard, '创建密钥')!.attributes('disabled')).toBeDefined()
    await createName.setValue('新的 Codex')
    expect(findButton(wizard, '创建密钥')!.attributes('disabled')).toBeUndefined()
    const createPriority = wizard.get('.priority-input .n-input-number input')
    await createPriority.setValue('')
    await createPriority.trigger('blur')
    expect(createPriority.element).toHaveProperty('value', '')
    expect(findButton(wizard, '创建密钥')!.attributes('disabled')).toBeDefined()
    await createPriority.setValue('20')
    expect(findButton(wizard, '创建密钥')!.attributes('disabled')).toBeUndefined()
    const createRoute = wizard.get('.route-option [role="checkbox"]')
    await createRoute.trigger('click')
    expect(findButton(wizard, '创建密钥')!.attributes('disabled')).toBeDefined()
    await createRoute.trigger('click')
    expect(findButton(wizard, '创建密钥')!.attributes('disabled')).toBeUndefined()
    wizard.unmount()

    const keys = mount(KeysView, {
      attachTo: document.body,
      props: { keys: [apiKey], plans: [activePlan] },
      global: { stubs: { teleport: true } },
    })
    const editButton = keys.get('button[aria-label="编辑配置"]')
    await editButton.trigger('click')
    const editName = keys.get('input[placeholder="例如：个人 Codex"]')
    await editName.setValue('')
    expect(editName.element).toHaveProperty('value', '')
    expect(findButton(keys, '保存配置')!.attributes('disabled')).toBeDefined()
    await editName.setValue('更新后的名称')
    expect(findButton(keys, '保存配置')!.attributes('disabled')).toBeUndefined()
  })

  it('shows the same persisted API key every time the usage dialog is opened', async () => {
    const wrapper = mount(KeysView, {
      attachTo: document.body,
      props: { keys: [apiKey], plans: [activePlan] },
      global: { stubs: { teleport: true } },
    })

    await findButton(wrapper, '使用密钥')!.trigger('click')
    expect(wrapper.get('.credential-block .secret-value').text()).toBe(apiKey.key)
    expect(findButton(wrapper, '导入到 CCS')).toBeDefined()
    await findButton(wrapper, '关闭')!.trigger('click')

    await findButton(wrapper, '使用密钥')!.trigger('click')
    expect(wrapper.get('.credential-block .secret-value').text()).toBe(apiKey.key)
  })
})
