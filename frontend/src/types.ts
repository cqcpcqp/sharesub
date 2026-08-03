export type PlanVisibility = 'private' | 'public'
export type PlanAllocationMode = 'fixed' | 'shared'
export type RouteStrategy = 'priority' | 'balanced'
export type AccountStatus = 'active' | 'disabled' | 'refresh_required'

export interface User {
  id: string
  username: string
  email: string
  avatar_url: string
  status: string
  created_at: string
}

export interface AuthResult { user: User; token: string }

export interface Account {
  id: string
  owner_user_id: string
  name: string
  notes: string
  email: string
  chatgpt_account_id: string
  plan_type: string
  proxy_url: string
  max_concurrency: number
  rpm_limit: number
  token_expires_at: string
  status: AccountStatus
  last_error?: string
  created_at: string
}

export interface AccountConfigInput {
  name: string
  notes: string
  proxy_url: string
  max_concurrency: number
  rpm_limit: number
  status: AccountStatus
}

export interface Plan {
  id: string
  owner_user_id: string
  account_id: string
  name: string
  status: string
  visibility: PlanVisibility
  public_slots: number
  public_share_basis_points: number
  allocation_mode: PlanAllocationMode
  created_at: string
  archived_at?: string
}

export interface Member {
  id: string
  plan_id: string
  user_id: string
  username: string
  avatar_url: string
  email?: string
  role: 'owner' | 'member'
  status: string
  share_basis_points: number
  created_at: string
}

export interface Invite {
  id: string
  plan_id: string
  share_basis_points: number
  status: string
  expires_at: string
  accepted_by_user_id?: string
  accepted_at?: string
  revoked_at?: string
  created_at: string
}

export interface CreatedInvite {
  invite: Invite
  invite_url: string
}

export interface InvitePreview {
  plan_id: string
  plan_name: string
  owner_username: string
  allocation_mode: PlanAllocationMode
  share_basis_points: number
  expires_at: string
}

export interface JoinApplication {
  id: string
  plan_id: string
  user_id: string
  username: string
  avatar_url: string
  email?: string
  message: string
  status: string
  member_id?: string
  reviewed_at?: string
  created_at: string
}

export interface PublicPlan {
  plan: Plan
  owner_username: string
  owner_avatar_url: string
  plan_type: string
  member_count: number
  available_slots: number
  application_status: '' | 'pending' | 'approved' | 'rejected'
}

export interface QuotaWindow {
  window_type: '5h' | '7d'
  used_micros: number
  account_used_micros: number
  reset_at: string
}

export interface MemberQuota { member_id: string; windows: QuotaWindow[] }

export interface PerformanceSummary {
  request_count: number
  success_count: number
  average_ttft_ms: number
  p95_ttft_ms: number
  average_duration_ms: number
  p95_duration_ms: number
}

export interface TokenUsage {
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  total_tokens: number
}

export interface WindowUsage {
  window_type: QuotaWindow['window_type']
  request_count: number
  token_usage: TokenUsage
  estimated_cost_micros: number
  window_start: string
  window_end: string
}

export interface MemberUsageRank {
  member_id: string
  username: string
  request_count: number
  token_usage: TokenUsage
  estimated_cost_micros: number
}

export interface DashboardPerformance {
  requests_today: number
  success_rate: number
  requests_per_minute: number
  tokens_per_minute: number
  average_ttft_ms: number
  average_duration_ms: number
  active_plans: number
}

export interface DashboardTrendPoint {
  bucket_start: string
  input_tokens: number
  output_tokens: number
  cached_tokens: number
}

export interface Dashboard {
  today_tokens: TokenUsage
  total_tokens: TokenUsage
  performance: DashboardPerformance
  trend: DashboardTrendPoint[]
}

export interface PlanInsights {
  account_windows: QuotaWindow[]
  member_quotas: MemberQuota[]
  performance: PerformanceSummary
  window_usage: WindowUsage[]
  member_ranking: MemberUsageRank[]
}

export interface PlanDetail {
  plan: Plan
  account: Account
  members: Member[]
  invites: Invite[]
  applications: JoinApplication[]
  insights: PlanInsights
}

export interface APIKeyRoute {
  plan_id: string
  plan_name: string
  priority: number
  enabled: boolean
}

export interface APIKey {
  id: string
  user_id: string
  name: string
  key: string
  key_available: boolean
  key_prefix: string
  strategy: RouteStrategy
  status: string
  last_used_at?: string
  created_at: string
  routes: APIKeyRoute[]
}

export interface CreatedAPIKey { api_key: APIKey; key: string }
export interface OAuthStart { authorization_url: string; flow_id: string }
export interface QuotaSignal {
  window_type: QuotaWindow['window_type']
  window_start: string
  reset_at: string
  account_used_micros: number
}

export interface QuotaRefreshResult {
  account_id: string
  signals: QuotaSignal[]
}

export interface AuditEvent {
  id: string
  actor_user_id: string
  actor_username: string
  action: string
  resource_type: string
  resource_id: string
  metadata: Record<string, string | number>
  created_at: string
}

export interface Notification {
  id: string
  user_id: string
  type: string
  title: string
  body: string
  resource_type: string
  resource_id: string
  read_at?: string
  created_at: string
}

export interface NotificationList {
  items: Notification[]
  unread_count: number
}

export interface UpdatedCount { updated_count: number }
export interface APIError { error: { code: string; message: string } }
