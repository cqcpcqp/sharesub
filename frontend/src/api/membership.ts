import { request } from './client'

export type MembershipTier = 'none' | 'vip' | 'svip'
export interface Membership {
  user_id: string
  tier: MembershipTier
  expires_at: string | null
  active: boolean
  owner_limit_override: number | null
  owner_limit: number
  owned_plans: number
  revision: number
  source: 'none' | 'rollout' | 'admin' | 'payment'
  billing_started: boolean
  payment_enabled: boolean
}
export interface MembershipOrder {
  id: string
  user_id: string
  product: 'vip' | 'svip' | 'upgrade'
  amount_cents: number
  upgrade_expires_at: string | null
  payment_method: 'alipay' | 'wxpay'
  status: 'pending' | 'expired' | 'paid' | 'review_required'
  trade_no: string | null
  created_at: string
  expires_at: string
  paid_at: string | null
  service_expires_at: string | null
}
export interface MembershipAdjustment {
  tier: MembershipTier
  expires_at: string | null
  owner_limit_override: number | null
  revision: number
  reason: string
  review_order_id: string
}
const root = (userID: string) => userID ? `/api/admin/users/${userID}/membership` : '/api/membership'
export const membershipAPI = {
  get: (userID = '') => request<Membership>(root(userID)),
  orders: (userID = '') => request<MembershipOrder[]>(`${root(userID)}/orders`),
  checkout: (product: MembershipOrder['product'], method: MembershipOrder['payment_method']) => request<{ order: MembershipOrder; pay_url: string }>('/api/membership/orders', { method: 'POST', body: JSON.stringify({ product, payment_method: method }) }),
  verify: (id: string) => request<MembershipOrder>(`/api/membership/orders/${id}/verify`, { method: 'POST' }),
  cancel: (id: string) => request<{ cancelled: boolean }>(`/api/membership/orders/${id}/cancel`, { method: 'POST' }),
  adjust: (userID: string, input: MembershipAdjustment) => request<{ updated: boolean }>(root(userID), { method: 'PATCH', body: JSON.stringify(input) }),
}
