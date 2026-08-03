// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import AppInput from './AppInput.vue'

describe('AppInput', () => {
  it('keeps its internal input mounted while the parent writes each edit back', async () => {
    const Host = defineComponent({
      setup() {
        const value = ref('')
        return () => h('div', [
          h(AppInput, {
            value: value.value,
            placeholder: '测试输入',
            'onUpdate:value': (nextValue: string) => { value.value = nextValue },
          }),
          h('output', value.value),
          h('button', { onClick: () => { value.value = '外部重置' } }, '重置'),
        ])
      },
    })
    const wrapper = mount(Host, { attachTo: document.body })
    const originalInput = wrapper.get('input').element

    await wrapper.get('input').setValue('本')
    expect(wrapper.get('input').element).toBe(originalInput)
    expect(wrapper.get('output').text()).toBe('本')

    await wrapper.get('input').setValue('本地功能测试')
    expect(wrapper.get('input').element).toBe(originalInput)
    expect(wrapper.get('input').element).toHaveProperty('value', '本地功能测试')
    expect(wrapper.get('output').text()).toBe('本地功能测试')

    await wrapper.get('input').setValue('')
    expect(wrapper.get('input').element).toBe(originalInput)
    expect(wrapper.get('output').text()).toBe('')

    await wrapper.get('button').trigger('click')
    expect(wrapper.get('input').element).toBe(originalInput)
    expect(wrapper.get('input').element).toHaveProperty('value', '外部重置')
  })
})
