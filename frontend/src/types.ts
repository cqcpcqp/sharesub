export type PlanVisibility = 'private' | 'public'
export type PlanAllocationMode = 'fixed' | 'shared'
export type RouteStrategy = 'priority' | 'balanced'
export type AccountStatus = 'active' | 'disabled' | 'refresh_required'

export interface User {
  id: string
  username: string
  email: string
  email_verified_at: string | null
  avatar_url: string
  status: string
  created_at: string
  is_admin: boolean
  role: 'user' | 'admin'
  must_change_password: boolean
}

export interface AdminOverview {
  user_count: number
  active_user_count: number
  account_count: number
  active_accounts: number
  plan_count: number
  active_plans: number
  api_key_count: number
  active_api_keys: number
  requests_24h: number
  tokens_24h: number
  cost_micros_24h: number
  success_rate_24h: number
  runtime: AdminRuntimeStatus
}

export type RuntimeStatus = 'healthy' | 'warning' | 'critical' | 'pending' | 'disabled' | 'unavailable'

export interface AdminRuntimeJob {
  id: string
  name: string
  status: RuntimeStatus
  last_run_at: string | null
  last_success_at: string | null
  last_error_at: string | null
  last_error: string
  last_duration_ms: number
  last_result: string
}

export interface AdminRuntimeStatus {
  collected_at: string
  cpu: { status: RuntimeStatus; usage_percent: number }
  memory: { status: RuntimeStatus; used_bytes: number; total_bytes: number; usage_percent: number }
  database: {
    status: RuntimeStatus
    open_connections: number
    active_connections: number
    idle_connections: number
    waiting_requests: number
    max_connections: number
  }
  goroutines: { status: RuntimeStatus; count: number }
  jobs_status: RuntimeStatus
  jobs: AdminRuntimeJob[]
}

export interface AdminUser extends User {
  account_count: number
  plan_count: number
  api_key_count: number
}

export interface AdminAccount extends Account {
  owner_username: string
  owner_email: string
  plan_id: string
  plan_name: string
}

export interface AdminPlan extends Plan {
  owner_username: string
  account_email: string
  member_count: number
  requests_24h: number
  total_tokens_24h: number
}

export interface AdminAPIKey {
  id: string
  user_id: string
  username: string
  email: string
  name: string
  key_prefix: string
  strategy: RouteStrategy
  status: string
  last_used_at?: string
  created_at: string
  route_count: number
}

export interface AuthResult { user: User; token: string }

export interface RegistrationResult {
  email: string
  verification_expires_at: string
  resend_available_at: string
}

export interface EmailVerificationDispatch {
  accepted: boolean
  resend_available_at: string
}

export interface Account {
  id: string
  owner_user_id: string
  name: string
  notes: string
  email: string
  chatgpt_account_id: string
  plan_type: string
  subscription_expires_at: string | null
  proxy_url: string
  max_concurrency: number
  rpm_limit: number
  fast_policy: FastPolicyRule[]
  codex_fingerprint_mode: 'off' | 'device' | 'session' | 'full'
  token_expires_at: string
  status: AccountStatus
  last_error?: string
  created_at: string
}

export type FastPolicyTier = 'all' | 'priority' | 'flex'
export type FastPolicyAction = 'pass' | 'filter' | 'block' | 'force_priority'

export interface FastPolicyRule {
  service_tier: FastPolicyTier
  action: FastPolicyAction
  user_ids: string[]
  error_message: string
  model_whitelist: string[]
  fallback_action: FastPolicyAction
  fallback_error_message: string
}

export interface AccountConfigInput {
  name: string
  notes: string
  proxy_url: string
  max_concurrency: number
  rpm_limit: number
  fast_policy: FastPolicyRule[]
  codex_fingerprint_mode: 'off' | 'device' | 'session' | 'full'
  status: AccountStatus
}

export interface Plan {
  id: string
  owner_user_id: string
  account_id: string
  name: string
  description: string
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

export interface MemberShareAllocation {
  member_id: string
  share_basis_points: number
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
  subscription_expires_at: string | null
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

export type PerformancePeriod = 'today' | '30m' | '6h' | '12h' | '24h'

export interface PerformanceSummary {
  request_count: number
  success_count: number
  average_ttft_ms: number
  p95_ttft_ms: number
  average_duration_ms: number
  p95_duration_ms: number
}

export type GatewayErrorSource = '' | 'request' | 'upstream' | 'gateway'

export interface PlanRequestError {
  id: number
  request_id: string
  endpoint: string
  is_stream: boolean
  status_code: number
  error_source: GatewayErrorSource
  error_code: string
  error_message: string
  requested_model: string
  upstream_model: string
  service_tier: string
  duration_ms: number
  member_id: string
  member_username: string
  account_id: string
  account_name: string
  api_key_name: string
  api_key_prefix: string
  created_at: string
}

export interface PlanRequestErrorList {
  items: PlanRequestError[]
  total: number
  page: number
  page_size: number
}

export interface TokenUsage {
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  cache_creation_tokens: number
  image_input_tokens: number
  image_output_tokens: number
  image_count: number
  total_tokens: number
}

export interface WindowUsage {
  window_type: QuotaWindow['window_type']
  request_count: number
  token_usage: TokenUsage
  web_search_calls: number
  estimated_cost_micros: number
  window_start: string
  window_end: string
}

export interface MemberUsageRank {
  member_id: string
  username: string
  request_count: number
  token_usage: TokenUsage
  web_search_calls: number
  estimated_cost_micros: number
}

export type MemberRankingPeriodID = 'today' | 'last_7_days' | 'account_7d' | 'account_lifecycle'

export interface MemberRankingPeriod {
  period: MemberRankingPeriodID
  window_start: string
  window_end: string
  members: MemberUsageRank[]
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
  cache_creation_tokens: number
  image_input_tokens: number
  image_output_tokens: number
  image_count: number
  web_search_calls: number
}

export interface ModelUsage {
  model: string
  request_count: number
  token_usage: TokenUsage
  web_search_calls: number
  estimated_cost_micros: number
}

export interface MemberUsageTrend {
  member_id: string
  username: string
  trend: DashboardTrendPoint[]
}

export interface PlanPerformance extends PerformanceSummary {
  model_usage: ModelUsage[]
  token_trend: DashboardTrendPoint[]
  recent_usage: MemberUsageTrend[]
}

export interface Dashboard {
  today_tokens: TokenUsage
  total_tokens: TokenUsage
  today_web_search_calls: number
  total_web_search_calls: number
  performance: DashboardPerformance
  trend: DashboardTrendPoint[]
}

export interface PlanInsights {
  account_windows: QuotaWindow[]
  member_quotas: MemberQuota[]
  performance: PerformanceSummary
  window_usage: WindowUsage[]
  member_ranking: MemberUsageRank[]
  member_rankings: MemberRankingPeriod[]
  model_usage: ModelUsage[]
  token_trend: DashboardTrendPoint[]
  recent_usage: MemberUsageTrend[]
}

export interface PlanDetail {
  plan: Plan
  account: Account | null
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
  fast_policy: FastPolicyRule[]
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

export interface QuotaResetCredit {
  expires_at: string
}

export interface QuotaResetCredits {
  available_count: number
  credits: QuotaResetCredit[]
  fetched_at: string
}

export interface ConsumedQuotaResetCredit {
  id: string
  reset_type: string
  status: string
  granted_at: string
  expires_at: string
  redeem_started_at: string
  redeemed_at: string
}

export interface PlanQuotaResetResult {
  code: string
  credit: ConsumedQuotaResetCredit | null
  windows_reset: number
  quota_refreshed: boolean
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
