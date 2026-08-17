import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('modal layout', () => {
  it('keeps scrolling on the modal overlay instead of the dialog surface', () => {
    const styles = readFileSync(new URL('./featureStyles.css', import.meta.url), 'utf8')
    const modalRule = styles.match(/(?:^|\n)\.modal \{([^}]+)\}/)?.[1]

    expect(modalRule).toBeDefined()
    expect(modalRule).not.toContain('max-height')
    expect(modalRule).not.toContain('overflow')
    expect(styles).not.toMatch(/\.n-modal-container \.modal[^{]*\{[^}]*max-height/)
  })

  it('keeps keyboard focus inside modal dialogs', () => {
    const source = readFileSync(new URL('./components/ModalShell.vue', import.meta.url), 'utf8')

    expect(source).toContain('aria-modal="true"')
    expect(source).not.toContain(':trap-focus="false"')
    expect(source).toContain('<div class="modal"')
    expect(source).not.toContain('<section class="modal"')
  })
})
