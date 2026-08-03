package domain

import (
	"encoding/json"
	"time"
)

const (
	StatusActive          = "active"
	StatusDisabled        = "disabled"
	StatusRefreshRequired = "refresh_required"
	StatusArchived        = "archived"
	RoleOwner             = "owner"
	RoleMember            = "member"
	Window5H              = "5h"
	Window7D              = "7d"
	VisibilityPrivate     = "private"
	VisibilityPublic      = "public"
	AllocationFixed       = "fixed"
	AllocationShared      = "shared"
	RoutePriority         = "priority"
	RouteBalanced         = "balanced"
	MaxShareBPS           = 10_000
	PercentMicros         = 1_000_000
)

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	AvatarURL    string    `json:"avatar_url"`
	PasswordHash string    `json:"-"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
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
	AccessTokenCiphertext  []byte           `json:"-"`
	RefreshTokenCiphertext []byte           `json:"-"`
	ProxyURLCiphertext     []byte           `json:"-"`
	ProxyURL               string           `json:"proxy_url"`
	MaxConcurrency         int              `json:"max_concurrency"`
	RPMLimit               int              `json:"rpm_limit"`
	FastPolicy             []FastPolicyRule `json:"fast_policy"`
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
	ID                     string     `json:"id"`
	OwnerUserID            string     `json:"owner_user_id"`
	AccountID              string     `json:"account_id"`
	Name                   string     `json:"name"`
	Status                 string     `json:"status"`
	Visibility             string     `json:"visibility"`
	PublicSlots            int        `json:"public_slots"`
	PublicShareBasisPoints int        `json:"public_share_basis_points"`
	AllocationMode         string     `json:"allocation_mode"`
	CreatedAt              time.Time  `json:"created_at"`
	ArchivedAt             *time.Time `json:"archived_at,omitempty"`
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
	ID            string        `json:"id"`
	UserID        string        `json:"user_id"`
	Name          string        `json:"name"`
	Key           string        `json:"key"`
	KeyAvailable  bool          `json:"key_available"`
	KeyPrefix     string        `json:"key_prefix"`
	KeyHash       []byte        `json:"-"`
	KeyCiphertext []byte        `json:"-"`
	Strategy      string        `json:"strategy"`
	Status        string        `json:"status"`
	LastUsedAt    *time.Time    `json:"last_used_at,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	Routes        []APIKeyRoute `json:"routes"`
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
	Plan              Plan   `json:"plan"`
	OwnerUsername     string `json:"owner_username"`
	OwnerAvatarURL    string `json:"owner_avatar_url"`
	PlanType          string `json:"plan_type"`
	MemberCount       int    `json:"member_count"`
	AvailableSlots    int    `json:"available_slots"`
	ApplicationStatus string `json:"application_status"`
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

type TokenUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CachedTokens int64 `json:"cached_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

type WindowUsage struct {
	WindowType          string     `json:"window_type"`
	RequestCount        int64      `json:"request_count"`
	TokenUsage          TokenUsage `json:"token_usage"`
	EstimatedCostMicros int64      `json:"estimated_cost_micros"`
	WindowStart         time.Time  `json:"window_start"`
	WindowEnd           time.Time  `json:"window_end"`
}

type MemberUsageRank struct {
	MemberID            string     `json:"member_id"`
	Username            string     `json:"username"`
	RequestCount        int64      `json:"request_count"`
	TokenUsage          TokenUsage `json:"token_usage"`
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
	BucketStart  time.Time `json:"bucket_start"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CachedTokens int64     `json:"cached_tokens"`
}

type Dashboard struct {
	TodayTokens TokenUsage            `json:"today_tokens"`
	TotalTokens TokenUsage            `json:"total_tokens"`
	Performance DashboardPerformance  `json:"performance"`
	Trend       []DashboardTrendPoint `json:"trend"`
}

type PlanInsights struct {
	AccountWindows []QuotaWindow         `json:"account_windows"`
	MemberQuotas   []MemberQuota         `json:"member_quotas"`
	Performance    PerformanceSummary    `json:"performance"`
	WindowUsage    []WindowUsage         `json:"window_usage"`
	MemberRanking  []MemberUsageRank     `json:"member_ranking"`
	MemberRankings []MemberRankingPeriod `json:"member_rankings"`
}

type PlanDetail struct {
	Plan         Plan              `json:"plan"`
	Account      Account           `json:"account"`
	Members      []Member          `json:"members"`
	Invites      []Invite          `json:"invites"`
	Applications []JoinApplication `json:"applications"`
	Insights     PlanInsights      `json:"insights"`
}

type GatewayCredential struct {
	APIKeyID               string
	APIKeyStrategy         string
	RoutePriority          int
	UsageMicros            int64
	AccountUsageMicros     int64
	Member                 Member
	Plan                   Plan
	Account                Account
	AccessTokenCiphertext  []byte
	RefreshTokenCiphertext []byte
	ProxyURLCiphertext     []byte
	TokenExpiresAt         time.Time
}

type GatewayRouteSet struct {
	APIKey     APIKey
	Candidates []GatewayCredential
}

type GatewayMetric struct {
	RequestID         string
	APIKeyID          string
	PlanID            string
	AccountID         string
	MemberID          string
	Model             string
	ServiceTier       string
	StatusCode        int
	TTFT              time.Duration
	Duration          time.Duration
	TokenUsage        TokenUsage
	AccountCostMicros int64
	CreatedAt         time.Time
}

type QuotaSignal struct {
	WindowType        string    `json:"window_type"`
	WindowStart       time.Time `json:"window_start"`
	ResetAt           time.Time `json:"reset_at"`
	AccountUsedMicros int64     `json:"account_used_micros"`
}

type PlanQuotaCredential struct {
	PlanID                 string
	AccountID              string
	OwnerMemberID          string
	AccountOwnerUserID     string
	ChatGPTAccountID       string
	AccessTokenCiphertext  []byte
	RefreshTokenCiphertext []byte
	ProxyURLCiphertext     []byte
	TokenExpiresAt         time.Time
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
