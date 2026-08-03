export const CODEX_MODEL = 'gpt-5.4'

export interface KeyConfigFile {
  path: string
  content: string
}

export function gatewayBaseURL(origin: string): string {
  return `${origin.replace(/\/+$/, '')}/v1`
}

export function codexConfigFiles(baseURL: string, apiKey: string, platform: 'unix' | 'windows'): KeyConfigFile[] {
  const configDirectory = platform === 'windows' ? '%USERPROFILE%\\.codex' : '~/.codex'
  return [
    {
      path: `${configDirectory}/config.toml`,
      content: `model_provider = "OpenAI"
model = "${CODEX_MODEL}"
review_model = "${CODEX_MODEL}"
model_reasoning_effort = "xhigh"
disable_response_storage = true
network_access = "enabled"
windows_wsl_setup_acknowledged = true

[model_providers.OpenAI]
name = "ShareSub"
base_url = "${baseURL}"
wire_api = "responses"
requires_openai_auth = true

[features]
goals = true`,
    },
    {
      path: `${configDirectory}/auth.json`,
      content: JSON.stringify({ OPENAI_API_KEY: apiKey }, null, 2),
    },
  ]
}

export function openCodeConfig(baseURL: string, apiKey: string): KeyConfigFile {
  return {
    path: 'opencode.json',
    content: JSON.stringify({
      provider: {
        openai: {
          options: { baseURL, apiKey },
          models: {
            [CODEX_MODEL]: {
              name: 'GPT-5.4',
              options: { store: false },
              variants: { low: {}, medium: {}, high: {}, xhigh: {} },
            },
          },
        },
      },
      agent: {
        build: { options: { store: false } },
        plan: { options: { store: false } },
      },
      $schema: 'https://opencode.ai/config.json',
    }, null, 2),
  }
}

export function buildCCSwitchImportDeepLink(input: {
  homepage: string
  endpoint: string
  apiKey: string
  providerName: string
}): string {
  const params = new URLSearchParams([
    ['resource', 'provider'],
    ['app', 'codex'],
    ['model', CODEX_MODEL],
    ['name', input.providerName],
    ['homepage', input.homepage],
    ['endpoint', input.endpoint],
    ['apiKey', input.apiKey],
    ['configFormat', 'json'],
    ['usageEnabled', 'false'],
  ])
  return `ccswitch://v1/import?${params.toString()}`
}

export function openCCSwitchImport(deepLink: string): boolean {
  try {
    return window.open(deepLink, '_self') !== null
  } catch {
    return false
  }
}
