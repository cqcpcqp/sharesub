package domain

import (
	"encoding/json"
	"time"
)

const (
	StatusActive             = "active"
	StatusDisabled           = "disabled"
	StatusRefreshRequired    = "refresh_required"
	StatusArchived           = "archived"
	RoleOwner                = "owner"
	RoleMember               = "member"
	RoleUser                 = "user"
	RoleAdmin                = "admin"
	Window5H                 = "5h"
	Window7D                 = "7d"
	VisibilityPrivate        = "private"
	VisibilityPublic         = "public"
	AllocationFixed          = "fixed"
	AllocationShared         = "shared"
	RoutePriority            = "priority"
	RouteBalanced            = "balanced"
	MaxShareBPS              = 10_000
	PercentMicros            = 1_000_000
	RuntimeStatusHealthy     = "healthy"
	RuntimeStatusWarning     = "warning"
	RuntimeStatusCritical    = "critical"
	RuntimeStatusPending     = "pending"
	RuntimeStatusDisabled    = "disabled"
	RuntimeStatusUnavailable = "unavailable"
)

type User struct {
	ID                 string     `json:"id"`
	Username           string     `json:"username"`
	Email              string     `json:"email"`
	EmailVerifiedAt    *time.Time `json:"email_verified_at"`
	AvatarURL          string     `json:"avatar_url"`
	PasswordHash       string     `json:"-"`
	Status             string     `json:"status"`
	CreatedAt          time.Time  `json:"created_at"`
	IsAdmin            bool       `json:"is_admin"`
	Role               string     `json:"role"`
	MustChangePassword bool       `json:"must_change_password"`
}

type EmailVerificationToken struct {
	ID        string
	UserID    string
	TokenHash []byte
	ExpiresAt time.Time
	CreatedAt time.Time
}

type AgreementAcceptance struct {
	UserID               string    `json:"user_id"`
	TermsVersion         string    `json:"terms_version"`
	PrivacyPolicyVersion string    `json:"privacy_policy_version"`
	AcceptableUseVersion string    `json:"acceptable_use_version"`
	AcceptedAt           time.Time `json:"accepted_at"`
}

type AdminOverview struct {
	UserCount       int64              `json:"user_count"`
	ActiveUserCount int64              `json:"active_user_count"`
	AccountCount    int64              `json:"account_count"`
	ActiveAccounts  int64              `json:"active_accounts"`
	PlanCount       int64              `json:"plan_count"`
	ActivePlans     int64              `json:"active_plans"`
	APIKeyCount     int64              `json:"api_key_count"`
	ActiveAPIKeys   int64              `json:"active_api_keys"`
	Requests24H     int64              `json:"requests_24h"`
	Tokens24H       int64              `json:"tokens_24h"`
	CostMicros24H   int64              `json:"cost_micros_24h"`
	SuccessRate24H  float64            `json:"success_rate_24h"`
	Runtime         AdminRuntimeStatus `json:"runtime"`
}

type AdminRuntimeMetric struct {
	Status       string  `json:"status"`
	UsagePercent float64 `json:"usage_percent"`
}

type AdminRuntimeMemory struct {
	Status       string  `json:"status"`
	UsedBytes    uint64  `json:"used_bytes"`
	TotalBytes   uint64  `json:"total_bytes"`
	UsagePercent float64 `json:"usage_percent"`
}

type AdminRuntimeDatabase struct {
	Status            string `json:"status"`
	OpenConnections   int32  `json:"open_connections"`
	ActiveConnections int32  `json:"active_connections"`
	IdleConnections   int32  `json:"idle_connections"`
	WaitingRequests   int64  `json:"waiting_requests"`
	MaxConnections    int32  `json:"max_connections"`
}

type AdminRuntimeGoroutines struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type AdminRuntimeJob struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	LastRunAt      *time.Time `json:"last_run_at"`
	LastSuccessAt  *time.Time `json:"last_success_at"`
	LastErrorAt    *time.Time `json:"last_error_at"`
	LastError      string     `json:"last_error"`
	LastDurationMS int64      `json:"last_duration_ms"`
	LastResult     string     `json:"last_result"`
}

type AdminRuntimeStatus struct {
	CollectedAt time.Time              `json:"collected_at"`
	CPU         AdminRuntimeMetric     `json:"cpu"`
	Memory      AdminRuntimeMemory     `json:"memory"`
	Database    AdminRuntimeDatabase   `json:"database"`
	Goroutines  AdminRuntimeGoroutines `json:"goroutines"`
	JobsStatus  string                 `json:"jobs_status"`
	Jobs        []AdminRuntimeJob      `json:"jobs"`
}

