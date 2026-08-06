// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { NPopconfirm } from 'naive-ui'
import { describe, expect, it } from 'vitest'
import QuotaResetControls from './QuotaResetControls.vue'

const baseProps = {
  credits: null,
  querying: false,
  resetting: false,
  disabled: false,
  allowReset: true,
}

describe('QuotaResetControls', () => {
  it('queries reset credits before enabling reset', async () => {
    const wrapper = mount(QuotaResetControls, { props: baseProps })

    await wrapper.get('button[aria-label="查询重置次数"]').trigger('click')

    expect(wrapper.emitted('query')).toHaveLength(1)
    expect(wrapper.get('button[aria-label="重置 OpenAI 额度"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('查询 OpenAI 当前可用次数及到期时间')
  })

  it('renders the exact available count and expandable expirations', async () => {
    const wrapper = mount(QuotaResetControls, {
      props: {
        ...baseProps,
        credits: {
          available_count: 2,
          credits: [
            { expires_at: '2026-08-13T02:13:00Z' },
            { expires_at: '2026-08-12T05:09:00Z' },
          ],
          fetched_at: '2026-08-06T10:00:00Z',
        },
      },
    })

    expect(wrapper.text()).toContain('次数 2')
    expect(wrapper.text()).toContain('+1')
    await wrapper.get('button[aria-label="查看其余 1 个到期时间"]').trigger('click')
    expect(wrapper.findAll('.quota-reset-expiration-list span')).toHaveLength(2)
    expect(wrapper.findAll('.quota-reset-expiration-list span')[0].attributes('title')).toContain('2026')
  })

  it('emits reset only after confirmation and blocks busy or empty states', async () => {
    const wrapper = mount(QuotaResetControls, {
      props: {
        ...baseProps,
        credits: {
          available_count: 1,
          credits: [{ expires_at: '2026-08-12T05:09:00Z' }],
          fetched_at: '2026-08-06T10:00:00Z',
        },
      },
    })

    expect(wrapper.get('button[aria-label="重置 OpenAI 额度"]').attributes('disabled')).toBeUndefined()
    wrapper.getComponent(NPopconfirm).vm.$emit('positiveClick')
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('reset')).toHaveLength(1)

    await wrapper.setProps({ querying: true })
    expect(wrapper.get('button[aria-label="查询重置次数"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button[aria-label="重置 OpenAI 额度"]').attributes('disabled')).toBeDefined()

    await wrapper.setProps({ querying: false, credits: { available_count: 0, credits: [], fetched_at: '2026-08-06T10:00:00Z' } })
    expect(wrapper.text()).toContain('当前没有可用的重置机会')
    expect(wrapper.get('button[aria-label="重置 OpenAI 额度"]').attributes('disabled')).toBeDefined()
  })

  it('lets members query and inspect credits without enabling reset', async () => {
    const wrapper = mount(QuotaResetControls, {
      props: {
        ...baseProps,
        allowReset: false,
        credits: {
          available_count: 1,
          credits: [{ expires_at: '2026-08-12T05:09:00Z' }],
          fetched_at: '2026-08-06T10:00:00Z',
        },
      },
    })

    await wrapper.get('button[aria-label="查询重置次数"]').trigger('click')
    expect(wrapper.emitted('query')).toHaveLength(1)
    expect(wrapper.text()).toContain('次数 1')
    expect(wrapper.get('button[aria-label="重置 OpenAI 额度"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button[aria-label="重置 OpenAI 额度"]').attributes('title')).toBe('只有房主可以执行额度重置')
  })
})
