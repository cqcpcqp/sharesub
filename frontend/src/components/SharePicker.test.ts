// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { NPopover } from 'naive-ui'
import { describe, expect, it } from 'vitest'
import SharePicker from './SharePicker.vue'

describe('SharePicker', () => {
  it('teleports its panel outside modal clipping contexts', () => {
    const wrapper = mount(SharePicker, { props: { modelValue: 25 } })

    expect(wrapper.getComponent(NPopover).props('to')).toBe('body')
  })
})
