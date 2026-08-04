// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UserAvatar from './UserAvatar.vue'

describe('UserAvatar', () => {
  it('renders the uploaded image when an avatar URL is provided', () => {
    const wrapper = mount(UserAvatar, {
      props: { username: 'Alice', src: '/api/users/alice/avatar?v=1' },
    })

    const image = wrapper.get('img')
    expect(image.attributes('src')).toBe('/api/users/alice/avatar?v=1')
    expect(image.attributes('alt')).toBe('Alice的头像')
  })

  it('renders the username initial when no avatar URL is provided', () => {
    const wrapper = mount(UserAvatar, {
      props: { username: 'Alice' },
    })

    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toBe('A')
  })
})
