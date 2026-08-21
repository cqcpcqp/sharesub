// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import DashboardView from './DashboardView.vue'
import type { Dashboard } from '../types'

function dashboard(): Dashboard {
  return {
    today_tokens: { input_tokens: 400, output_tokens: 200, cached_tokens: 100, cache_creation_tokens: 0, image_input_tokens: 0, image_output_tokens: 0, image_count: 0, total_tokens: 700 },
    total_tokens: { input_tokens: 10_000, output_tokens: 5_000, cached_tokens: 2_000, cache_creation_tokens: 0, image_input_tokens: 0, image_output_tokens: 0, image_count: 0, total_tokens: 17_000 },
    today_web_search_calls: 0,
    total_web_search_calls: 0,
    performance: {
      requests_today: 12,
      success_rate: 98.5,
      requests_per_minute: 3,
      tokens_per_minute: 900,
      average_ttft_ms: 120,
      average_duration_ms: 850,
      active_plans: 2,
    },
    trend: [{ bucket_start: '2026-08-21T08:00:00Z', input_tokens: 600, output_tokens: 400, cached_tokens: 0, cache_creation_tokens: 0, image_input_tokens: 0, image_output_tokens: 0, image_count: 2, web_search_calls: 3 }],
    daily_usage: [
      { usage_date: '2026-08-20', request_count: 2, token_usage: { input_tokens: 600, output_tokens: 400, cached_tokens: 0, cache_creation_tokens: 0, image_input_tokens: 0, image_output_tokens: 0, image_count: 0, total_tokens: 1_000 } },
      { usage_date: '2026-08-21', request_count: 0, token_usage: { input_tokens: 0, output_tokens: 0, cached_tokens: 0, cache_creation_tokens: 0, image_input_tokens: 0, image_output_tokens: 0, image_count: 0, total_tokens: 0 } },
    ],
  }
}

function mountDashboard(attachToDocument = false) {
  return mount(DashboardView, {
    attachTo: attachToDocument ? document.body : undefined,
    props: {
      dashboard: dashboard(),
      loading: false,
      refreshing: false,
      theme: 'light',
    },
    global: { stubs: { TokenActivityHeatmap: true, TokenUsageChart: true } },
  })
}

describe('DashboardView', () => {
  it('emits a page refresh from the dashboard action', async () => {
    const wrapper = mountDashboard()

    await wrapper.get('button[aria-label="刷新仪表盘数据"]').trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)
  })

  it('uses an open metric band instead of dashboard cards', () => {
    const wrapper = mountDashboard()

    expect(wrapper.find('.dashboard-metric-band').exists()).toBe(true)
    expect(wrapper.findAll('.dashboard-overview-ledger > div')).toHaveLength(4)
    expect(wrapper.find('.dashboard-total-metric').exists()).toBe(false)
    expect(wrapper.find('.dashboard-kpi').exists()).toBe(false)
    expect(wrapper.find('.dashboard-trend-panel').exists()).toBe(false)
  })

  it('keeps annual activity and the 24-hour trend visible together', () => {
    const wrapper = mountDashboard()

    expect(wrapper.get('.dashboard-annual-facts').text()).toContain('活跃日1最近 365 天')
    expect(wrapper.get('.dashboard-annual-facts').text()).toContain('年度覆盖50%')
    expect(wrapper.findComponent({ name: 'TokenActivityHeatmap' }).exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'TokenUsageChart' }).exists()).toBe(true)
    expect(wrapper.find('[role="tablist"]').exists()).toBe(false)
  })

  it('derives annual rhythm and 24-hour tool usage from the existing dashboard contract', () => {
    const wrapper = mountDashboard()

    expect(wrapper.get('.dashboard-annual-facts').text()).toContain('年度请求2')
    expect(wrapper.get('.dashboard-annual-facts').text()).toContain('活跃日均量1K')
    expect(wrapper.get('.dashboard-annual-facts').text()).toContain('峰值日8月20日1K Token')
    expect(wrapper.get('.dashboard-trend-facts').text()).toContain('Token1K')
    expect(wrapper.get('.dashboard-trend-facts').text()).toContain('Web Search3')
    expect(wrapper.get('.dashboard-trend-facts').text()).toContain('图片2')
  })
})
