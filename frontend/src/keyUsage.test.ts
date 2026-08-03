// @vitest-environment happy-dom

import { afterEach, describe, expect, it, vi } from 'vitest'
import { CODEX_MODEL, buildCCSwitchImportDeepLink, codexConfigFiles, gatewayBaseURL, openCCSwitchImport, openCodeConfig } from './keyUsage'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('API key usage configuration', () => {
  it('builds the gateway URL without a duplicate slash', () => {
    expect(gatewayBaseURL('https://sharesub.example.com/')).toBe('https://sharesub.example.com/v1')
  })

  it('generates Codex and OpenCode configs with the selected key', () => {
    const files = codexConfigFiles('https://sharesub.example.com/v1', 'sk-sharesub-test', 'windows')
    expect(files[0].path).toBe('%USERPROFILE%\\.codex/config.toml')
    expect(files[0].content).toContain('base_url = "https://sharesub.example.com/v1"')
    expect(files[1].content).toContain('sk-sharesub-test')
    expect(openCodeConfig('https://sharesub.example.com/v1', 'sk-sharesub-test').content).toContain(`"${CODEX_MODEL}"`)
  })

  it('matches the CC Switch provider import protocol', () => {
    const deepLink = buildCCSwitchImportDeepLink({
      homepage: 'https://sharesub.example.com',
      endpoint: 'https://sharesub.example.com/v1',
      apiKey: 'sk-sharesub-test',
      providerName: 'ShareSub',
    })
    const params = new URLSearchParams(deepLink.split('?')[1])
    expect(deepLink.startsWith('ccswitch://v1/import?')).toBe(true)
    expect(params.get('resource')).toBe('provider')
    expect(params.get('app')).toBe('codex')
    expect(params.get('model')).toBe(CODEX_MODEL)
    expect(params.get('endpoint')).toBe('https://sharesub.example.com/v1')
    expect(params.get('apiKey')).toBe('sk-sharesub-test')
  })

  it('opens the CC Switch deep link in the current browsing context', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(window)
    const deepLink = 'ccswitch://v1/import?resource=provider'

    expect(openCCSwitchImport(deepLink)).toBe(true)
    expect(open).toHaveBeenCalledWith(deepLink, '_self')
  })

  it('reports when the browser refuses to open CC Switch', () => {
    vi.spyOn(window, 'open').mockReturnValue(null)

    expect(openCCSwitchImport('ccswitch://v1/import?resource=provider')).toBe(false)
  })
})
