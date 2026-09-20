import type { CodexStateStatus } from '../types'
import { request } from './client'
export const stateAPI = {
  status: (id: string) => request<CodexStateStatus>(`/api/accounts/${id}/state`),
  refresh: (id: string) => request<CodexStateStatus>(`/api/accounts/${id}/state/refresh`, { method: 'POST' }),
}
