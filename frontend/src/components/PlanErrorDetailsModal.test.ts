// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api'
import PlanErrorDetailsModal from './PlanErrorDetailsModal.vue'

const errorItem = {
  id: 42,
  request_id: 'request-42',
  endpoint: '/v1/responses',
  is_stream: true,
  status_code: 503,
  error_source: 'upstream' as const,
  error_code: 'server_error',
  error_message: 'upstream temporarily unavailable',
  requested_model: 'gpt-5.6-sol',
  upstream_model: 'gpt-5.6-sol',
  service_tier: 'priority',
  duration_ms: 5728,
  member_id: 'member-1',
  member_username: '成员一',
  account_id: 'account-1',
  account_name: '团队账号',
  api_key_name: 'Codex Key',
  api_key_prefix: 'sk-sharesub-abcd',
  created_at: '2026-08-07T12:02:30Z',
}

const modalStub = {
  props: ['title', 'subtitle'],
  emits: ['close'],
  template: '<section><h2>{{ title }}</h2><p>{{ subtitle }}</p><slot /></section>',
}

afterEach(() => vi.restoreAllMocks())

describe('PlanErrorDetailsModal', () => {
  it('loads the fixed Plan error response and expands a row', async () => {
    const request = vi.spyOn(api, 'planRequestErrors').mockResolvedValue({ items: [errorItem], total: 1, page: 1, page_size: 20 })
    const wrapper = mount(PlanErrorDetailsModal, {
      props: { planId: 'plan-1', period: '24h' },
      global: { stubs: { ModalShell: modalStub } },
    })
    await flushPromises()

    expect(request).toHaveBeenCalledWith('plan-1', '24h', 1, 20, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('1条错误')
    expect(wrapper.text()).toContain('server_error')
    expect(wrapper.text()).toContain('upstream temporarily unavailable')
    await wrapper.get('button[aria-label="查看请求 request-42 详情"]').trigger('click')
    expect(wrapper.text()).toContain('/v1/responses')
    expect(wrapper.text()).toContain('团队账号')
    expect(wrapper.text()).toContain('Codex Key')
  })

  it('shows an explicit empty state when the period has no errors', async () => {
    vi.spyOn(api, 'planRequestErrors').mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    const wrapper = mount(PlanErrorDetailsModal, {
      props: { planId: 'plan-1', period: 'today' },
      global: { stubs: { ModalShell: modalStub } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('这个时间段没有错误')
    expect(wrapper.text()).toContain('当前所有已记录请求都返回了 2xx')
  })

  it('shows the API error and offers a retry action', async () => {
    vi.spyOn(api, 'planRequestErrors').mockRejectedValue(new Error('数据库暂时不可用'))
    const wrapper = mount(PlanErrorDetailsModal, {
      props: { planId: 'plan-1', period: '6h' },
      global: { stubs: { ModalShell: modalStub } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('错误明细加载失败')
    expect(wrapper.text()).toContain('数据库暂时不可用')
    expect(wrapper.get('button').text()).toContain('重试')
  })
})
