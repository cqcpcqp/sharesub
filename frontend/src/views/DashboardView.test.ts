// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import DashboardView from './DashboardView.vue'

describe('DashboardView refresh', () => {
  it('emits a page refresh from the dashboard action', async () => {
    const wrapper = mount(DashboardView, {
      props: {
        dashboard: {
          today_tokens: { input_tokens: 1, output_tokens: 2, cached_tokens: 0, total_tokens: 3 },
          total_tokens: { input_tokens: 1, output_tokens: 2, cached_tokens: 0, total_tokens: 3 },
          performance: {
            requests_today: 1,
            success_rate: 100,
            requests_per_minute: 1,
            tokens_per_minute: 3,
            average_ttft_ms: 10,
            average_duration_ms: 20,
            active_plans: 1,
          },
          trend: [],
        },
        loading: false,
        refreshing: false,
        theme: 'light',
      },
      global: { stubs: { TokenUsageChart: true } },
    })

    await wrapper.get('button[aria-label="刷新仪表盘数据"]').trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)
  })
})
