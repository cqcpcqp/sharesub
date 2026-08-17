// @vitest-environment happy-dom

import { config, shallowMount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import type { User } from '../types'
import ProfileView from './ProfileView.vue'
import { buildInfo } from '../buildInfo'

const member: User = {
  id: 'member',
  username: 'member',
  email: 'member@example.com',
  email_verified_at: '2026-08-04T00:00:00Z',
  avatar_url: '',
  status: 'active',
  created_at: '2026-08-04T00:00:00Z',
  role: 'user',
  is_admin: false,
  must_change_password: false,
}

afterEach(() => { config.global.renderStubDefaultSlot = false })

describe('ProfileView password settings', () => {
  it('shows the exact Web release identity', () => {
    const wrapper = shallowMount(ProfileView, { props: { user: member, themeMode: 'system' } })

    expect(wrapper.get('.release-metadata').text()).toContain(buildInfo.version)
    expect(wrapper.get('.release-metadata').text()).toContain(buildInfo.revision)
  })

  it('opens the regular password dialog and publishes the updated user', async () => {
    config.global.renderStubDefaultSlot = true
    const wrapper = shallowMount(ProfileView, { props: { user: member, themeMode: 'system' } })
    const changePasswordButton = wrapper.findAllComponents({ name: 'Button' }).find(button => button.text() === '修改密码')
    expect(changePasswordButton).toBeDefined()

    changePasswordButton!.vm.$emit('click')
    await wrapper.vm.$nextTick()
    const dialog = wrapper.getComponent({ name: 'PasswordChangeDialog' })
    expect(dialog.props('forced')).toBe(false)

    dialog.vm.$emit('changed', member)
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('updated')).toEqual([[member]])
    expect(wrapper.emitted('message')).toEqual([['success', '密码已更新，其他设备的登录会话已失效']])
    expect(wrapper.findComponent({ name: 'PasswordChangeDialog' }).exists()).toBe(false)
  })
})
