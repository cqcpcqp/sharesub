// @vitest-environment happy-dom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { paymentSettingsAPI, type PaymentSettings } from '../api/paymentSettings'
import PaymentSettingsDialog from './PaymentSettingsDialog.vue'

vi.mock('../api/paymentSettings', () => ({ paymentSettingsAPI: { get: vi.fn(), save: vi.fn() } }))
const base: PaymentSettings = { base_url: 'https://pay.example.test', pid: 'merchant', enabled: false, revision: 1, started_at: null, key_configured: true, source: 'database', public_url: 'https://share.example.test', callback_ready: true }
let wrapper: VueWrapper
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(paymentSettingsAPI.get).mockResolvedValue({ ...base })
  vi.mocked(paymentSettingsAPI.save).mockResolvedValue({ ...base, revision: 2 })
})
afterEach(() => wrapper.unmount())
async function open() {
  wrapper = mount(PaymentSettingsDialog, { global: { stubs: { ModalShell: { template: '<div><slot /><slot name="footer" /></div>' } } } })
  await flushPromises()
}
async function click(text: string) {
  const button = wrapper.findAll('button').find(item => item.text() === text)
  expect(button).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}
it('saves without activating and keeps the existing secret when blank', async () => {
  await open()
  expect(wrapper.get<HTMLInputElement>('input[type="password"]').element.value).toBe('')
  await click('保存配置')
  expect(paymentSettingsAPI.save).toHaveBeenCalledWith({ base_url: base.base_url, pid: base.pid, key: '', enabled: false, revision: 1 })
})
it('requires a separate confirmation before enabling payment', async () => {
  await open()
  await click('启用收款')
  expect(paymentSettingsAPI.save).not.toHaveBeenCalled()
  expect(wrapper.text()).toContain('无法通过关闭收款撤销收费门禁')
  await click('确认启用')
  expect(paymentSettingsAPI.save).toHaveBeenCalledWith(expect.objectContaining({ enabled: true }))
})
it('does not allow activation without an HTTPS callback', async () => {
  vi.mocked(paymentSettingsAPI.get).mockResolvedValue({ ...base, callback_ready: false })
  await open()
  expect(wrapper.findAll('button').find(item => item.text() === '启用收款')!.attributes('disabled')).toBeDefined()
})
it('reports load errors and supports retry', async () => {
  vi.mocked(paymentSettingsAPI.get).mockRejectedValueOnce(new Error('读取失败'))
  await open()
  expect(wrapper.text()).toContain('读取失败')
  await click('重试')
  expect(wrapper.text()).toContain('异步通知地址')
})
