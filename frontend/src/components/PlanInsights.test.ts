// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { NSelect } from 'naive-ui'
import { describe, expect, it } from 'vitest'
import PlanInsights from './PlanInsights.vue'

describe('PlanInsights performance period', () => {
  it('offers today and the four fixed periods and emits a selection change', async () => {
    const wrapper = mount(PlanInsights, {
      props: {
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
            { period: 'today', window_start: '2026-08-04T00:00:00Z', window_end: '2026-08-04T10:00:00Z', members: [] },
          ],
          model_usage: [], token_trend: [], recent_usage: [],
        },
        members: [],
        allocationMode: 'shared',
        performancePeriod: '24h',
        theme: 'light',
      },
    })

    const selects = wrapper.findAllComponents(NSelect)
    expect(wrapper.text()).toContain('模型分布')
    expect(wrapper.text()).toContain('Token 使用趋势')
    expect(wrapper.text()).toContain('最近使用')
    const performanceSelect = selects.find(select => select.attributes('aria-label') === '性能统计时间段')!
    expect(performanceSelect.props('options')).toEqual([
      { value: 'today', label: '本日' },
      { value: '30m', label: '最近 30 分钟' },
      { value: '6h', label: '最近 6 小时' },
      { value: '12h', label: '最近 12 小时' },
      { value: '24h', label: '最近 24 小时' },
    ])
    performanceSelect.vm.$emit('update:value', '6h')
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('update:performancePeriod')).toEqual([['6h']])
    await wrapper.setProps({ performancePeriod: '6h' })
    expect(wrapper.find('.analytics-grid').text()).toContain('最近 6 小时')
    expect(wrapper.find('.recent-usage-panel').text()).toContain('最近 6 小时')
    expect(wrapper.find('.recent-usage-panel').text()).not.toContain('TOP 12')
    const rankingSelect = selects.find(select => select !== performanceSelect && (select.props('options') as Array<{ value: string }>).some(option => option.value === 'today'))!
    expect(rankingSelect.props('value')).toBe('today')
  })

  it('explains estimated member quota and displays separate 5h and 7d usage', () => {
    const wrapper = mount(PlanInsights, {
      props: {
        insights: {
          account_windows: [
            { window_type: '5h', used_micros: 20_000_000, account_used_micros: 20_000_000, reset_at: '2026-08-04T15:00:00Z' },
            { window_type: '7d', used_micros: 33_000_000, account_used_micros: 33_000_000, reset_at: '2026-08-08T00:00:00Z' },
          ],
          member_quotas: [
            { member_id: 'member-a', windows: [
              { window_type: '5h', used_micros: 16_000_000, account_used_micros: 20_000_000, reset_at: '2026-08-04T15:00:00Z' },
              { window_type: '7d', used_micros: 19_800_000, account_used_micros: 33_000_000, reset_at: '2026-08-08T00:00:00Z' },
            ] },
            { member_id: 'member-b', windows: [
              { window_type: '5h', used_micros: 4_000_000, account_used_micros: 20_000_000, reset_at: '2026-08-04T15:00:00Z' },
              { window_type: '7d', used_micros: 13_200_000, account_used_micros: 33_000_000, reset_at: '2026-08-08T00:00:00Z' },
            ] },
          ],
          performance: { request_count: 0, success_count: 0, average_ttft_ms: 0, p95_ttft_ms: 0, average_duration_ms: 0, p95_duration_ms: 0 },
          window_usage: [],
          member_ranking: [],
          member_rankings: [{ period: 'today', window_start: '2026-08-04T00:00:00Z', window_end: '2026-08-04T10:00:00Z', members: [] }],
          model_usage: [], token_trend: [], recent_usage: [],
        },
        members: [
          { id: 'member-a', plan_id: 'plan', user_id: 'user-a', username: '成员 A', avatar_url: '', email: 'a@example.com', role: 'member', status: 'active', share_basis_points: 0, created_at: '2026-08-04T00:00:00Z' },
          { id: 'member-b', plan_id: 'plan', user_id: 'user-b', username: '成员 B', avatar_url: '', email: 'b@example.com', role: 'member', status: 'active', share_basis_points: 0, created_at: '2026-08-04T00:00:00Z' },
        ],
        allocationMode: 'shared',
        theme: 'light',
      },
      global: { stubs: { MemberCostShareChart: true } },
    })

    expect(wrapper.get('button[aria-label="查看成员估算额度口径"]').attributes('aria-label')).toBe('查看成员估算额度口径')
    expect(wrapper.findAll('.member-share-window')).toHaveLength(2)
    const memberWindows = wrapper.findAll('.member-windows').map(item => item.text())
    expect(memberWindows[0]).toContain('5h16.0%')
    expect(memberWindows[0]).toContain('7d19.8%')
    expect(memberWindows[1]).toContain('5h4.0%')
    expect(memberWindows[1]).toContain('7d13.2%')
  })

  it('shows total tokens before the token breakdown in every quota card', () => {
    const wrapper = mount(PlanInsights, {
      props: {
        insights: {
          account_windows: [],
          member_quotas: [],
          performance: { request_count: 0, success_count: 0, average_ttft_ms: 0, p95_ttft_ms: 0, average_duration_ms: 0, p95_duration_ms: 0 },
          window_usage: [],
          member_ranking: [],
          member_rankings: [{ period: 'today', window_start: '2026-08-04T00:00:00Z', window_end: '2026-08-04T10:00:00Z', members: [] }],
          model_usage: [], token_trend: [], recent_usage: [],
        },
        members: [],
        allocationMode: 'shared',
        theme: 'light',
      },
    })

    for (const tokenGrid of wrapper.findAll('.token-grid')) {
      expect(tokenGrid.findAll('dt').map(item => item.text())).toEqual(['Total Token', 'Input', 'Output', 'Cached'])
    }
  })
})
