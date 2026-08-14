import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('public layout', () => {
  it('does not force a horizontal scrollbar at the minimum supported viewport', () => {
    const styles = readFileSync(new URL('./style.css', import.meta.url), 'utf8')
    const htmlRule = styles.match(/(?:^|\n)html \{([^}]+)\}/)?.[1]
    const bodyRule = styles.match(/(?:^|\n)body \{([^}]+)\}/)?.[1]

    expect(htmlRule).toBeDefined()
    expect(bodyRule).toBeDefined()
    expect(htmlRule).not.toContain('min-width')
    expect(bodyRule).not.toContain('min-width')
  })
})
