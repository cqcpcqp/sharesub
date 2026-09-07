// @vitest-environment happy-dom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { NSelect, NInputNumber, NPopconfirm } from 'naive-ui'
import { membershipAPI, type Membership, type MembershipOrder } from '../api/membership'
import MembershipPanel from './MembershipPanel.vue'

vi.mock('../api/membership', () => ({ membershipAPI: { get: vi.fn(), orders: vi.fn(), checkout: vi.fn(), verify: vi.fn(), cancel: vi.fn(), adjust: vi.fn() } }))
const base: Membership = { user_id: 'user', tier: 'vip', active: true, expires_at: '2099-10-01T00:00:00Z', owner_limit_override: null, owner_limit: 2, owned_plans: 0, revision: 1, source: 'payment', billing_started: true, payment_enabled: true }
const order: MembershipOrder = { id: 'order', user_id: 'user', product: 'upgrade', amount_cents: 1000, upgrade_expires_at: base.expires_at, payment_method: 'alipay', status: 'review_required', trade_no: 'trade', created_at: '2026-09-07T00:00:00Z', expires_at: '2026-09-07T00:30:00Z', paid_at: '2026-09-07T00:31:00Z', service_expires_at: null }
let wrapper: VueWrapper
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(membershipAPI.get).mockResolvedValue({ ...base })
  vi.mocked(membershipAPI.orders).mockResolvedValue([])
})
afterEach(() => wrapper?.unmount())
async function open(adminUserId = '') {
  wrapper = mount(MembershipPanel, { props: { adminUserId } })
  await flushPromises()
}
describe('personal membership', () => {
  it('does not claim capacity is enforced before membership rollout', async () => {
    vi.mocked(membershipAPI.get).mockResolvedValue({ ...base, billing_started: false, owned_plans: 3 })
    await open()
    expect(wrapper.text()).toContain('现有使用不受影响')
    expect(wrapper.text()).not.toContain('禁止新增')
  })
  it('shows the capacity boundary when all slots are occupied', async () => {
    vi.mocked(membershipAPI.get).mockResolvedValue({ ...base, owned_plans: 2 })
    await open()
    expect(wrapper.text()).toContain('名额已用满，禁止新增')
  })
  it('prevents saving stale membership while a refresh is in progress', async () => {
    await open('user')
    await wrapper.get('input[placeholder="必填：赠送、补偿或调整原因"]').setValue('调整名额')
    let finish!: (value: Membership) => void
    vi.mocked(membershipAPI.get).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await wrapper.findAll('button').find(button => button.text() === '刷新')!.trigger('click')
    expect(wrapper.findAll('button').find(button => button.text() === '保存权益调整')!.attributes('disabled')).toBeDefined()
    wrapper.findAllComponents(NPopconfirm).find(component => component.text().includes('保存权益调整'))!.vm.$emit('positive-click')
    await flushPromises()
    expect(membershipAPI.adjust).not.toHaveBeenCalled()
    finish({ ...base, revision: 2 })
    await flushPromises()
  })
  it('offers VIP renewal and a fixed-price upgrade without extending expiry', async () => {
    await open()
    expect(wrapper.text()).toContain('升级 SVIP · 补 ¥10')
    expect(wrapper.text()).toContain('不增加 30 天')
    expect(wrapper.text()).not.toContain('开通 SVIP · ¥19.9')
    expect(membershipAPI.checkout).not.toHaveBeenCalled()
  })
  it('only offers same-tier renewal to an active SVIP', async () => {
    vi.mocked(membershipAPI.get).mockResolvedValue({ ...base, tier: 'svip' })
    await open()
    expect(wrapper.text()).toContain('续费 SVIP · ¥19.9')
    expect(wrapper.text()).not.toContain('续费 VIP · ¥9.9')
    expect(wrapper.text()).not.toContain('升级 SVIP · 补 ¥10')
  })
  it('allows an expired SVIP to choose VIP or SVIP again', async () => {
    vi.mocked(membershipAPI.get).mockResolvedValue({ ...base, tier: 'svip', active: false })
    await open()
    expect(wrapper.text()).toContain('开通 VIP · ¥9.9')
    expect(wrapper.text()).toContain('开通 SVIP · ¥19.9')
    expect(wrapper.text()).toContain('仅暂停该用户的 API 调用')
  })
  it('disables checkout until collection is enabled', async () => {
    vi.mocked(membershipAPI.get).mockResolvedValue({ ...base, payment_enabled: false })
    await open()
    expect(wrapper.findAll('button').find(button => button.text() === '续费 VIP · ¥9.9')!.attributes('disabled')).toBeDefined()
  })
  it('sends the selected product and never supplies a client price', async () => {
    vi.mocked(membershipAPI.checkout).mockRejectedValue(new Error('渠道暂不可用'))
    await open()
    wrapper.findAllComponents(NPopconfirm).find(component => component.text().includes('升级 SVIP · 补 ¥10'))!.vm.$emit('positive-click')
    await flushPromises()
    expect(membershipAPI.checkout).toHaveBeenCalledWith('upgrade', 'alipay')
    expect(wrapper.text()).toContain('渠道暂不可用')
  })
  it('shows paid-but-unfulfilled orders without offering another payment', async () => {
    vi.mocked(membershipAPI.orders).mockResolvedValue([order])
    await open()
    expect(wrapper.text()).toContain('已到账，待人工处理')
    expect(wrapper.text()).toContain('请勿重复付款')
  })
  it('uses administrator APIs and requires a reason before applying limits', async () => {
    vi.mocked(membershipAPI.adjust).mockResolvedValue({ updated: true })
    await open('user')
    expect(membershipAPI.get).toHaveBeenCalledWith('user')
    expect(wrapper.text()).not.toContain('升级 SVIP · 补 ¥10')
    expect(wrapper.findAll('button').find(button => button.text() === '保存权益调整')!.attributes('disabled')).toBeDefined()
    wrapper.getComponent(NSelect).vm.$emit('update:value', 'svip')
    wrapper.getComponent(NInputNumber).vm.$emit('update:value', 5)
    await wrapper.get('input[placeholder="必填：赠送、补偿或调整原因"]').setValue('存量房主额度调整')
    wrapper.findAllComponents(NPopconfirm).find(component => component.text().includes('保存权益调整'))!.vm.$emit('positive-click')
    await flushPromises()
    expect(membershipAPI.adjust).toHaveBeenCalledWith('user', { tier: 'svip', expires_at: '2099-10-01T00:00:00.000Z', owner_limit_override: 5, revision: 1, reason: '存量房主额度调整', review_order_id: '' })
  })
  it('reports read failures without inventing a free membership', async () => {
    vi.mocked(membershipAPI.get).mockRejectedValue(new Error('读取失败'))
    await open()
    expect(wrapper.text()).toContain('读取失败')
    expect(wrapper.text()).not.toContain('暂无有效会员')
  })
})
