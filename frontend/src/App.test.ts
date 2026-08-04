// @vitest-environment happy-dom

import { config, flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import { api, setSessionToken } from './api'
import type { Dashboard, NotificationList, User } from './types'

const temporaryAdmin: User = {
  id: 'admin', username: 'admin', email: 'admin@underelay.com', avatar_url: '', status: 'active',
  created_at: '2026-08-04T00:00:00Z', role: 'admin', is_admin: true, must_change_password: true,
}

afterEach(() => {
  vi.restoreAllMocks()
  config.global.renderStubDefaultSlot = false
  localStorage.clear()
  window.history.replaceState(null, '', '/')
})

describe('forced password change workspace', () => {
  it('mounts the requested view only after the password is changed', async () => {
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }),
    })
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
