// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, clearSessionToken, sessionToken } from '../api'
import type { User } from '../types'
import EmailVerificationView from './EmailVerificationView.vue'

const token = `ss_verify_${'a'.repeat(43)}`
const user: User = {
  id: 'member', username: '成员', email: 'member@example.com', email_verified_at: '2026-08-17T09:00:00Z',
  avatar_url: '', status: 'active', created_at: '2026-08-17T08:00:00Z', role: 'user', is_admin: false, must_change_password: false,
}

afterEach(() => {
  clearSessionToken()
  vi.restoreAllMocks()
})

describe('email verification view', () => {
  it('requires an explicit confirmation before verifying and stores the returned session', async () => {
    const verify = vi.spyOn(api, 'verifyEmail').mockResolvedValue({ user, token: 'ss_session_test' })
    const wrapper = mount(EmailVerificationView, { props: { token }, attachTo: document.body })
    expect(verify).not.toHaveBeenCalled()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(verify).toHaveBeenCalledWith(token)
    expect(sessionToken()).toBe('ss_session_test')
    expect(wrapper.emitted('authenticated')?.[0]).toEqual([user])
  })

  it('shows a recovery action when the fragment has no valid token', () => {
    const wrapper = mount(EmailVerificationView, { props: { token: '' } })
    expect(wrapper.text()).toContain('验证链接不完整')
    expect(wrapper.findAll('button').some(button => button.text().includes('完成邮箱验证'))).toBe(false)
  })
})
