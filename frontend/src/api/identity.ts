import type { AuthResult, Dashboard, User } from '../types'
import { request } from './client'

export interface RegistrationAgreementInput {
  accepted: true
  terms_version: string
  privacy_policy_version: string
  acceptable_use_version: string
}

export const identityAPI = {
  register: (username: string, email: string, password: string, agreement: RegistrationAgreementInput) => request<AuthResult>('/api/auth/register', { method: 'POST', body: JSON.stringify({ username, email, password, agreement }) }),
  login: (email: string, password: string) => request<AuthResult>('/api/auth/login', { method: 'POST', body: JSON.stringify({ email, password }) }),
  me: () => request<User>('/api/me'),
  updateMe: (username: string) => request<User>('/api/me', { method: 'PATCH', body: JSON.stringify({ username }) }),
  changePassword: (currentPassword: string, newPassword: string) => request<User>('/api/me/password', { method: 'PATCH', body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }) }),
  updateAvatar: (file: File) => {
    const body = new FormData()
    body.append('avatar', file)
    return request<User>('/api/me/avatar', { method: 'PUT', body })
  },
  deleteAvatar: () => request<User>('/api/me/avatar', { method: 'DELETE' }),
  logout: () => request<{ logged_out: boolean }>('/api/auth/logout', { method: 'POST' }),
  dashboard: (timezone: string) => request<Dashboard>(`/api/dashboard?timezone=${encodeURIComponent(timezone)}`),
}
