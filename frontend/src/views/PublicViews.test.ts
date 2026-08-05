// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import LegalDocumentView from './LegalDocumentView.vue'
import PublicHomeView from './PublicHomeView.vue'

describe('public pages', () => {
  it('presents the product without representing demo content as real activity', () => {
    const wrapper = mount(PublicHomeView)
    expect(wrapper.text()).toContain('一起使用，也各自清楚。')
    expect(wrapper.text()).toContain('界面数据为产品功能示意，不代表真实用户或公开 Plan。')
    expect(wrapper.text()).toContain('ShareSub 是独立产品，与 OpenAI 无隶属、授权或代理关系')
    expect(wrapper.find('a[href="/terms"]').exists()).toBe(true)
    expect(wrapper.find('.public-cta .n-button__content').text()).toBe('免费创建账户')
  })

  it.each([
    ['terms', '用户协议'],
    ['privacy', '隐私政策'],
    ['acceptable-use', '可接受使用规范'],
  ] as const)('renders the public %s document', (page, title) => {
    const wrapper = mount(LegalDocumentView, { props: { page } })
    expect(wrapper.text()).toContain(title)
    expect(wrapper.text()).toContain('2026-08-05')
  })
})
