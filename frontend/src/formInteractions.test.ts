// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { effectScope, reactive } from 'vue'
import { NSelect } from 'naive-ui'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { APIRequestError, api } from './api'
import { adminAPI } from './api/admin'
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
  email_verified_at: createdAt,
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
  email_verified_at: createdAt,
  avatar_url: '',
  status: 'active',
  created_at: createdAt,
  is_admin: false,
  role: 'user',
  must_change_password: false,
}
const administrator: User = {
  id: 'admin', username: '管理员', email: 'admin@example.com', email_verified_at: createdAt, avatar_url: '', status: 'active',
  created_at: createdAt, is_admin: true, role: 'admin', must_change_password: false,
}
const account: Account = {
  id: 'account',
  owner_user_id: owner.id,
  name: '共享账号',
  notes: '',
  email: 'openai@example.com',
  chatgpt_account_id: 'chatgpt-account',
  plan_type: 'plus',
  subscription_expires_at: '2026-09-03T00:00:00Z',
  proxy_url: '',
  max_concurrency: 0,
    rpm_limit: 0,
    fast_policy: [],
  codex_fingerprint_mode: 'session',
  token_expires_at: createdAt,
  status: 'active',
  created_at: createdAt,
}
const archivedPlan: Plan = {
  id: 'plan',
  owner_user_id: owner.id,
  account_id: account.id,
  name: '本地功能测试',
  description: '团队项目的 Codex 协作空间',
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
    member_quotas: [{ member_id: 'member', windows: [] }],
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
  subscription_expires_at: '2026-09-03T00:00:00Z',
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
  fast_policy: [],
  status: 'active',
  created_at: createdAt,
  routes: [{ plan_id: activePlan.id, plan_name: activePlan.name, priority: 100, enabled: true }],
}
const activeDetail: PlanDetail = {
  ...detail,
  plan: activePlan,
  members: detail.members.map(member => ({ ...member, plan_id: activePlan.id })),
}
const unboundPlan: Plan = { ...activePlan, id: 'plan-unbound', name: '待配置 Plan', account_id: '' }
const unboundDetail: PlanDetail = {
  ...activeDetail,
  plan: unboundPlan,
  account: null,
  members: activeDetail.members.map(member => ({ ...member, plan_id: unboundPlan.id })),
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
  insights: {
    ...activeDetail.insights,
    member_quotas: [...activeDetail.insights.member_quotas, { member_id: 'member-record', windows: [] }],
  },
}

function findButton(wrapper: ReturnType<typeof mount>, text: string) {
  return wrapper.findAll('button').find(button => button.text().trim() === text)
}

