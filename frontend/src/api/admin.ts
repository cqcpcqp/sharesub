import type { AccountConfigInput, AdminAPIKey, AdminAccount, AdminOverview, AdminPlan, AdminUser, Plan, User } from '../types'
import { request } from './client'

export const adminAPI = {
  adminOverview: () => request<AdminOverview>('/api/admin/overview'),
  adminUsers: () => request<AdminUser[]>('/api/admin/users'),
  adminUpdateUserStatus: (id: string, status: 'active' | 'disabled') => request<User>(`/api/admin/users/${id}/status`, { method: 'PATCH', body: JSON.stringify({ status }) }),
  adminAccounts: () => request<AdminAccount[]>('/api/admin/accounts'),
  adminUpdateAccount: (id: string, config: AccountConfigInput) => request<AdminAccount>(`/api/admin/accounts/${id}`, { method: 'PATCH', body: JSON.stringify(config) }),
  adminUpdateAccountStatus: (id: string, status: 'active' | 'disabled') => request<AdminAccount>(`/api/admin/accounts/${id}/status`, { method: 'PATCH', body: JSON.stringify({ status }) }),
  adminPlans: () => request<AdminPlan[]>('/api/admin/plans'),
  adminUpdatePlan: (id: string, payload: { name: string } | { description: string }) => request<Plan>(`/api/admin/plans/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),
  adminUpdatePlanStatus: (id: string, status: 'active' | 'archived') => request<Plan>(`/api/admin/plans/${id}/status`, { method: 'PATCH', body: JSON.stringify({ status }) }),
  adminRebindPlanAccount: (id: string, accountID: string) => request<Plan>(`/api/admin/plans/${id}/account`, { method: 'PATCH', body: JSON.stringify({ account_id: accountID }) }),
  adminUpdatePlanPublication: (id: string, visibility: 'private' | 'public', publicSlots: number, publicShareBasisPoints: number) => request<Plan>(`/api/admin/plans/${id}/publication`, { method: 'PATCH', body: JSON.stringify({ visibility, public_slots: publicSlots, public_share_basis_points: publicShareBasisPoints }) }),
  adminKeys: () => request<AdminAPIKey[]>('/api/admin/keys'),
  adminRevokeKey: (id: string) => request<{ revoked: boolean }>(`/api/admin/keys/${id}`, { method: 'DELETE' }),
}