type AdminUser struct {
	User
	AccountCount int64      `json:"account_count"`
	PlanCount    int64      `json:"plan_count"`
	APIKeyCount  int64      `json:"api_key_count"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

type AdminAccount struct {
	Account
	OwnerUsername string `json:"owner_username"`
	OwnerEmail    string `json:"owner_email"`
	PlanID        string `json:"plan_id"`
	PlanName      string `json:"plan_name"`
}

type AdminPlan struct {
	Plan
	OwnerUsername  string     `json:"owner_username"`
	AccountEmail   string     `json:"account_email"`
	MemberCount    int64      `json:"member_count"`
	Requests24H    int64      `json:"requests_24h"`
	TotalTokens24H int64      `json:"total_tokens_24h"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
}

type AdminAPIKey struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Username   string     `json:"username"`
	Email      string     `json:"email"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	Strategy   string     `json:"strategy"`
	Status     string     `json:"status"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	RouteCount int64      `json:"route_count"`
}

type UserAvatar struct {
	Data      []byte
	MediaType string
}

type Account struct {
	ID                     string           `json:"id"`
	OwnerUserID            string           `json:"owner_user_id"`
	Name                   string           `json:"name"`
	Notes                  string           `json:"notes"`
	Email                  string           `json:"email"`
	ChatGPTAccountID       string           `json:"chatgpt_account_id"`
	PlanType               string           `json:"plan_type"`
	SubscriptionExpiresAt  *time.Time       `json:"subscription_expires_at"`
	AccessTokenCiphertext  []byte           `json:"-"`
	RefreshTokenCiphertext []byte           `json:"-"`
	ProxyURLCiphertext     []byte           `json:"-"`
	ProxyURL               string           `json:"proxy_url"`
	MaxConcurrency         int              `json:"max_concurrency"`
	RPMLimit               int              `json:"rpm_limit"`
	FastPolicy             []FastPolicyRule `json:"fast_policy"`
	CodexFingerprintMode   string           `json:"codex_fingerprint_mode"`
	TokenExpiresAt         time.Time        `json:"token_expires_at"`
	Status                 string           `json:"status"`
	LastError              string           `json:"last_error,omitempty"`
	CreatedAt              time.Time        `json:"created_at"`
}

type FastPolicyRule struct {
	ServiceTier          string   `json:"service_tier"`
	Action               string   `json:"action"`
	UserIDs              []string `json:"user_ids"`
	ErrorMessage         string   `json:"error_message"`
	ModelWhitelist       []string `json:"model_whitelist"`
	FallbackAction       string   `json:"fallback_action"`
	FallbackErrorMessage string   `json:"fallback_error_message"`
}

type Plan struct {
	ID                       string     `json:"id"`
	OwnerUserID              string     `json:"owner_user_id"`
	AccountID                string     `json:"account_id"`
	Name                     string     `json:"name"`
	Description              string     `json:"description"`
	Status                   string     `json:"status"`
	Visibility               string     `json:"visibility"`
	PublicSlots              int        `json:"public_slots"`
	PublicShareBasisPoints   int        `json:"public_share_basis_points"`
	AllocationMode           string     `json:"allocation_mode"`
	CreatedAt                time.Time  `json:"created_at"`
	ArchivedAt               *time.Time `json:"archived_at,omitempty"`
	AccountBindingGeneration int64      `json:"-"`
	AccountBoundAt           *time.Time `json:"-"`
}

type MemberShareAllocation struct {
	MemberID         string `json:"member_id"`
	ShareBasisPoints int    `json:"share_basis_points"`
}

type Member struct {
	ID               string    `json:"id"`
	PlanID           string    `json:"plan_id"`
	UserID           string    `json:"user_id"`
	Username         string    `json:"username"`
	AvatarURL        string    `json:"avatar_url"`
	Email            string    `json:"email,omitempty"`
	Role             string    `json:"role"`
	Status           string    `json:"status"`
	ShareBasisPoints int       `json:"share_basis_points"`
	CreatedAt        time.Time `json:"created_at"`
}

