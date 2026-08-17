// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api'
import type { User } from '../types'
import PasswordChangeDialog from './PasswordChangeDialog.vue'

afterEach(() => vi.restoreAllMocks())

describe('PasswordChangeDialog', () => {
  it('cannot be dismissed and completes the forced password change', async () => {
    const updated: User = { id: 'admin', username: 'admin', email: 'admin@underelay.com', email_verified_at: '2026-08-04T00:00:00Z', avatar_url: '', status: 'active', created_at: '2026-08-04T00:00:00Z', role: 'admin', is_admin: true, must_change_password: false }
    vi.spyOn(api, 'changePassword').mockResolvedValue(updated)
    const wrapper = mount(PasswordChangeDialog, { attachTo: document.body, global: { stubs: { teleport: true } } })
    expect(wrapper.find('button[aria-label="关闭"]').exists()).toBe(false)
    const inputs = wrapper.findAll('input')
    await inputs[0].setValue('temporary-password')
    await inputs[1].setValue('a-new-secure-password')
    await inputs[2].setValue('a-new-secure-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.changePassword).toHaveBeenCalledWith('temporary-password', 'a-new-secure-password')
    expect(wrapper.emitted('changed')).toEqual([[updated]])
    wrapper.unmount()
  })

  it('supports a dismissible password change for signed-in users', async () => {
    const updated: User = { id: 'member', username: 'member', email: 'member@example.com', email_verified_at: '2026-08-04T00:00:00Z', avatar_url: '', status: 'active', created_at: '2026-08-04T00:00:00Z', role: 'user', is_admin: false, must_change_password: false }
    vi.spyOn(api, 'changePassword').mockResolvedValue(updated)
    const wrapper = mount(PasswordChangeDialog, { props: { forced: false }, attachTo: document.body, global: { stubs: { teleport: true } } })
    expect(wrapper.text()).toContain('修改密码')
    expect(wrapper.text()).toContain('当前密码')
    expect(wrapper.find('button[aria-label="关闭"]').exists()).toBe(true)
    const inputs = wrapper.findAll('input')
    await inputs[0].setValue('current-password')
    await inputs[1].setValue('a-new-secure-password')
    await inputs[2].setValue('a-new-secure-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.changePassword).toHaveBeenCalledWith('current-password', 'a-new-secure-password')
    expect(wrapper.emitted('changed')).toEqual([[updated]])
    wrapper.unmount()
  })
})
