import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const app = readFileSync(new URL('./App.vue', import.meta.url), 'utf8')
const baseStyles = readFileSync(new URL('./style.css', import.meta.url), 'utf8')
const responsiveStyles = readFileSync(new URL('./responsiveStyles.css', import.meta.url), 'utf8')
const indexDocument = readFileSync(new URL('../index.html', import.meta.url), 'utf8')

describe('mobile workspace layout', () => {
  it('keeps the desktop navigation and uses a bounded mobile navigation', () => {
    expect(app).toContain('class="desktop-nav"')
    expect(app).toContain('class="mobile-nav"')
    expect(app).toContain("['dashboard', 'lobby', 'plans', 'keys']")
    expect(app).toContain('mobileSecondaryItems')
    expect(responsiveStyles).toContain('.sidebar .desktop-nav { display: none; }')
    expect(responsiveStyles).toContain('grid-template-columns: repeat(5, minmax(0, 1fr))')
  })

  it('accounts for dynamic mobile viewports and device safe areas', () => {
    expect(baseStyles).toMatch(/body \{[^}]*min-height: 100dvh/)
    expect(baseStyles).toMatch(/\.app-shell \{[^}]*min-height: 100dvh/)
    expect(indexDocument).toContain('viewport-fit=cover')
    expect(responsiveStyles).toContain('env(safe-area-inset-bottom)')
    expect(responsiveStyles).toContain('env(safe-area-inset-top)')
    expect(responsiveStyles).toContain('env(safe-area-inset-left)')
    expect(responsiveStyles).toContain('env(safe-area-inset-right)')
  })

  it('keeps mobile navigation content legible without the desktop active tile', () => {
    expect(responsiveStyles).toMatch(/\.sidebar \{[^}]*background: var\(--sidebar\);/)
    expect(responsiveStyles).toContain('.sidebar nav .n-button.active { background: transparent; color: var(--primary); }')
  })

  it('switches the authentication page before its desktop minimum width can overflow', () => {
    expect(responsiveStyles).toMatch(/@media \(max-width: 820px\) \{[\s\S]*?\.auth-shell \{ display: block;/)
  })
})
