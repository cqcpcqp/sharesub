import type {
  Account,
  AdminAPIKey,
  AdminAccount,
  AdminOverview,
  AdminPlan,
  AdminUser,
  AccountConfigInput,
  APIError,
  APIKey,
  APIKeyRoute,
  AuditEvent,
  AuthResult,
  CreatedAPIKey,
  CreatedInvite,
  Dashboard,
  InvitePreview,
  JoinApplication,
  Member,
  Notification,
  NotificationList,
  OAuthStart,
  Plan,
  PlanAllocationMode,
  PlanDetail,
  PerformancePeriod,
  PlanPerformance,
  PlanRequestErrorList,
  PublicPlan,
  QuotaRefreshResult,
  QuotaResetCredits,
  PlanQuotaResetResult,
  RouteStrategy,
  UpdatedCount,
  User,
} from './types'

const tokenKey = 'sharesub_session'

export function sessionToken(): string { return localStorage.getItem(tokenKey) ?? '' }
export function setSessionToken(token: string): void { localStorage.setItem(tokenKey, token) }
export function clearSessionToken(): void { localStorage.removeItem(tokenKey) }

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (!(init.body instanceof FormData)) headers.set('Content-Type', 'application/json')
  const token = sessionToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const controller = new AbortController()
  const abortFromCaller = () => controller.abort(init.signal?.reason)
  if (init.signal?.aborted) abortFromCaller()
  else init.signal?.addEventListener('abort', abortFromCaller, { once: true })
  const timeout = setTimeout(() => controller.abort(new DOMException('请求超时', 'TimeoutError')), 120_000)
  try {
    const response = await fetch(path, { ...init, headers, signal: controller.signal })
    const body = await response.json() as T | APIError
    if (!response.ok) {
      const error = (body as APIError).error
      throw new APIRequestError(response.status, error.code, error.message)
    }
    return body as T
  } finally {
    clearTimeout(timeout)
    init.signal?.removeEventListener('abort', abortFromCaller)
  }
}

export class APIRequestError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message)
    this.name = 'APIRequestError'
  }
}

export interface KeyConfigInput {
  name: string
  strategy: RouteStrategy
  routes: APIKeyRoute[]
}

export interface RegistrationAgreementInput {
  accepted: true
  terms_version: string
  privacy_policy_version: string
  acceptable_use_version: string
}