afterEach(() => {
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('form interactions', () => {
  it('creates a Plan without requiring an OpenAI account', async () => {
    const createPlan = vi.spyOn(api, 'createPlan').mockResolvedValue(unboundDetail)
    const wrapper = mount(PlansView, {
      attachTo: document.body,
      props: { accounts: [], plans: [], user: owner, theme: 'light' },
      global: { stubs: { teleport: true } },
    })

    const openCreate = findButton(wrapper, '创建 Plan')
    expect(openCreate?.attributes('disabled')).toBeUndefined()
    await openCreate!.trigger('click')
    await wrapper.get('input[placeholder="给这个共享空间起个名字"]').setValue(unboundPlan.name)
    const submit = findButton(wrapper, '创建')
    expect(submit?.attributes('disabled')).toBeUndefined()
    await submit!.trigger('click')
    await flushPromises()

    expect(createPlan).toHaveBeenCalledWith({
      account_id: '',
      name: unboundPlan.name,
      allocation_mode: 'fixed',
      owner_share_basis_points: 2000,
    })
    expect(wrapper.text()).toContain('绑定 OpenAI 账号')
  })

  it('does not refresh quota before the Plan has an account', async () => {
    vi.spyOn(api, 'plan').mockResolvedValue(unboundDetail)
    const refreshQuota = vi.spyOn(api, 'refreshPlanQuota')
    const wrapper = mount(PlansView, {
      attachTo: document.body,
      props: { accounts: [], plans: [unboundPlan], user: owner, theme: 'light' },
      global: { stubs: { teleport: true } },
    })

    await flushPromises()
    expect(refreshQuota).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('待绑定账号')
    await findButton(wrapper, '去绑定账号')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('接入新账号并绑定')
  })

  it('binds an available account from the created Plan', async () => {
    const boundPlan = { ...unboundPlan, account_id: account.id }
    const boundDetail: PlanDetail = { ...unboundDetail, plan: boundPlan, account }
    vi.spyOn(api, 'plan').mockResolvedValueOnce(unboundDetail).mockResolvedValue(boundDetail)
    const rebind = vi.spyOn(api, 'rebindPlanAccount').mockResolvedValue(boundPlan)
    vi.spyOn(api, 'refreshPlanQuota').mockResolvedValue({ account_id: account.id, signals: [] })
    const scope = effectScope()
    const view = scope.run(() => usePlansView(reactive({ accounts: [account], plans: [unboundPlan], user: owner, initialPlanId: '', invitePlanId: '' }), vi.fn()))!
    await flushPromises()

    view.updateRebindAccount(account.id)
    await view.rebindAccount()

    expect(rebind).toHaveBeenCalledWith(unboundPlan.id, account.id)
    expect(view.detail.value?.account).toEqual(account)
    scope.stop()
  })

  it('uses administrator Plan APIs while preserving the real owner account scope', async () => {
    const boundPlan = { ...unboundPlan, account_id: account.id }
    const boundDetail: PlanDetail = { ...unboundDetail, plan: boundPlan, account }
    vi.spyOn(adminAPI, 'adminPlan').mockResolvedValueOnce(unboundDetail).mockResolvedValue(boundDetail)
    const rebind = vi.spyOn(adminAPI, 'adminRebindPlanAccount').mockResolvedValue(boundPlan)
    const wrapper = mount(PlansView, {
      attachTo: document.body,
      props: { accounts: [account], plans: [unboundPlan], user: administrator, theme: 'light', adminMode: true },
      global: { stubs: { teleport: true } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('由 房主 提供')
    expect(findButton(wrapper, '创建 Plan')).toBeUndefined()
    await findButton(wrapper, '去绑定账号')!.trigger('click')
    const select = wrapper.findAllComponents(NSelect).find(component => component.props('placeholder') === '选择尚未绑定的账号')!
    select.vm.$emit('update:value', account.id)
    await flushPromises()
    await findButton(wrapper, '绑定账号')!.trigger('click')
    await flushPromises()
    expect(rebind).toHaveBeenCalledWith(unboundPlan.id, account.id)
  })

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

  it('updates the Plan description from settings', async () => {
    const updatedPlan = { ...archivedPlan, description: '仅用于代码审查与测试' }
    vi.spyOn(api, 'plan').mockResolvedValueOnce(detail).mockResolvedValue({ ...detail, plan: updatedPlan })
    const updateDescription = vi.spyOn(api, 'updatePlanDescription').mockResolvedValue(updatedPlan)
    const wrapper = mount(PlansView, {
      attachTo: document.body,
      props: { accounts: [account], plans: [archivedPlan], user: owner, theme: 'light' },
      global: { stubs: { teleport: true } },
    })
    await flushPromises()

    const settings = wrapper.findAll('.n-tabs-tab').find(tab => tab.text().includes('设置'))!
    await settings.trigger('click')
    const description = wrapper.get('textarea[aria-label="Plan 描述"]')
    await description.setValue('  仅用于代码审查与测试  ')
    const save = findButton(wrapper, '保存描述')!
    expect(save.attributes('disabled')).toBeUndefined()
    await save.trigger('click')
    await flushPromises()

    expect(updateDescription).toHaveBeenCalledWith(archivedPlan.id, '仅用于代码审查与测试')
    expect(wrapper.text()).toContain(updatedPlan.description)
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

  it('validates proposed public seat shares before saving', async () => {
    const fixedPlan: Plan = {
      ...activePlan,
      id: 'plan-fixed-publication',
      allocation_mode: 'fixed',
      visibility: 'private',
    }
    const fixedDetail: PlanDetail = {
      ...activeDetail,
      plan: fixedPlan,
      members: [{ ...activeDetail.members[0], plan_id: fixedPlan.id, share_basis_points: 100 }],
    }
    vi.spyOn(api, 'plan').mockResolvedValue(fixedDetail)
    vi.spyOn(api, 'refreshPlanQuota').mockResolvedValue({ account_id: account.id, signals: [] })
    const updatePublication = vi.spyOn(api, 'updatePublication').mockResolvedValue({
      ...fixedPlan,
      visibility: 'public',
      public_slots: 3,
      public_share_basis_points: 2500,
    })
    const scope = effectScope()
    const view = scope.run(() => usePlansView(reactive({ accounts: [account], plans: [fixedPlan], user: owner, initialPlanId: '', invitePlanId: '' }), vi.fn()))!

    await view.loadPlan(fixedPlan.id)
    view.setPublicationVisibility(true)
    view.updatePublicationSlots(3)
    view.updatePublicationShare(25)

    expect(view.publicationReservedShares.value.total).toBe(7600)
    expect(view.maxPublicSeatSharePercent.value).toBe(33)
    expect(view.canSavePublication.value).toBe(true)
    await view.savePublication()
    expect(updatePublication).toHaveBeenCalledWith(fixedPlan.id, {
      visibility: 'public',
      public_slots: 3,
      public_share_basis_points: 2500,
    })

    scope.stop()
  })

  it('blocks a public seat allocation that exceeds the remaining Plan share', async () => {
    const fixedPlan: Plan = {
      ...activePlan,
      id: 'plan-fixed-over-allocation',
      allocation_mode: 'fixed',
      visibility: 'private',
    }
    const fixedDetail: PlanDetail = {
      ...activeDetail,
      plan: fixedPlan,
      members: [{ ...activeDetail.members[0], plan_id: fixedPlan.id, share_basis_points: 100 }],
      invites: [{
        id: 'pending-invite',
        plan_id: fixedPlan.id,
        share_basis_points: 2500,
        status: 'pending',
        expires_at: '2099-08-08T00:00:00Z',
        created_at: createdAt,
      }],
    }
    vi.spyOn(api, 'plan').mockResolvedValue(fixedDetail)
    vi.spyOn(api, 'refreshPlanQuota').mockResolvedValue({ account_id: account.id, signals: [] })
    const updatePublication = vi.spyOn(api, 'updatePublication')
    const scope = effectScope()
    const view = scope.run(() => usePlansView(reactive({ accounts: [account], plans: [fixedPlan], user: owner, initialPlanId: '', invitePlanId: '' }), vi.fn()))!

    await view.loadPlan(fixedPlan.id)
    view.setPublicationVisibility(true)
    view.updatePublicationSlots(3)
    view.updatePublicationShare(25)

    expect(view.publicationReservedShares.value.total).toBe(10100)
    expect(view.maxPublicSeatSharePercent.value).toBe(24)
    expect(view.publicationCapacityExceeded.value).toBe(true)
    expect(view.canSavePublication.value).toBe(false)
    await view.savePublication()
    expect(updatePublication).not.toHaveBeenCalled()

    scope.stop()
  })

  it('explains a concurrent share allocation conflict in Chinese', async () => {
    const fixedPlan: Plan = {
      ...activePlan,
      id: 'plan-fixed-conflict',
      allocation_mode: 'fixed',
      visibility: 'private',
    }
    const fixedDetail: PlanDetail = {
      ...activeDetail,
      plan: fixedPlan,
      members: [{ ...activeDetail.members[0], plan_id: fixedPlan.id, share_basis_points: 100 }],
    }
    vi.spyOn(api, 'plan').mockResolvedValue(fixedDetail)
    vi.spyOn(api, 'refreshPlanQuota').mockResolvedValue({ account_id: account.id, signals: [] })
    vi.spyOn(api, 'updatePublication').mockRejectedValue(new APIRequestError(409, 'share_exceeded', 'allocated shares exceed 100 percent'))
    const emit = vi.fn()
    const scope = effectScope()
    const view = scope.run(() => usePlansView(reactive({ accounts: [account], plans: [fixedPlan], user: owner, initialPlanId: '', invitePlanId: '' }), emit))!

    await view.loadPlan(fixedPlan.id)
    view.setPublicationVisibility(true)
    view.updatePublicationSlots(3)
    view.updatePublicationShare(25)
    await view.savePublication()

    expect(emit).toHaveBeenCalledWith('message', 'error', '分配份额已超过 100%，请刷新 Plan 后减少成员、邀请或公开招募预留额度')
    scope.stop()
  })

  it('publishes an unbound Plan for member recruitment', async () => {
    vi.spyOn(api, 'plan').mockResolvedValue(unboundDetail)
    const updatePublication = vi.spyOn(api, 'updatePublication').mockResolvedValue({
      ...unboundPlan,
      visibility: 'public',
      public_slots: 1,
      public_share_basis_points: 0,
    })
    const wrapper = mount(PlansView, {
      attachTo: document.body,
      props: { accounts: [], plans: [unboundPlan], user: owner, theme: 'light' },
      global: { stubs: { teleport: true } },
    })
    await flushPromises()

    const settings = wrapper.findAll('.n-tabs-tab').find(tab => tab.text().includes('设置'))!
    await settings.trigger('click')
    const publicationSwitch = wrapper.get('.publication-control .n-switch')
    expect(publicationSwitch.attributes('aria-disabled')).not.toBe('true')
    await publicationSwitch.trigger('click')
    expect(wrapper.text()).toContain('当前以筹备中状态在大厅展示')
    const save = findButton(wrapper, '保存设置')!
    expect(save.attributes('disabled')).toBeUndefined()
    await save.trigger('click')
    await flushPromises()

    expect(updatePublication).toHaveBeenCalledWith(unboundPlan.id, {
      visibility: 'public',
      public_slots: 1,
      public_share_basis_points: 0,
    })
  })

  it('automatically refreshes quota when a regular member enters an active Plan', async () => {
    vi.spyOn(api, 'plan').mockResolvedValue(memberDetail)
    const refreshQuota = vi.spyOn(api, 'refreshPlanQuota').mockResolvedValue({ account_id: account.id, signals: [] })
    const queryCredits = vi.spyOn(api, 'planQuotaResetCredits').mockResolvedValue({
      available_count: 1,
      credits: [{ expires_at: '2026-08-12T05:09:00Z' }],
      fetched_at: '2026-08-06T10:00:00Z',
    })
    const wrapper = mount(PlansView, {
      attachTo: document.body,
      props: { accounts: [], plans: [memberPlan], user: member, theme: 'light' },
      global: { stubs: { teleport: true } },
    })

    await flushPromises()
    expect(refreshQuota).toHaveBeenCalledWith(memberPlan.id, true)
    await wrapper.get('button[aria-label="查询重置次数"]').trigger('click')
    await flushPromises()
    expect(queryCredits).toHaveBeenCalledWith(memberPlan.id)
    expect(wrapper.text()).toContain('次数 1')
    expect(wrapper.get('button[aria-label="重置 OpenAI 额度"]').attributes('disabled')).toBeDefined()
  })

  it('queries and consumes Plan quota reset credits, then reloads quota and remaining credits', async () => {
    const plan = { ...activePlan, id: 'plan-quota-reset' }
    const planDetail = { ...activeDetail, plan }
    vi.spyOn(api, 'plan').mockImplementation(async () => structuredClone(planDetail))
    vi.spyOn(api, 'refreshPlanQuota').mockResolvedValue({ account_id: account.id, signals: [] })
    const queryCredits = vi.spyOn(api, 'planQuotaResetCredits')
      .mockResolvedValueOnce({
        available_count: 2,
        credits: [{ expires_at: '2026-08-12T05:09:00Z' }, { expires_at: '2026-08-13T02:13:00Z' }],
        fetched_at: '2026-08-06T10:00:00Z',
      })
      .mockResolvedValueOnce({
        available_count: 1,
        credits: [{ expires_at: '2026-08-13T02:13:00Z' }],
        fetched_at: '2026-08-06T10:01:00Z',
      })
    const reset = vi.spyOn(api, 'resetPlanQuota').mockResolvedValue({
      code: 'rate_limit_reset_credit_redeemed',
      credit: {
        id: 'credit-1',
        reset_type: 'codex_rate_limits',
        status: 'redeemed',
        granted_at: '2026-08-01T00:00:00Z',
        expires_at: '2026-08-12T05:09:00Z',
        redeem_started_at: '2026-08-06T10:00:00Z',
        redeemed_at: '2026-08-06T10:00:01Z',
      },
      windows_reset: 2,
      quota_refreshed: true,
      signals: [],
    })
    const emit = vi.fn()
    const scope = effectScope()
    const view = scope.run(() => usePlansView(reactive({ accounts: [account], plans: [plan], user: owner, initialPlanId: '', invitePlanId: '' }), emit))!
    await flushPromises()

    await view.queryQuotaResetCredits()
    expect(queryCredits).toHaveBeenNthCalledWith(1, plan.id)
    expect(view.quotaResetCredits.value?.available_count).toBe(2)

    await view.resetQuota()
    expect(reset).toHaveBeenCalledOnce()
    expect(reset).toHaveBeenCalledWith(plan.id)
    expect(queryCredits).toHaveBeenNthCalledWith(2, plan.id)
    expect(view.quotaResetCredits.value?.available_count).toBe(1)
    expect(emit).toHaveBeenCalledWith('message', 'success', '已使用 1 次重置机会，OpenAI 已重置 2 个额度窗口')
    scope.stop()
  })

  it('reports a successful reset separately when the post-reset quota sync fails', async () => {
    const plan = { ...activePlan, id: 'plan-quota-reset-sync-failure' }
    const planDetail = { ...activeDetail, plan }
    vi.spyOn(api, 'plan').mockImplementation(async () => structuredClone(planDetail))
    vi.spyOn(api, 'refreshPlanQuota').mockResolvedValue({ account_id: account.id, signals: [] })
    vi.spyOn(api, 'planQuotaResetCredits')
      .mockResolvedValueOnce({ available_count: 1, credits: [{ expires_at: '2026-08-12T05:09:00Z' }], fetched_at: '2026-08-06T10:00:00Z' })
      .mockResolvedValueOnce({ available_count: 0, credits: [], fetched_at: '2026-08-06T10:01:00Z' })
    const reset = vi.spyOn(api, 'resetPlanQuota').mockResolvedValue({
      code: 'rate_limit_reset_credit_redeemed',
      credit: null,
      windows_reset: 2,
      quota_refreshed: false,
      signals: [],
    })
    const emit = vi.fn()
    const scope = effectScope()
    const view = scope.run(() => usePlansView(reactive({ accounts: [account], plans: [plan], user: owner, initialPlanId: '', invitePlanId: '' }), emit))!
    await flushPromises()

    await view.queryQuotaResetCredits()
    await view.resetQuota()

    expect(reset).toHaveBeenCalledOnce()
    expect(view.quotaResetCredits.value?.available_count).toBe(0)
    expect(emit).toHaveBeenCalledWith('message', 'success', '已使用 1 次重置机会，OpenAI 已重置 2 个额度窗口')
    expect(emit).toHaveBeenCalledWith('message', 'error', '重置已成功，但最新额度暂未同步；可稍后使用“查询额度”更新显示，请勿重复重置。')
    scope.stop()
  })

  it('requires re-querying credits when a reset response cannot be confirmed', async () => {
    const plan = { ...activePlan, id: 'plan-quota-reset-unknown-result' }
    const planDetail = { ...activeDetail, plan }
    vi.spyOn(api, 'plan').mockImplementation(async () => structuredClone(planDetail))
    vi.spyOn(api, 'refreshPlanQuota').mockResolvedValue({ account_id: account.id, signals: [] })
    vi.spyOn(api, 'planQuotaResetCredits').mockResolvedValue({
      available_count: 1,
      credits: [{ expires_at: '2026-08-12T05:09:00Z' }],
      fetched_at: '2026-08-06T10:00:00Z',
    })
    vi.spyOn(api, 'resetPlanQuota').mockRejectedValue(new Error('upstream connection closed'))
    const emit = vi.fn()
    const scope = effectScope()
    const view = scope.run(() => usePlansView(reactive({ accounts: [account], plans: [plan], user: owner, initialPlanId: '', invitePlanId: '' }), emit))!
    await flushPromises()

    await view.queryQuotaResetCredits()
    await view.resetQuota()

    expect(view.quotaResetCredits.value).toBeNull()
    expect(emit).toHaveBeenCalledWith('message', 'error', '重置请求未能确认结果，请先重新查询剩余次数：upstream connection closed')
    scope.stop()
  })

  it('keeps quota reset locks isolated while switching between Plans', async () => {
    const accountA = { ...account, id: 'account-reset-a' }
    const accountB = { ...account, id: 'account-reset-b' }
    const planA = { ...activePlan, id: 'plan-reset-a', account_id: accountA.id }
    const planB = { ...activePlan, id: 'plan-reset-b', account_id: accountB.id }
    const details: Record<string, PlanDetail> = {
      [planA.id]: { ...activeDetail, plan: planA, account: accountA },
      [planB.id]: { ...activeDetail, plan: planB, account: accountB },
    }
    vi.spyOn(api, 'plan').mockImplementation(async id => structuredClone(details[id]))
    vi.spyOn(api, 'refreshPlanQuota').mockResolvedValue({ account_id: account.id, signals: [] })
    vi.spyOn(api, 'planQuotaResetCredits').mockResolvedValue({
      available_count: 1,
      credits: [{ expires_at: '2026-08-12T05:09:00Z' }],
      fetched_at: '2026-08-06T10:00:00Z',
    })
    let finishResetA!: (result: Awaited<ReturnType<typeof api.resetPlanQuota>>) => void
    let finishResetB!: (result: Awaited<ReturnType<typeof api.resetPlanQuota>>) => void
    const resetA = new Promise<Awaited<ReturnType<typeof api.resetPlanQuota>>>(resolve => { finishResetA = resolve })
    const resetB = new Promise<Awaited<ReturnType<typeof api.resetPlanQuota>>>(resolve => { finishResetB = resolve })
    const reset = vi.spyOn(api, 'resetPlanQuota').mockImplementation(id => id === planA.id ? resetA : resetB)
    const emit = vi.fn()
    const scope = effectScope()
    const view = scope.run(() => usePlansView(reactive({ accounts: [accountA, accountB], plans: [planA, planB], user: owner, initialPlanId: '', invitePlanId: '' }), emit))!
    await flushPromises()

    await view.queryQuotaResetCredits()
    const pendingA = view.resetQuota()
    await view.loadPlan(planB.id)
    await view.queryQuotaResetCredits()
    const pendingB = view.resetQuota()
    expect(reset).toHaveBeenCalledTimes(2)
    expect(view.quotaResetting.value).toBe(true)

    finishResetA({ code: 'ok', credit: null, windows_reset: 2, quota_refreshed: false, signals: [] })
    await pendingA
    expect(view.quotaResetting.value).toBe(true)
    await view.resetQuota()
    expect(reset).toHaveBeenCalledTimes(2)

    finishResetB({ code: 'ok', credit: null, windows_reset: 2, quota_refreshed: false, signals: [] })
    await pendingB
    expect(view.quotaResetting.value).toBe(false)
    scope.stop()
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
    const register = vi.spyOn(api, 'register').mockResolvedValue({ email: member.email, verification_expires_at: '2026-08-03T01:00:00Z', resend_available_at: '2026-08-03T00:01:00Z' })
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
    expect(wrapper.get('.verification-pending').text()).toContain(member.email)
  })

  it('resends a verification email and explains delivery failures', async () => {
    const register = vi.spyOn(api, 'register').mockResolvedValue({
      email: member.email,
      verification_expires_at: new Date(Date.now() + 60 * 60_000).toISOString(),
      resend_available_at: new Date(Date.now() - 1_000).toISOString(),
    })
    const resend = vi.spyOn(api, 'resendEmailVerification')
      .mockRejectedValueOnce(new APIRequestError(502, 'email_delivery_unavailable', 'unavailable'))
      .mockResolvedValueOnce({ accepted: true, resend_available_at: new Date(Date.now() + 60_000).toISOString() })
    const wrapper = mount(AuthView, {
      attachTo: document.body,
      props: { invitePending: false, invitation: null, inviteLoading: false, inviteError: '', initialMode: 'register' },
    })
    await wrapper.get('input[placeholder="你的公开昵称"]').setValue('成员')
    await wrapper.get('input[placeholder="name@example.com"]').setValue(member.email)
    await wrapper.get('input[placeholder="至少 10 个字符"]').setValue('strong-password')
    await wrapper.get('.agreement-check').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(register).toHaveBeenCalledOnce()

    await findButton(wrapper, '重新发送验证邮件')!.trigger('click')
    await flushPromises()
    expect(resend).toHaveBeenLastCalledWith(member.email)
    expect(wrapper.text()).toContain('邮件服务暂时不可用')

    await findButton(wrapper, '重新发送验证邮件')!.trigger('click')
    await flushPromises()
    expect(resend).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('秒后可重新发送')
    wrapper.unmount()
  })

  it('edits and clears lobby search and application message', async () => {
    const wrapper = mount(LobbyView, {
      attachTo: document.body,
      props: { plans: [publicPlan], user: owner },
      global: { stubs: { teleport: true } },
    })
    const card = wrapper.get('.plan-card')
    expect(card.find('.plan-icon').exists()).toBe(false)
    expect(card.find('.status-badge').exists()).toBe(false)
    expect(card.get('.plan-subscription').text()).toContain('订阅有效期至')
    expect(card.get('.plan-subscription').text()).toContain('2026')
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

  it('opens a public Plan detail from the lobby and applies from the detail view', async () => {
    const wrapper = mount(LobbyView, {
      attachTo: document.body,
      props: { plans: [publicPlan], user: owner },
      global: { stubs: { teleport: true } },
    })

    await findButton(wrapper, '查看详情')!.trigger('click')

    const detailView = wrapper.get('.public-plan-detail-layout')
    expect(wrapper.find('.lobby-grid').exists()).toBe(false)
    expect(detailView.text()).toContain(publicPlan.plan.name)
    expect(detailView.text()).toContain(publicPlan.plan.description)
    expect(detailView.text()).toContain(publicPlan.owner_username)
    expect(detailView.text()).toContain('plus 账号')
    expect(detailView.text()).toContain('1 / 2 可申请')
    expect(detailView.text()).toContain('共享额度')

    await findButton(wrapper, '申请加入')!.trigger('click')
    wrapper.get('textarea[placeholder="向房主简单介绍你的使用需求（可选）"]')
    await findButton(wrapper, '取消')!.trigger('click')
    await findButton(wrapper, '返回探索大厅')!.trigger('click')
    expect(wrapper.find('.public-plan-detail-layout').exists()).toBe(false)
    wrapper.get('.lobby-grid')
  })

  it('labels an unbound public Plan as preparing before application', async () => {
    const preparingPlan: PublicPlan = {
      ...publicPlan,
      plan: { ...publicPlan.plan, account_id: '' },
      plan_type: '',
      subscription_expires_at: null,
    }
    const wrapper = mount(LobbyView, {
      attachTo: document.body,
      props: { plans: [preparingPlan], user: owner },
      global: { stubs: { teleport: true } },
    })

    const card = wrapper.get('.plan-card')
    expect(card.text()).toContain('筹备中 · 尚未绑定账号')
    expect(card.get('.plan-subscription').text()).toContain('等待房主接入账号')
    await findButton(wrapper, '申请加入')!.trigger('click')
    expect(wrapper.text()).toContain('批准加入后需等待房主接入账号，才能开始使用')
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

  it('shows the OpenAI subscription expiry without exposing the OAuth token expiry', () => {
    const wrapper = mount(AccountsView, { props: { accounts: [account] } })
    expect(wrapper.text()).toContain('订阅有效期至')
    expect(wrapper.text()).not.toContain('OAuth Token 到期')
    expect(wrapper.text()).not.toContain('ShareSub ID')
    expect(wrapper.find('.account-logo').exists()).toBe(false)
    expect(wrapper.get('.account-identity h3').text()).toBe('共享账号')
    expect(wrapper.get('.account-email').text()).toBe('openai@example.com')
    expect(wrapper.get('.account-plan-type').text()).toBe('plus')
    expect(wrapper.text()).not.toContain('OPENAI@EXAMPLE.COM')
    expect(wrapper.text()).toContain('2026')
  })

  it('lets the account owner manually refresh one active account token', async () => {
    const refresh = vi.spyOn(api, 'refreshAccountToken').mockResolvedValue({ ...account, token_expires_at: '2026-08-14T00:00:00Z' })
    const wrapper = mount(AccountsView, { props: { accounts: [account] } })

    await wrapper.get('button[aria-label="刷新令牌"]').trigger('click')
    await flushPromises()

    expect(refresh).toHaveBeenCalledWith(account.id)
    expect(wrapper.emitted('message')).toContainEqual(['success', 'OpenAI 账号令牌已刷新'])
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('does not allow manual token refresh for a non-active owned account', () => {
    const wrapper = mount(AccountsView, { props: { accounts: [{ ...account, status: 'refresh_required' }] } })
    expect(wrapper.get('button[aria-label="刷新令牌"]').attributes('disabled')).toBeDefined()
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

  it('creates an API key with its own Fast policy', async () => {
    const createKey = vi.spyOn(api, 'createKey').mockResolvedValue({ api_key: apiKey, key: apiKey.key })
    const wrapper = mount(APIKeySetupWizard, {
      attachTo: document.body,
      props: { show: true, plans: [activePlan] },
      global: { stubs: { teleport: true } },
    })

    await findButton(wrapper, '新增规则')!.trigger('click')
    expect(wrapper.text()).toContain('仅在绑定账号透传或未命中时生效')
    await findButton(wrapper, '创建密钥')!.trigger('click')
    await flushPromises()

    expect(createKey).toHaveBeenCalledWith({
      name: '我的 Codex',
      strategy: 'balanced',
      routes: [{ plan_id: activePlan.id, plan_name: activePlan.name, priority: 100, enabled: true }],
      fast_policy: [{
        service_tier: 'priority', action: 'force_priority', user_ids: [], error_message: '',
        model_whitelist: [], fallback_action: 'pass', fallback_error_message: '',
      }],
    })
  })

  it('does not create new routes for archived Plans but preserves an existing archived route while editing', async () => {
    const archivedKey: APIKey = {
      ...apiKey,
      routes: [{ plan_id: archivedPlan.id, plan_name: archivedPlan.name, priority: 100, enabled: true }],
    }
    const updateKey = vi.spyOn(api, 'updateKey').mockResolvedValue(archivedKey)
    const wrapper = mount(KeysView, {
      attachTo: document.body,
      props: { keys: [archivedKey], plans: [archivedPlan] },
      global: { stubs: { teleport: true } },
    })

    expect(findButton(wrapper, '创建 API Key')!.attributes('disabled')).toBeDefined()
    await wrapper.get('button[aria-label="编辑配置"]').trigger('click')
    expect(wrapper.text()).toContain('已归档，恢复后自动生效')
    await findButton(wrapper, '保存配置')!.trigger('click')
    await flushPromises()

    expect(updateKey).toHaveBeenCalledWith(archivedKey.id, {
      name: archivedKey.name,
      strategy: archivedKey.strategy,
      routes: archivedKey.routes,
      fast_policy: [],
    })
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
