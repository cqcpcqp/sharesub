// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { QuotaResetVote } from '../types'
import QuotaResetVotePanel from './QuotaResetVotePanel.vue'

function fixedVote(overrides: Partial<QuotaResetVote> = {}): QuotaResetVote {
  return {
    id: 'vote-1', plan_id: 'plan-1', initiator_member_id: 'member-1', initiator_user_id: 'user-1', initiator_username: 'alice',
    allocation_mode: 'fixed', status: 'active', eligible_count: 3, eligible_weight_basis_points: 10000,
    support_count: 1, support_weight_basis_points: 5000, oppose_count: 0, current_user_choice: '', can_vote: true,
    windows_reset: 0, result_code: '', created_at: '2026-08-23T10:00:00Z', expires_at: '2099-08-23T12:00:00Z',
    execution_started_at: null, completed_at: null,
    members: [
      { member_id: 'member-1', user_id: 'user-1', username: 'alice', avatar_url: '', weight_basis_points: 5000, choice: 'support', voted_at: '2026-08-23T10:00:00Z' },
      { member_id: 'member-2', user_id: 'user-2', username: 'bob', avatar_url: '', weight_basis_points: 3000, choice: '', voted_at: null },
      { member_id: 'member-3', user_id: 'user-3', username: 'carol', avatar_url: '', weight_basis_points: 2000, choice: '', voted_at: null },
    ],
    ...overrides,
  }
}

describe('QuotaResetVotePanel', () => {
  it('explains that fixed votes must be strictly above fifty percent', () => {
    const wrapper = mount(QuotaResetVotePanel, { props: { vote: fixedVote(), loading: false, action: '' } })
    expect(wrapper.text()).toContain('严格超过整个 Plan 的 50%')
    expect(wrapper.text()).toContain('50%')
    wrapper.unmount()
  })

  it('emits support and oppose choices', async () => {
    const wrapper = mount(QuotaResetVotePanel, { props: { vote: fixedVote(), loading: false, action: '' } })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('cast')?.[0]).toEqual(['support'])
    await wrapper.findAll('button')[1].trigger('click')
    expect(wrapper.emitted('cast')?.[1]).toEqual(['oppose'])
    wrapper.unmount()
  })

  it('renders shared voting as one person one vote', () => {
    const wrapper = mount(QuotaResetVotePanel, {
      props: { vote: fixedVote({ allocation_mode: 'shared', eligible_count: 4, support_count: 2, support_weight_basis_points: 0 }), loading: false, action: '' },
    })
    expect(wrapper.text()).toContain('需要至少 3 票赞成')
    expect(wrapper.text()).toContain('2 / 4 票')
    wrapper.unmount()
  })

  it('warns against retrying when the external result is unknown', () => {
    const wrapper = mount(QuotaResetVotePanel, {
      props: { vote: fixedVote({ status: 'outcome_unknown', can_vote: false }), loading: false, action: '' },
    })
    expect(wrapper.text()).toContain('不要重复操作')
    wrapper.unmount()
  })
})
