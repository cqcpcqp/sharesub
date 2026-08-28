// @vitest-environment happy-dom

import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { beforeEach, describe, expect, it } from 'vitest'
import type { PublicPageID } from './appRoutes'
import { applyPrivatePageSEO, applyPublicPageSEO, HOME_DESCRIPTION, HOME_TITLE, PUBLIC_PAGE_SEO, SITE_ORIGIN, SOCIAL_IMAGE_URL } from './seo'

function staticPageDocument(page: Exclude<PublicPageID, 'home'>) {
  return readFileSync(resolve(process.cwd(), page, 'index.html'), 'utf8')
}

describe('SEO metadata', () => {
  beforeEach(() => {
    document.head.innerHTML = '<meta name="description" content="old"><link rel="canonical" href="https://old.example/">'
  })

  it('applies complete homepage metadata and structured data', () => {
    applyPublicPageSEO('home')

    expect(document.title).toBe(HOME_TITLE)
    expect(document.querySelector('meta[name="description"]')?.getAttribute('content')).toBe(HOME_DESCRIPTION)
    expect(document.querySelector('meta[name="robots"]')?.getAttribute('content')).toContain('index, follow')
    expect(document.querySelector('link[rel="canonical"]')?.getAttribute('href')).toBe(`${SITE_ORIGIN}/`)
    expect(document.querySelector('meta[property="og:image"]')?.getAttribute('content')).toBe(SOCIAL_IMAGE_URL)
    expect(document.querySelector('meta[name="twitter:card"]')?.getAttribute('content')).toBe('summary_large_image')

    const structuredData = JSON.parse(document.querySelector('#seo-structured-data')?.textContent ?? '{}')
    expect(structuredData['@graph'].map((entry: { '@type': string }) => entry['@type'])).toEqual([
      'Organization',
      'WebSite',
      'WebApplication',
      'FAQPage',
    ])
  })

  it('removes homepage canonical and structured data from private pages', () => {
    applyPublicPageSEO('home')
    applyPrivatePageSEO('登录｜ShareSub')

    expect(document.title).toBe('登录｜ShareSub')
    expect(document.querySelector('meta[name="robots"]')?.getAttribute('content')).toBe('noindex, nofollow, noarchive')
    expect(document.querySelector('link[rel="canonical"]')).toBeNull()
    expect(document.querySelector('#seo-structured-data')).toBeNull()
  })

  it('uses a distinct canonical URL for each public legal page', () => {
    applyPublicPageSEO('privacy')

    expect(document.title).toBe('隐私政策｜ShareSub')
    expect(document.querySelector('link[rel="canonical"]')?.getAttribute('href')).toBe(`${SITE_ORIGIN}/privacy`)
    expect(document.querySelector('meta[name="robots"]')?.getAttribute('content')).toContain('index, follow')
  })

  it('ships route-specific metadata in every public legal page HTML entry', () => {
    for (const page of ['terms', 'privacy', 'acceptable-use'] as const) {
      const html = staticPageDocument(page)
      const seo = PUBLIC_PAGE_SEO[page]

      expect(html).toContain(`<title>${seo.title}</title>`)
      expect(html).toContain(`<meta name="description" content="${seo.description}" />`)
      expect(html).toContain(`<meta property="og:url" content="${SITE_ORIGIN}${seo.path}" />`)
      expect(html).toContain(`<link rel="canonical" href="${SITE_ORIGIN}${seo.path}" />`)
      expect(html).not.toContain('seo-fallback')
    }
  })

  it('hides the homepage fallback synchronously on non-home SPA routes', () => {
    const html = readFileSync(resolve(process.cwd(), 'index.html'), 'utf8')

    expect(html).toContain("window.location.pathname.replace(/\\/+$/, '') !== ''")
    expect(html).toContain('.non-home-route .seo-fallback{display:none}')
  })
})
