// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { agreementVersions } from '../agreements'
import LegalDocumentView from './LegalDocumentView.vue'
import PublicHomeView from './PublicHomeView.vue'

describe('public pages', () => {
  it('presents the product without representing demo content as real activity', () => {
    const wrapper = mount(PublicHomeView)
    expect(wrapper.text()).toContain('一起使用，也各自清楚。')
    expect(wrapper.text()).toContain('界面数据为产品功能示意，不代表真实用户或公开 Plan。')
    expect(wrapper.text()).toContain('成员估算额度按同期请求费用分摊')
    expect(wrapper.text()).not.toContain('成员用量实时归属')
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
    const version = page === 'terms' ? agreementVersions.terms : page === 'privacy' ? agreementVersions.privacy : agreementVersions.acceptableUse
    expect(wrapper.text()).toContain(version)
    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.find('aside h2').text()).toBe(title)
  })

  it('discloses Tencent SES as the verification email processor', () => {
    const wrapper = mount(LegalDocumentView, { props: { page: 'privacy' } })
    expect(wrapper.text()).toContain('腾讯云邮件推送（SES）')
    expect(wrapper.text()).toContain('2026 年 8 月 17 日')
  })
})
