import { describe, expect, it } from 'vitest'
import { isThemeMode, resolveTheme } from './themePreference'

describe('theme preference', () => {
  it('follows the system preference in system mode', () => {
    expect(resolveTheme('system', true)).toBe('dark')
    expect(resolveTheme('system', false)).toBe('light')
  })

  it('keeps an explicit theme regardless of the system preference', () => {
    expect(resolveTheme('light', true)).toBe('light')
    expect(resolveTheme('dark', false)).toBe('dark')
  })

  it('accepts only supported stored values', () => {
    expect(isThemeMode('system')).toBe(true)
    expect(isThemeMode('light')).toBe(true)
    expect(isThemeMode('dark')).toBe(true)
    expect(isThemeMode('auto')).toBe(false)
    expect(isThemeMode(null)).toBe(false)
  })
})
