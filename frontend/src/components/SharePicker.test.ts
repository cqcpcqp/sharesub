// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { NPopover, NSlider } from 'naive-ui'
import { describe, expect, it } from 'vitest'
import SharePicker from './SharePicker.vue'

describe('SharePicker', () => {
  it('teleports its panel outside modal clipping contexts', () => {
    const wrapper = mount(SharePicker, { props: { modelValue: 25 } })

    expect(wrapper.getComponent(NPopover).props('to')).toBe('body')
  })

  it('allows selecting a zero-percent view-only share', async () => {
    const wrapper = mount(SharePicker, {
      attachTo: document.body,
      props: { modelValue: 20 },
      global: { stubs: { teleport: true } },
    })

    await wrapper.get('button').trigger('click')
    const slider = wrapper.getComponent(NSlider)
    expect(slider.props('min')).toBe(0)
    const zeroPreset = wrapper.findAll('.share-presets button').find(button => button.text().startsWith('0%'))
    expect(zeroPreset).toBeTruthy()
    await zeroPreset!.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toContainEqual([0])

    wrapper.unmount()
  })

  it('limits the slider and presets to the remaining share', async () => {
    const wrapper = mount(SharePicker, {
      attachTo: document.body,
      props: { modelValue: 10, max: 25 },
      global: { stubs: { teleport: true } },
    })

    await wrapper.get('button').trigger('click')
    expect(wrapper.getComponent(NSlider).props('max')).toBe(25)
    expect(wrapper.findAll('.share-presets button').map(button => button.text())).toEqual([
      '0%仅查看', '10%1/10', '20%1/5', '25%1/4',
    ])

    wrapper.unmount()
  })
})
