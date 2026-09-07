import { request } from './client'

export interface PaymentSettings {
  base_url: string
  pid: string
  enabled: boolean
  revision: number
  started_at: string | null
  key_configured: boolean
  source: 'database' | 'environment'
  public_url: string
  callback_ready: boolean
}

export interface PaymentSettingsInput {
  base_url: string
  pid: string
  key: string
  enabled: boolean
  revision: number
}

export const paymentSettingsAPI = {
  get: () => request<PaymentSettings>('/api/admin/payment-settings'),
  save: (input: PaymentSettingsInput) => request<PaymentSettings>('/api/admin/payment-settings', { method: 'PUT', body: JSON.stringify(input) }),
}
