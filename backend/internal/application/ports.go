package application

import (
	"context"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type Store interface {
	CreateUser(context.Context, domain.User) error
	UserByEmail(context.Context, string) (domain.User, error)
	UserBySessionHash(context.Context, []byte, time.Time) (domain.User, error)
	UpdateUsername(context.Context, string, string) (domain.User, error)
	UpdateUserAvatar(context.Context, string, domain.UserAvatar, time.Time) (domain.User, error)
	DeleteUserAvatar(context.Context, string) (domain.User, error)
	UpdatePassword(context.Context, string, string, bool, []byte) (domain.User, error)
	EnsureBootstrapAdmin(context.Context, domain.User) (bool, error)
	ResetAdminPassword(context.Context, string, string) (domain.User, error)
	UserAvatar(context.Context, string) (domain.UserAvatar, error)
	CreateSession(context.Context, string, string, []byte, time.Time) error
	DeleteSession(context.Context, []byte) error

	CreateOAuthFlow(context.Context, OAuthFlow) error
	ConsumeOAuthFlow(context.Context, []byte, time.Time) (OAuthFlow, error)
	UpsertAccount(context.Context, domain.Account) (domain.Account, error)
	ListAccounts(context.Context, string) ([]domain.Account, error)
	AccountByID(context.Context, string) (domain.Account, error)
	UpdateAccountConfig(context.Context, string, domain.Account) (domain.Account, error)
	UpdateAccountAuthorization(context.Context, string, domain.Account, domain.AuditEvent) (domain.Account, error)
	UpdateAccountTokens(context.Context, string, []byte, []byte, time.Time) error
	MarkAccountError(context.Context, string, string) error
	Dashboard(context.Context, string, time.Time, time.Time, time.Time) (domain.Dashboard, error)

	CreatePlan(context.Context, domain.Plan, domain.Member, domain.AuditEvent) error
	ListPlans(context.Context, string) ([]domain.Plan, error)
	PlanDetail(context.Context, string, string, time.Time, time.Time) (domain.PlanDetail, error)
	PlanPerformance(context.Context, string, string, time.Time, time.Time, time.Duration) (domain.PlanPerformance, error)
	RenamePlan(context.Context, string, string, string, domain.AuditEvent) (domain.Plan, error)
	UpdatePlanStatus(context.Context, string, string, string, domain.AuditEvent) (domain.Plan, error)
	DeletePlan(context.Context, string, string, domain.AuditEvent) error
	TransferPlanOwnership(context.Context, string, string, string, domain.AuditEvent) (domain.Plan, error)
	RebindPlanAccount(context.Context, string, string, string, domain.AuditEvent) (domain.Plan, error)
	ListPublicPlans(context.Context, string) ([]domain.PublicPlan, error)
	UpdatePlanPublication(context.Context, string, string, string, int, int, domain.AuditEvent) (domain.Plan, error)
	CreateJoinApplication(context.Context, domain.JoinApplication, domain.AuditEvent) (domain.JoinApplication, error)
	ReviewJoinApplication(context.Context, string, string, bool, string, time.Time, domain.AuditEvent) (domain.JoinApplication, error)
	CreateInvite(context.Context, string, string, domain.Invite, domain.AuditEvent) error
	InvitePreview(context.Context, []byte, time.Time) (domain.InvitePreview, error)
	AcceptInvite(context.Context, []byte, domain.User, string, time.Time, domain.AuditEvent) (domain.Member, error)
	RevokeInvite(context.Context, string, string, string, domain.AuditEvent) (domain.Invite, error)
	UpdateMemberShare(context.Context, string, string, string, int, domain.AuditEvent) (domain.Member, error)
	RemovePlanMember(context.Context, string, string, string, domain.AuditEvent) error
	ListPlanAuditEvents(context.Context, string, string) ([]domain.AuditEvent, error)
	PlanQuotaCredential(context.Context, string, string) (domain.PlanQuotaCredential, error)
	PlanQuotaCredentialForMember(context.Context, string, string) (domain.PlanQuotaCredential, error)
	AccountQuotaUpdatedAt(context.Context, string) (time.Time, error)
	CreateAPIKey(context.Context, domain.APIKey, []domain.APIKeyRoute) error
	UpdateAPIKey(context.Context, string, domain.APIKey, []domain.APIKeyRoute) (domain.APIKey, error)
	ListAPIKeys(context.Context, string) ([]domain.APIKey, error)
	RevokeAPIKey(context.Context, string, string) error
	ListNotifications(context.Context, string) (domain.NotificationList, error)
	UpdateNotification(context.Context, string, string, bool, time.Time) (domain.Notification, error)
	ReadAllNotifications(context.Context, string, time.Time) (int64, error)

	AdminOverview(context.Context, time.Time) (domain.AdminOverview, error)
	AdminListUsers(context.Context) ([]domain.AdminUser, error)
	AdminUpdateUserStatus(context.Context, string, string) (domain.User, error)
	AdminListAccounts(context.Context) ([]domain.AdminAccount, error)
	AdminUpdateAccountStatus(context.Context, string, string) (domain.AdminAccount, error)
	AdminListPlans(context.Context, time.Time) ([]domain.AdminPlan, error)
	AdminListAPIKeys(context.Context) ([]domain.AdminAPIKey, error)
	AdminRevokeAPIKey(context.Context, string) error

	ResolveGatewayRoutes(context.Context, []byte, time.Time) (domain.GatewayRouteSet, error)
	TouchAPIKey(context.Context, string, time.Time) error
	MemberQuotaExhausted(context.Context, string, string, int, time.Time) (bool, error)
	AccountQuotaExhausted(context.Context, string, time.Time) (bool, error)
	RecordAccountQuotaSignals(context.Context, string, []domain.QuotaSignal, time.Time) error
	RecordQuotaSignals(context.Context, string, string, []domain.QuotaSignal, string, time.Time) error
	RecordGatewayMetric(context.Context, domain.GatewayMetric) error
}

type OAuthFlow struct {
	ID              string
	UserID          string
	StateHash       []byte
	CodeVerifier    string
	RedirectURI     string
	Purpose         string
	TargetAccountID string
	ExpiresAt       time.Time
}

type OAuthToken struct {
	AccessToken      string
	RefreshToken     string
	IDToken          string
	ExpiresAt        time.Time
	Email            string
	ChatGPTAccountID string
	PlanType         string
}

type OpenAIOAuth interface {
	AuthorizationURL(state, challenge, redirectURI string) string
	Exchange(context.Context, string, string, string) (OAuthToken, error)
	Refresh(context.Context, string) (OAuthToken, error)
}
