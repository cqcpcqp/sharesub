// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import InvitationSummary from './InvitationSummary.vue'

describe('InvitationSummary', () => {
  it('shows the fixed allocation, member share, owner, and expiry', () => {
    const wrapper = mount(InvitationSummary, {
      props: {
        preview: {
          plan_id: 'plan',
          plan_name: '设计协作',
          owner_username: '房主甲',
          allocation_mode: 'fixed',
          share_basis_points: 2500,
          expires_at: '2026-08-14T08:00:00Z',
        },
      },
    })

    expect(wrapper.text()).toContain('设计协作')
    expect(wrapper.text()).toContain('房主甲')
    expect(wrapper.text()).toContain('固定分配')
    expect(wrapper.text()).toContain('25%')
    expect(wrapper.text()).toContain('有效期至')
  })

  it('explains a zero-percent view-only invitation', () => {
    const wrapper = mount(InvitationSummary, {
      props: {
        preview: {
          plan_id: 'plan',
          plan_name: '只读协作',
          owner_username: '房主乙',
          allocation_mode: 'fixed',
          share_basis_points: 0,
          expires_at: '2026-08-14T08:00:00Z',
        },
      },
    })

    expect(wrapper.text()).toContain('仅查看，不能发起请求')
  })
})