type Invite struct {
	ID               string     `json:"id"`
	PlanID           string     `json:"plan_id"`
	TokenHash        []byte     `json:"-"`
	ShareBasisPoints int        `json:"share_basis_points"`
	Status           string     `json:"status"`
	ExpiresAt        time.Time  `json:"expires_at"`
	AcceptedByUserID *string    `json:"accepted_by_user_id,omitempty"`
	AcceptedAt       *time.Time `json:"accepted_at,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type InvitePreview struct {
	PlanID           string    `json:"plan_id"`
	PlanName         string    `json:"plan_name"`
	OwnerUsername    string    `json:"owner_username"`
	AllocationMode   string    `json:"allocation_mode"`
	ShareBasisPoints int       `json:"share_basis_points"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type APIKey struct {
	ID            string           `json:"id"`
	UserID        string           `json:"user_id"`
	Name          string           `json:"name"`
	Key           string           `json:"key"`
	KeyAvailable  bool             `json:"key_available"`
	KeyPrefix     string           `json:"key_prefix"`
	KeyHash       []byte           `json:"-"`
	KeyCiphertext []byte           `json:"-"`
	Strategy      string           `json:"strategy"`
	FastPolicy    []FastPolicyRule `json:"fast_policy"`
	Status        string           `json:"status"`
	LastUsedAt    *time.Time       `json:"last_used_at,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
	Routes        []APIKeyRoute    `json:"routes"`
}

type APIKeyRoute struct {
	PlanID   string `json:"plan_id"`
	PlanName string `json:"plan_name"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

type JoinApplication struct {
	ID         string     `json:"id"`
	PlanID     string     `json:"plan_id"`
	UserID     string     `json:"user_id"`
	Username   string     `json:"username"`
	AvatarURL  string     `json:"avatar_url"`
	Email      string     `json:"email,omitempty"`
	Message    string     `json:"message"`
	Status     string     `json:"status"`
	MemberID   *string    `json:"member_id,omitempty"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type PublicPlan struct {
	Plan                  Plan       `json:"plan"`
	OwnerUsername         string     `json:"owner_username"`
	OwnerAvatarURL        string     `json:"owner_avatar_url"`
	PlanType              string     `json:"plan_type"`
	SubscriptionExpiresAt *time.Time `json:"subscription_expires_at"`
	MemberCount           int        `json:"member_count"`
	AvailableSlots        int        `json:"available_slots"`
	ApplicationStatus     string     `json:"application_status"`
}

type QuotaWindow struct {
	WindowType        string    `json:"window_type"`
	UsedMicros        int64     `json:"used_micros"`
	AccountUsedMicros int64     `json:"account_used_micros"`
	ResetAt           time.Time `json:"reset_at"`
}

type MemberQuota struct {
	MemberID string        `json:"member_id"`
	Windows  []QuotaWindow `json:"windows"`
}

type PerformanceSummary struct {
	RequestCount      int64   `json:"request_count"`
	SuccessCount      int64   `json:"success_count"`
	AverageTTFTMs     float64 `json:"average_ttft_ms"`
	P95TTFTMs         float64 `json:"p95_ttft_ms"`
	AverageDurationMs float64 `json:"average_duration_ms"`
	P95DurationMs     float64 `json:"p95_duration_ms"`
}

const (
	GatewayErrorSourceRequest  = "request"
	GatewayErrorSourceUpstream = "upstream"
	GatewayErrorSourceGateway  = "gateway"
)

type PlanRequestError struct {
	ID             int64     `json:"id"`
	RequestID      string    `json:"request_id"`
	Endpoint       string    `json:"endpoint"`
	IsStream       bool      `json:"is_stream"`
	StatusCode     int       `json:"status_code"`
	ErrorSource    string    `json:"error_source"`
	ErrorCode      string    `json:"error_code"`
	ErrorMessage   string    `json:"error_message"`
	RequestedModel string    `json:"requested_model"`
	UpstreamModel  string    `json:"upstream_model"`
	ServiceTier    string    `json:"service_tier"`
	DurationMs     int64     `json:"duration_ms"`
	MemberID       string    `json:"member_id"`
	MemberUsername string    `json:"member_username"`
	AccountID      string    `json:"account_id"`
	AccountName    string    `json:"account_name"`
	APIKeyName     string    `json:"api_key_name"`
	APIKeyPrefix   string    `json:"api_key_prefix"`
	CreatedAt      time.Time `json:"created_at"`
}

type PlanRequestErrorList struct {
	Items    []PlanRequestError `json:"items"`
	Total    int64              `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
}

type TokenUsage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	ImageInputTokens    int64 `json:"image_input_tokens"`
	ImageOutputTokens   int64 `json:"image_output_tokens"`
	ImageCount          int64 `json:"image_count"`
	TotalTokens         int64 `json:"total_tokens"`
}

type CostBreakdown struct {
	InputMicros         int64 `json:"input_micros"`
	OutputMicros        int64 `json:"output_micros"`
	CacheCreationMicros int64 `json:"cache_creation_micros"`
	CacheReadMicros     int64 `json:"cache_read_micros"`
	ImageInputMicros    int64 `json:"image_input_micros"`
	ImageOutputMicros   int64 `json:"image_output_micros"`
	WebSearchMicros     int64 `json:"web_search_micros"`
	TotalMicros         int64 `json:"total_micros"`
}

type WindowUsage struct {
	WindowType          string     `json:"window_type"`
	RequestCount        int64      `json:"request_count"`
	TokenUsage          TokenUsage `json:"token_usage"`
	WebSearchCalls      int64      `json:"web_search_calls"`
	EstimatedCostMicros int64      `json:"estimated_cost_micros"`
	WindowStart         time.Time  `json:"window_start"`
	WindowEnd           time.Time  `json:"window_end"`
}

type MemberUsageRank struct {
	MemberID            string     `json:"member_id"`
	Username            string     `json:"username"`
	RequestCount        int64      `json:"request_count"`
	TokenUsage          TokenUsage `json:"token_usage"`
	WebSearchCalls      int64      `json:"web_search_calls"`
	EstimatedCostMicros int64      `json:"estimated_cost_micros"`
}

type MemberRankingPeriod struct {
	Period      string            `json:"period"`
	WindowStart time.Time         `json:"window_start"`
	WindowEnd   time.Time         `json:"window_end"`
	Members     []MemberUsageRank `json:"members"`
}

type DashboardPerformance struct {
	RequestsToday     int64   `json:"requests_today"`
	SuccessRate       float64 `json:"success_rate"`
	RequestsPerMinute int64   `json:"requests_per_minute"`
	TokensPerMinute   int64   `json:"tokens_per_minute"`
	AverageTTFTMs     float64 `json:"average_ttft_ms"`
	AverageDurationMs float64 `json:"average_duration_ms"`
	ActivePlans       int64   `json:"active_plans"`
}

type DashboardTrendPoint struct {
	BucketStart         time.Time `json:"bucket_start"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	CachedTokens        int64     `json:"cached_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	ImageInputTokens    int64     `json:"image_input_tokens"`
	ImageOutputTokens   int64     `json:"image_output_tokens"`
	ImageCount          int64     `json:"image_count"`
	WebSearchCalls      int64     `json:"web_search_calls"`
}

type ModelUsage struct {
	Model               string     `json:"model"`
	RequestCount        int64      `json:"request_count"`
	TokenUsage          TokenUsage `json:"token_usage"`
	WebSearchCalls      int64      `json:"web_search_calls"`
	EstimatedCostMicros int64      `json:"estimated_cost_micros"`
}

type MemberUsageTrend struct {
	MemberID string                `json:"member_id"`
	Username string                `json:"username"`
	Trend    []DashboardTrendPoint `json:"trend"`
}

type PlanPerformance struct {
	PerformanceSummary
	ModelUsage  []ModelUsage          `json:"model_usage"`
	TokenTrend  []DashboardTrendPoint `json:"token_trend"`
	RecentUsage []MemberUsageTrend    `json:"recent_usage"`
}

type Dashboard struct {
	TodayTokens         TokenUsage            `json:"today_tokens"`
	TotalTokens         TokenUsage            `json:"total_tokens"`
	TodayWebSearchCalls int64                 `json:"today_web_search_calls"`
	TotalWebSearchCalls int64                 `json:"total_web_search_calls"`
	Performance         DashboardPerformance  `json:"performance"`
	Trend               []DashboardTrendPoint `json:"trend"`
}

type PlanInsights struct {
	AccountWindows []QuotaWindow         `json:"account_windows"`
	MemberQuotas   []MemberQuota         `json:"member_quotas"`
	Performance    PerformanceSummary    `json:"performance"`
	WindowUsage    []WindowUsage         `json:"window_usage"`
	MemberRanking  []MemberUsageRank     `json:"member_ranking"`
	MemberRankings []MemberRankingPeriod `json:"member_rankings"`
	ModelUsage     []ModelUsage          `json:"model_usage"`
	TokenTrend     []DashboardTrendPoint `json:"token_trend"`
	RecentUsage    []MemberUsageTrend    `json:"recent_usage"`
}

type PlanDetail struct {
	Plan         Plan              `json:"plan"`
	Account      *Account          `json:"account"`
	Members      []Member          `json:"members"`
	Invites      []Invite          `json:"invites"`
	Applications []JoinApplication `json:"applications"`
	Insights     PlanInsights      `json:"insights"`
}

type GatewayCredential struct {
	APIKeyID                 string
	APIKeyStrategy           string
	APIKeyFastPolicy         []FastPolicyRule
	RoutePriority            int
	UsageMicros              int64
	AccountUsageMicros       int64
	Member                   Member
	Plan                     Plan
	Account                  Account
	AccessTokenCiphertext    []byte
	RefreshTokenCiphertext   []byte
	ProxyURLCiphertext       []byte
	TokenExpiresAt           time.Time
	AccountBindingGeneration int64
}

type GatewayRouteSet struct {
	APIKey     APIKey
	Candidates []GatewayCredential
}

type GatewayMetric struct {
	RequestID                string
	APIKeyID                 string
	PlanID                   string
	AccountID                string
	MemberID                 string
	Model                    string
	RequestedModel           string
	UpstreamModel            string
	BillingModel             string
	ServiceTier              string
	Endpoint                 string
	IsStream                 bool
	StatusCode               int
	ErrorSource              string
	ErrorCode                string
	ErrorMessage             string
	TTFT                     time.Duration
	Duration                 time.Duration
	TokenUsage               TokenUsage
	ImageCount               int64
	ImageSize                string
	WebSearchCalls           int64
	CostBreakdown            CostBreakdown
	AccountCostMicros        int64
	CreatedAt                time.Time
	AccountBindingGeneration int64
}

type QuotaSignal struct {
	WindowType        string    `json:"window_type"`
	WindowStart       time.Time `json:"window_start"`
	ResetAt           time.Time `json:"reset_at"`
	AccountUsedMicros int64     `json:"account_used_micros"`
}

type QuotaResetCredit struct {
	ExpiresAt time.Time `json:"expires_at"`
}

type QuotaResetCredits struct {
	AvailableCount int                `json:"available_count"`
	Credits        []QuotaResetCredit `json:"credits"`
	FetchedAt      time.Time          `json:"fetched_at"`
}

type ConsumedQuotaResetCredit struct {
	ID              string `json:"id"`
	ResetType       string `json:"reset_type"`
	Status          string `json:"status"`
	GrantedAt       string `json:"granted_at"`
	ExpiresAt       string `json:"expires_at"`
	RedeemStartedAt string `json:"redeem_started_at"`
	RedeemedAt      string `json:"redeemed_at"`
}

type QuotaResetResult struct {
	Code         string                    `json:"code"`
	Credit       *ConsumedQuotaResetCredit `json:"credit"`
	WindowsReset int                       `json:"windows_reset"`
}

type PlanQuotaResetResult struct {
	Code           string                    `json:"code"`
	Credit         *ConsumedQuotaResetCredit `json:"credit"`
	WindowsReset   int                       `json:"windows_reset"`
	QuotaRefreshed bool                      `json:"quota_refreshed"`
	Signals        []QuotaSignal             `json:"signals"`
}

type PlanQuotaCredential struct {
	PlanID                   string
	AccountID                string
	AccountBindingGeneration int64
	AccountOwnerUserID       string
	ChatGPTAccountID         string
	AccessTokenCiphertext    []byte
	RefreshTokenCiphertext   []byte
	ProxyURLCiphertext       []byte
	TokenExpiresAt           time.Time
}

type AuditEvent struct {
	ID            string          `json:"id"`
	ActorUserID   string          `json:"actor_user_id"`
	ActorUsername string          `json:"actor_username"`
	Action        string          `json:"action"`
	ResourceType  string          `json:"resource_type"`
	ResourceID    string          `json:"resource_id"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedAt     time.Time       `json:"created_at"`
}

type Notification struct {
	ID           string     `json:"id"`
	UserID       string     `json:"user_id"`
	Type         string     `json:"type"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	ResourceType string     `json:"resource_type"`
	ResourceID   string     `json:"resource_id"`
	ReadAt       *time.Time `json:"read_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type NotificationList struct {
	Items       []Notification `json:"items"`
	UnreadCount int64          `json:"unread_count"`
}
