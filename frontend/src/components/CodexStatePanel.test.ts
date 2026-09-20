// @vitest-environment happy-dom
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CodexStatePanel from './CodexStatePanel.vue'
const api = vi.hoisted(() => ({ status: vi.fn(), refresh: vi.fn() }))
vi.mock('../api/codexState', () => ({ stateAPI: api }))
beforeEach(() => vi.resetAllMocks())
describe('CodexStatePanel', () => {
  it('shows the model registration empty state without guessing response fields', async () => {
    api.status.mockResolvedValue({ enabled: true, supported: true, models: [] })
    const wrapper = mount(CodexStatePanel, { props: { accountId: 'a' } })
    await flushPromises()
    expect(api.status).toHaveBeenCalledWith('a')
    expect(wrapper.text()).toContain('首次 HTTP Responses 请求')
    expect(wrapper.findAll('button').find(button => button.text() === '重新验证')?.attributes('disabled')).toBeDefined()
  })
  it('refreshes a ready model and renders an actionable load failure', async () => {
    const result = { enabled: true, supported: true, models: [{ model: 'm', status: 'ready', result: 'ready', expires_at: null, next_attempt_at: '2026-09-20T00:00:00Z', attempts: 1 }] }
    api.status.mockResolvedValue(result)
    api.refresh.mockRejectedValue(new Error('刷新失败'))
    const wrapper = mount(CodexStatePanel, { props: { accountId: 'a' } })
    await flushPromises()
    expect(wrapper.text()).toContain('m · 票据可用')
    await wrapper.findAll('button').find(button => button.text() === '重新验证')!.trigger('click')
    await flushPromises()
    expect(api.refresh).toHaveBeenCalledWith('a')
    expect(wrapper.get('[role="alert"]').text()).toBe('刷新失败')
  })
})
