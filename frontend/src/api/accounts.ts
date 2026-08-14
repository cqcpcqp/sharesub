import type { Account, AccountConfigInput, OAuthStart } from '../types'
import { request } from './client'

export const accountAPI = {
  accounts: () => request<Account[]>('/api/accounts'),
  oauthStart: () => request<OAuthStart>('/api/accounts/openai/oauth/start', { method: 'POST' }),
  oauthComplete: (state: string, code: string, config: AccountConfigInput) => request<Account>('/api/accounts/openai/oauth/complete', { method: 'POST', body: JSON.stringify({ state, code, config }) }),
  oauthReauthorizeStart: (id: string) => request<OAuthStart>(`/api/accounts/${id}/oauth/start`, { method: 'POST' }),
  oauthReauthorizeComplete: (id: string, state: string, code: string) => request<Account>(`/api/accounts/${id}/oauth/complete`, { method: 'POST', body: JSON.stringify({ state, code }) }),
  updateAccount: (id: string, config: AccountConfigInput) => request<Account>(`/api/accounts/${id}`, { method: 'PATCH', body: JSON.stringify(config) }),
}

export function parseOAuthCallback(raw: string): { code: string; state: string } {
  const url = new URL(raw)
  return { code: url.searchParams.get('code') ?? '', state: url.searchParams.get('state') ?? '' }
}
