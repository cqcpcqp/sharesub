import type { PublicPageID } from './appRoutes'

export const SITE_ORIGIN = 'https://share.underelay.com'
export const HOME_TITLE = 'Codex 拼车与团队协作平台｜ShareSub'
export const HOME_DESCRIPTION = 'ShareSub 是面向 Codex 拼车与小团队协作的平台，支持独立 API Key、成员管理、额度分配和用量查看。仅可接入自己拥有或已获授权使用的 OpenAI Codex 账号。'
export const SOCIAL_IMAGE_URL = `${SITE_ORIGIN}/brand/sharesub-social-card.png`

interface PageSEO {
  title: string
  description: string
  path?: string
  indexable: boolean
  structuredData?: object
}

const homeStructuredData = {
  '@context': 'https://schema.org',
  '@graph': [
    {
      '@type': 'Organization',
      '@id': `${SITE_ORIGIN}/#organization`,
      name: 'ShareSub',
      url: `${SITE_ORIGIN}/`,
      logo: `${SITE_ORIGIN}/brand/sharesub-mark.svg`,
    },
    {
      '@type': 'WebSite',
      '@id': `${SITE_ORIGIN}/#website`,
      url: `${SITE_ORIGIN}/`,
      name: 'ShareSub',
      description: HOME_DESCRIPTION,
      inLanguage: 'zh-CN',
      publisher: { '@id': `${SITE_ORIGIN}/#organization` },
    },
    {
      '@type': 'WebApplication',
      '@id': `${SITE_ORIGIN}/#webapp`,
      name: 'ShareSub',
      url: `${SITE_ORIGIN}/`,
      description: HOME_DESCRIPTION,
      applicationCategory: 'DeveloperApplication',
      operatingSystem: 'Web',
      inLanguage: 'zh-CN',
      featureList: [
        'Codex 拼车与小团队协作',
        '成员独立 API Key',
        '五小时和七天额度窗口管理',
        '成员用量与性能查看',
        '公开招募、私密邀请和申请审批',
      ],
      publisher: { '@id': `${SITE_ORIGIN}/#organization` },
    },
    {
      '@type': 'FAQPage',
      '@id': `${SITE_ORIGIN}/#faq`,
      mainEntity: [
        {
          '@type': 'Question',
          name: 'ShareSub 是 OpenAI 官方产品吗？',
          acceptedAnswer: {
            '@type': 'Answer',
            text: '不是。ShareSub 是独立开发的共享管理平台，与 OpenAI 无隶属、授权或代理关系。',
          },
        },
        {
          '@type': 'Question',
          name: '平台会保存我的对话内容吗？',
          acceptedAnswer: {
            '@type': 'Answer',
            text: '不会。系统记录账号额度状态、成员估算用量与性能指标，但不把请求或响应正文写入数据库或性能记录。',
          },
        },
        {
          '@type': 'Question',
          name: '任何 OpenAI 账号都可以接入吗？',
          acceptedAnswer: {
            '@type': 'Answer',
            text: '你只能接入自己拥有或已获得明确授权使用的账号，并需要遵守 OpenAI 条款及 ShareSub 使用规范。',
          },
        },
      ],
    },
  ],
}

export const PUBLIC_PAGE_SEO: Record<PublicPageID, PageSEO> = {
  home: {
    title: HOME_TITLE,
    description: HOME_DESCRIPTION,
    path: '/',
    indexable: true,
    structuredData: homeStructuredData,
  },
  terms: {
    title: '用户协议｜ShareSub',
    description: '阅读 ShareSub 用户协议，了解使用 Codex 共享协作服务时适用的账户、内容、安全与责任规则。',
    path: '/terms',
    indexable: true,
  },
  privacy: {
    title: '隐私政策｜ShareSub',
    description: '阅读 ShareSub 隐私政策，了解平台如何处理账户信息、凭据、用量指标与服务日志。',
    path: '/privacy',
    indexable: true,
  },
  'acceptable-use': {
    title: '可接受使用规范｜ShareSub',
    description: '阅读 ShareSub 可接受使用规范，了解 Codex 共享协作服务的使用边界与禁止行为。',
    path: '/acceptable-use',
    indexable: true,
  },
}

function upsertMeta(attribute: 'name' | 'property', key: string, content: string) {
  let element = document.head.querySelector<HTMLMetaElement>(`meta[${attribute}="${key}"]`)
  if (!element) {
    element = document.createElement('meta')
    element.setAttribute(attribute, key)
    document.head.append(element)
  }
  element.content = content
}

function setCanonical(path?: string) {
  const existing = document.head.querySelector<HTMLLinkElement>('link[rel="canonical"]')
  if (!path) {
    existing?.remove()
    return
  }
  const element = existing ?? document.createElement('link')
  element.rel = 'canonical'
  element.href = new URL(path, SITE_ORIGIN).href
  if (!existing) document.head.append(element)
}

function setStructuredData(value?: object) {
  const existing = document.head.querySelector<HTMLScriptElement>('#seo-structured-data')
  if (!value) {
    existing?.remove()
    return
  }
  const element = existing ?? document.createElement('script')
  element.id = 'seo-structured-data'
  element.type = 'application/ld+json'
  element.textContent = JSON.stringify(value)
  if (!existing) document.head.append(element)
}

export function applyPageSEO(page: PageSEO) {
  const canonicalURL = page.path ? new URL(page.path, SITE_ORIGIN).href : SITE_ORIGIN
  document.title = page.title
  upsertMeta('name', 'description', page.description)
  upsertMeta('name', 'robots', page.indexable ? 'index, follow, max-image-preview:large, max-snippet:-1, max-video-preview:-1' : 'noindex, nofollow, noarchive')
  upsertMeta('property', 'og:title', page.title)
  upsertMeta('property', 'og:description', page.description)
  upsertMeta('property', 'og:type', 'website')
  upsertMeta('property', 'og:url', canonicalURL)
  upsertMeta('property', 'og:site_name', 'ShareSub')
  upsertMeta('property', 'og:locale', 'zh_CN')
  upsertMeta('property', 'og:image', SOCIAL_IMAGE_URL)
  upsertMeta('property', 'og:image:width', '1200')
  upsertMeta('property', 'og:image:height', '630')
  upsertMeta('property', 'og:image:alt', 'ShareSub — Codex 拼车与团队协作平台')
  upsertMeta('name', 'twitter:card', 'summary_large_image')
  upsertMeta('name', 'twitter:title', page.title)
  upsertMeta('name', 'twitter:description', page.description)
  upsertMeta('name', 'twitter:image', SOCIAL_IMAGE_URL)
  upsertMeta('name', 'twitter:image:alt', 'ShareSub — Codex 拼车与团队协作平台')
  setCanonical(page.indexable ? page.path : undefined)
  setStructuredData(page.structuredData)
}

export function applyPublicPageSEO(page: PublicPageID) {
  applyPageSEO(PUBLIC_PAGE_SEO[page])
}

export function applyPrivatePageSEO(title: string, description = '登录 ShareSub，管理你的 Codex 共享协作。') {
  applyPageSEO({ title, description, indexable: false })
}
