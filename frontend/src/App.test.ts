// @vitest-environment happy-dom

import { config, flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import { api, setSessionToken } from './api'
import type { Dashboard, Notification, NotificationList, Plan, User } from './types'

const temporaryAdmin: User = {
  id: 'admin', username: 'admin', email: 'admin@underelay.com', avatar_url: '', status: 'active',
  created_at: '2026-08-04T00:00:00Z', role: 'admin', is_admin: true, must_change_password: true,
}

const member: User = {
  id: 'member', username: 'member', email: 'member@example.com', avatar_url: '', status: 'active',
  created_at: '2026-08-06T00:00:00Z', role: 'user', is_admin: false, must_change_password: false,
}

const approvedPlan: Plan = {
  id: 'approved-plan', owner_user_id: 'owner', account_id: 'account', name: '已批准 Plan', description: '',
  status: 'active', visibility: 'public', public_slots: 1, public_share_basis_points: 0,
  allocation_mode: 'shared', created_at: '2026-08-06T00:00:00Z',
}

const approvalNotification: Notification = {
  id: 'approval-notification', user_id: member.id, type: 'application_approved', title: '已加入 Plan',
  body: '你的加入申请已通过', resource_type: 'plan', resource_id: approvedPlan.id,
  created_at: '2026-08-06T00:00:00Z',
}

function mockMatchMedia() {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }),
  })
}

function mockWorkspace(plans: Plan[]) {
  config.global.renderStubDefaultSlot = true
  window.history.replaceState(null, '', '/dashboard')
  setSessionToken('ss_session_current')
  vi.spyOn(api, 'me').mockResolvedValue(member)
  vi.spyOn(api, 'dashboard').mockResolvedValue({} as Dashboard)
  vi.spyOn(api, 'accounts').mockResolvedValue([])
  vi.spyOn(api, 'plans').mockResolvedValue(plans)
  vi.spyOn(api, 'keys').mockResolvedValue([])
  vi.spyOn(api, 'publicPlans').mockResolvedValue([])
  vi.spyOn(api, 'notifications').mockResolvedValue({ items: [approvalNotification], unread_count: 1 })
}

afterEach(() => {
  vi.restoreAllMocks()
  config.global.renderStubDefaultSlot = false
  localStorage.clear()
  window.history.replaceState(null, '', '/')
})

describe('forced password change workspace', () => {
  it('mounts the requested view only after the password is changed', async () => {
    mockMatchMedia()
    window.history.replaceState(null, '', '/admin')
    setSessionToken('ss_session_current')
    vi.spyOn(api, 'me').mockResolvedValue(temporaryAdmin)
    vi.spyOn(api, 'dashboard').mockResolvedValue({} as Dashboard)
    vi.spyOn(api, 'accounts').mockResolvedValue([])
    vi.spyOn(api, 'plans').mockResolvedValue([])
    vi.spyOn(api, 'keys').mockResolvedValue([])
    vi.spyOn(api, 'publicPlans').mockResolvedValue([])
    vi.spyOn(api, 'notifications').mockResolvedValue({ items: [], unread_count: 0 } as NotificationList)

    config.global.renderStubDefaultSlot = true
    const wrapper = shallowMount(App)
    await flushPromises()
    expect(wrapper.find('.app-shell').exists()).toBe(false)
    const passwordDialog = wrapper.getComponent({ name: 'PasswordChangeDialog' })

    passwordDialog.vm.$emit('changed', { ...temporaryAdmin, must_change_password: false })
    await flushPromises()
    expect(wrapper.find('.app-shell').exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'AdminView' }).exists()).toBe(true)
    wrapper.unmount()
  })
})

describe('approval notification routing', () => {
  it('does not open API Key setup for an unbound Plan', async () => {
    mockMatchMedia()
    mockWorkspace([{ ...approvedPlan, account_id: '' }])
    const wrapper = shallowMount(App)
    await flushPromises()

    wrapper.getComponent({ name: 'NotificationCenter' }).vm.$emit('open', approvalNotification)
    await flushPromises()

    expect(wrapper.getComponent({ name: 'APIKeySetupWizard' }).props('show')).toBe(false)
    wrapper.unmount()
  })

  it('opens API Key setup for a bound active Plan', async () => {
    mockMatchMedia()
    mockWorkspace([approvedPlan])
    const wrapper = shallowMount(App)
    await flushPromises()

    wrapper.getComponent({ name: 'NotificationCenter' }).vm.$emit('open', approvalNotification)
    await flushPromises()

    const wizard = wrapper.getComponent({ name: 'APIKeySetupWizard' })
    expect(wizard.props('show')).toBe(true)
    expect(wizard.props('initialPlanId')).toBe(approvedPlan.id)
    wrapper.unmount()
  })
})
