// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import CustomerSupportPopover from './CustomerSupportPopover.vue'

describe('CustomerSupportPopover', () => {
  it('shows the support QQ group from the toolbar trigger', async () => {
    const wrapper = mount(CustomerSupportPopover, { attachTo: document.body })

    const trigger = wrapper.get('button[aria-label="售后客服"]')
    expect(trigger.text()).toContain('售后客服')

    await trigger.trigger('click')
    expect(document.body.textContent).toContain('QQ 群')
    expect(document.body.textContent).toContain('1095916809')

    wrapper.unmount()
  })
})
