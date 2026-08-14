import type { APIKey, APIKeyRoute, CreatedAPIKey, FastPolicyRule, RouteStrategy } from '../types'
import { request } from './client'

export interface KeyConfigInput {
  name: string
  strategy: RouteStrategy
  routes: APIKeyRoute[]
  fast_policy: FastPolicyRule[]
}

export const keyAPI = {
  createKey: (payload: KeyConfigInput) => request<CreatedAPIKey>('/api/keys', { method: 'POST', body: JSON.stringify(payload) }),
  updateKey: (id: string, payload: KeyConfigInput) => request<APIKey>(`/api/keys/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),
  keys: () => request<APIKey[]>('/api/keys'),
  revokeKey: (id: string) => request<{ revoked: boolean }>(`/api/keys/${id}`, { method: 'DELETE' }),
}
