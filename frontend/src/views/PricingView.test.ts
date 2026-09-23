// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import { api } from '../api'
import type { PricingConfig, PricingVersion, PublishPricingInput } from '../pricing'
import type { User } from '../types'
import PricingView from './PricingView.vue'

afterEach(() => vi.restoreAllMocks())

it('publishes a historical draft against the refreshed catalog while keeping newly added models', async () => {
  const historicalConfig: PricingConfig = {
    models: [{ model: 'gpt-6-astra', standard: { input: 8000000, output: 40000000, cache_read: 800000, cache_write: 10000000, image_input: 8000000, image_output: 40000000 }, long_context_tokens: 272000, long_input_bps: 20000, long_output_bps: 15000 }],
    fast_multiplier_bps: 20000, flex_multiplier_bps: 5000, web_search_micros: 10000, image_1k_micros: 134000, image_2k_micros: 201000, image_4k_micros: 268000,
  }
  const historical: PricingVersion = { id: 1, published_at: '2026-09-01T00:00:00Z', published_by: 'admin', reason: '旧价格', config: historicalConfig }
  const latest: PricingVersion = {
    ...historical, id: 3,
    config: { ...historicalConfig, fast_multiplier_bps: 30000, models: [
      { ...historicalConfig.models[0], standard: { ...historicalConfig.models[0].standard, input: 10000000 } },
      { ...historicalConfig.models[0], model: 'gpt-6-sol', standard: { input: 3000000, output: 12000000, cache_read: 300000, cache_write: 3750000, image_input: 3000000, image_output: 12000000 } },
      { ...historicalConfig.models[0], model: 'gpt-6-luna', standard: { input: 150000, output: 600000, cache_read: 15000, cache_write: 187500, image_input: 150000, image_output: 600000 } },
    ] },
  }
  const pricing = vi.spyOn(api, 'pricing').mockResolvedValueOnce({ ...historical, id: 2 }).mockResolvedValue(latest)
  vi.spyOn(api, 'pricingHistory').mockResolvedValue([historical])
  vi.spyOn(api, 'pricingVersion').mockResolvedValue(historical)
  const publish = vi.spyOn(api, 'publishPricing').mockImplementation(async (input: PublishPricingInput) => ({ ...latest, id: 4, config: input.config }))
  const user: User = { id: 'admin', username: 'admin', email: 'admin@example.com', email_verified_at: '', avatar_url: '', status: 'active', created_at: '', role: 'admin', is_admin: true, must_change_password: false }
  const wrapper = mount(PricingView, {
    props: { user },
    global: { stubs: {
      Button: { props: { disabled: Boolean }, template: '<button :disabled="disabled"><slot /></button>' },
      Modal: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
      Drawer: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
      DrawerContent: { template: '<div><slot /></div>' },
    } },
  })
  const click = async (label: string) => {
    const button = wrapper.findAll('button').find(item => item.text() === label)
    expect(button, label).toBeDefined()
    await button!.trigger('click')
    await flushPromises()
  }
  try {
    await flushPromises()
    await click('版本历史')
    await click('恢复为草稿')
    expect(pricing).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('gpt-6-sol')
    expect(wrapper.text()).toContain('gpt-6-luna')
    await click('预览变更')
    await click('确认发布并生效')
    expect(publish).toHaveBeenCalledTimes(1)
    expect(publish).toHaveBeenCalledWith({
      base_version_id: 3, reason: '恢复版本 1 的价格',
      config: { ...historicalConfig, models: [historicalConfig.models[0], latest.config.models[1], latest.config.models[2]] },
    })
    expect(wrapper.text()).toContain('版本 4 已发布')
  } finally { wrapper.unmount() }
})
