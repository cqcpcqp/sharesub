// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TokenActivityHeatmap from './TokenActivityHeatmap.vue'
import type { DashboardDailyUsage, TokenUsage } from '../types'

function tokenUsage(totalTokens: number): TokenUsage {
  return {
    input_tokens: Math.floor(totalTokens * 0.8),
    output_tokens: Math.ceil(totalTokens * 0.2),
    cached_tokens: Math.floor(totalTokens * 0.4),
    cache_creation_tokens: 0,
    image_input_tokens: 0,
    image_output_tokens: 0,
    image_count: 0,
    total_tokens: totalTokens,
  }
}

function dailyUsage(): DashboardDailyUsage[] {
  const start = Date.UTC(2025, 7, 22)
  const activeValues = [100, 200, 300, 400, 10_000]
  return Array.from({ length: 365 }, (_, index) => ({
    usage_date: new Date(start + index * 86_400_000).toISOString().slice(0, 10),
    request_count: index < activeValues.length ? index + 1 : 0,
    token_usage: tokenUsage(activeValues[index] ?? 0),
  }))
}

describe('TokenActivityHeatmap', () => {
  it('renders the fixed 365-day contract with relative levels and a concise tooltip', async () => {
    const wrapper = mount(TokenActivityHeatmap, { props: { usage: dailyUsage() } })
    const cells = wrapper.findAll('.activity-cell')

    expect(cells).toHaveLength(365)
    expect(wrapper.find('.activity-summary').exists()).toBe(false)
    expect(cells[0].classes()).toContain('activity-level-1')
    expect(cells[4].classes()).toContain('activity-level-4')

    await cells[0].trigger('mouseenter')
    expect(wrapper.find('.activity-tooltip').text()).toContain('2025年8月22日')
    expect(wrapper.find('.activity-tooltip').text()).toContain('100 Token')
    expect(wrapper.find('.activity-tooltip').text()).not.toContain('Input')
    expect(wrapper.find('.activity-tooltip').text()).not.toContain('Output')
    expect(wrapper.find('.activity-tooltip').text()).not.toContain('Cached')
    expect(wrapper.find('.activity-tooltip').text()).not.toContain('次请求')
    expect(cells[0].attributes('aria-label')).toBe('2025年8月22日：100 Token')
    expect(cells[4].attributes('aria-label')).toContain('10K Token')

    await cells[0].trigger('mouseleave')
    expect(wrapper.find('.activity-tooltip').exists()).toBe(false)
    await cells[4].trigger('click')
    expect(wrapper.find('.activity-tooltip').text()).toContain('10K Token')
  })

  it('supports roving keyboard navigation across week columns', async () => {
    const wrapper = mount(TokenActivityHeatmap, { attachTo: document.body, props: { usage: dailyUsage() } })
    const firstCell = wrapper.get<HTMLElement>('[data-usage-index="0"]')
    await firstCell.trigger('focus')
    await firstCell.trigger('keydown', { key: 'ArrowRight' })

    expect(wrapper.get('[data-usage-index="7"]').attributes('tabindex')).toBe('0')
    expect(document.activeElement).toBe(wrapper.get('[data-usage-index="7"]').element)
    expect(wrapper.find('.activity-tooltip').text()).toContain('2025年8月29日')
    await wrapper.get('[data-usage-index="7"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('.activity-tooltip').exists()).toBe(false)
    expect(document.activeElement).toBe(wrapper.get('[data-usage-index="7"]').element)
    wrapper.unmount()
  })
})