export const api = {
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
  accounts: () => request<Account[]>('/api/accounts'),
  oauthStart: () => request<OAuthStart>('/api/accounts/openai/oauth/start', { method: 'POST' }),
  oauthComplete: (state: string, code: string, config: AccountConfigInput) => request<Account>('/api/accounts/openai/oauth/complete', { method: 'POST', body: JSON.stringify({ state, code, config }) }),
  oauthReauthorizeStart: (id: string) => request<OAuthStart>(`/api/accounts/${id}/oauth/start`, { method: 'POST' }),
  oauthReauthorizeComplete: (id: string, state: string, code: string) => request<Account>(`/api/accounts/${id}/oauth/complete`, { method: 'POST', body: JSON.stringify({ state, code }) }),
  updateAccount: (id: string, config: AccountConfigInput) => request<Account>(`/api/accounts/${id}`, { method: 'PATCH', body: JSON.stringify(config) }),
  plans: () => request<Plan[]>('/api/plans'),
  createPlan: (payload: { account_id: string; name: string; allocation_mode: PlanAllocationMode; owner_share_basis_points: number }) => request<PlanDetail>('/api/plans', { method: 'POST', body: JSON.stringify(payload) }),
  plan: (id: string) => request<PlanDetail>(`/api/plans/${id}?timezone=${encodeURIComponent(Intl.DateTimeFormat().resolvedOptions().timeZone)}`),
  planPerformance: (id: string, period: PerformancePeriod) => request<PlanPerformance>(`/api/plans/${id}/performance?period=${period}&timezone=${encodeURIComponent(Intl.DateTimeFormat().resolvedOptions().timeZone)}`),
  planRequestErrors: (id: string, period: PerformancePeriod, page: number, pageSize: number, signal?: AbortSignal) => request<PlanRequestErrorList>(`/api/plans/${id}/errors?period=${period}&timezone=${encodeURIComponent(Intl.DateTimeFormat().resolvedOptions().timeZone)}&page=${page}&page_size=${pageSize}`, { signal }),
  renamePlan: (id: string, name: string) => request<Plan>(`/api/plans/${id}`, { method: 'PATCH', body: JSON.stringify({ name }) }),
  updatePlanDescription: (id: string, description: string) => request<Plan>(`/api/plans/${id}`, { method: 'PATCH', body: JSON.stringify({ description }) }),
  updatePlanStatus: (id: string, status: 'active' | 'archived') => request<Plan>(`/api/plans/${id}/status`, { method: 'PATCH', body: JSON.stringify({ status }) }),
  deletePlan: (id: string) => request<{ deleted: boolean }>(`/api/plans/${id}`, { method: 'DELETE' }),
  transferPlanOwnership: (id: string, memberID: string) => request<Plan>(`/api/plans/${id}/owner`, { method: 'PATCH', body: JSON.stringify({ member_id: memberID }) }),
  rebindPlanAccount: (id: string, accountID: string) => request<Plan>(`/api/plans/${id}/account`, { method: 'PATCH', body: JSON.stringify({ account_id: accountID }) }),
  planAuditEvents: (id: string) => request<AuditEvent[]>(`/api/plans/${id}/audit-events`),
  refreshPlanQuota: (id: string, automatic = false) => request<QuotaRefreshResult>(`/api/plans/${id}/quota/refresh${automatic ? '?automatic=true' : ''}`, { method: 'POST' }),
  planQuotaResetCredits: (id: string) => request<QuotaResetCredits>(`/api/plans/${id}/quota/reset-credits`),
  resetPlanQuota: (id: string) => request<PlanQuotaResetResult>(`/api/plans/${id}/quota/reset`, { method: 'POST' }),
  publicPlans: () => request<PublicPlan[]>('/api/public-plans'),
  updatePublication: (id: string, payload: { visibility: string; public_slots: number; public_share_basis_points: number }) => request<Plan>(`/api/plans/${id}/publication`, { method: 'PATCH', body: JSON.stringify(payload) }),
  applyToPlan: (id: string, message: string) => request<JoinApplication>(`/api/public-plans/${id}/applications`, { method: 'POST', body: JSON.stringify({ message }) }),
  reviewApplication: (id: string, decision: 'approve' | 'reject') => request<JoinApplication>(`/api/join-applications/${id}`, { method: 'PATCH', body: JSON.stringify({ decision }) }),
  invite: (id: string, share_basis_points: number) => request<CreatedInvite>(`/api/plans/${id}/invites`, { method: 'POST', body: JSON.stringify({ share_basis_points }) }),
  invitePreview: (token: string) => request<InvitePreview>('/api/invites/preview', { method: 'POST', body: JSON.stringify({ token }) }),
  acceptInvite: (token: string) => request<Member>('/api/invites/accept', { method: 'POST', body: JSON.stringify({ token }) }),
  revokeInvite: (planID: string, inviteID: string) => request<CreatedInvite['invite']>(`/api/plans/${planID}/invites/${inviteID}`, { method: 'DELETE' }),
  updateMember: (planId: string, memberId: string, share_basis_points: number) => request<Member>(`/api/plans/${planId}/members/${memberId}`, { method: 'PATCH', body: JSON.stringify({ share_basis_points }) }),
  removeMember: (planID: string, memberID: string) => request<{ removed: boolean }>(`/api/plans/${planID}/members/${memberID}`, { method: 'DELETE' }),
  createKey: (payload: KeyConfigInput) => request<CreatedAPIKey>('/api/keys', { method: 'POST', body: JSON.stringify(payload) }),
  updateKey: (id: string, payload: KeyConfigInput) => request<APIKey>(`/api/keys/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),
  keys: () => request<APIKey[]>('/api/keys'),
  revokeKey: (id: string) => request<{ revoked: boolean }>(`/api/keys/${id}`, { method: 'DELETE' }),
  notifications: () => request<NotificationList>('/api/notifications'),
  markNotificationRead: (id: string) => request<Notification>(`/api/notifications/${id}`, { method: 'PATCH', body: JSON.stringify({ read: true }) }),
  markAllNotificationsRead: () => request<UpdatedCount>('/api/notifications/read-all', { method: 'POST' }),
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

export function parseOAuthCallback(raw: string): { code: string; state: string } {
  const url = new URL(raw)
  return { code: url.searchParams.get('code') ?? '', state: url.searchParams.get('state') ?? '' }
}
