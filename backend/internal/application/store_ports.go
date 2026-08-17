package application

import (
	"context"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

// Store preserves the existing application dependency while exposing the
// smaller capability boundaries used by individual use cases.
type Store interface {
	IdentityStore
	AccountStore
	PlanQueryStore
	PlanManagementStore
	PlanCollaborationStore
	KeyNotificationStore
	AdminStore
	GatewayStore
}

type IdentityStore interface {
	CreateUser(context.Context, domain.User) error
	CreateUserWithAgreement(context.Context, domain.User, domain.AgreementAcceptance) error
	CreateUserWithEmailVerification(context.Context, domain.User, domain.AgreementAcceptance, domain.EmailVerificationToken) error
	CreateEmailVerificationToken(context.Context, domain.EmailVerificationToken, time.Duration, time.Duration, int) error
	DeleteEmailVerificationToken(context.Context, string, string) error
	SupersedeEmailVerificationTokens(context.Context, string, string, time.Time) error
	ConsumeEmailVerificationToken(context.Context, []byte, time.Time) (domain.User, error)
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
}

type AccountStore interface {
	CreateOAuthFlow(context.Context, OAuthFlow) error
	ConsumeOAuthFlow(context.Context, []byte, time.Time) (OAuthFlow, error)
	CreateOrRotateAccountAuthorization(context.Context, domain.Account, bool) (domain.Account, error)
	ListAccounts(context.Context, string) ([]domain.Account, error)
	AccountByID(context.Context, string) (domain.Account, error)
	UpdateAccountConfig(context.Context, string, domain.Account, domain.AuditEvent) (domain.Account, error)
	UpdateAccountAuthorization(context.Context, string, domain.Account, domain.AuditEvent) (domain.Account, error)
	UpdateAccountTokensIfRefreshTokenUnchanged(context.Context, string, []byte, []byte, []byte, time.Time, *domain.AuditEvent) (bool, error)
	UpdateAccountSubscriptionExpiresAtIfRefreshTokenUnchanged(context.Context, string, []byte, *time.Time) (bool, error)
	MarkAccountErrorIfRefreshTokenUnchanged(context.Context, string, []byte, string) (bool, error)
	ListExpiringAccounts(context.Context, time.Time, int) ([]domain.Account, error)
	TryAcquireAccountRefreshLease(context.Context, string, string, time.Time) (bool, error)
	ReleaseAccountRefreshLease(context.Context, string, string) error
	Dashboard(context.Context, string, time.Time, time.Time, time.Time) (domain.Dashboard, error)
}

type PlanQueryStore interface {
	ListPlans(context.Context, string) ([]domain.Plan, error)
	PlanBinding(context.Context, string, string) (domain.Plan, error)
	PlanDetail(context.Context, string, string, time.Time, time.Time) (domain.PlanDetail, error)
	PlanPerformance(context.Context, string, string, time.Time, time.Time, time.Duration, time.Time) (domain.PlanPerformance, error)
	PlanRequestErrors(context.Context, string, string, time.Time, time.Time, int, int) (domain.PlanRequestErrorList, error)
	ListPublicPlans(context.Context, string) ([]domain.PublicPlan, error)
	ListPlanAuditEvents(context.Context, string, string) ([]domain.AuditEvent, error)
	AccountQuotaUpdatedAt(context.Context, string) (time.Time, error)
}

type PlanManagementStore interface {
	CreatePlan(context.Context, domain.Plan, domain.Member, []domain.QuotaSignal, time.Time, domain.AuditEvent) error
	RenamePlan(context.Context, string, string, string, domain.AuditEvent) (domain.Plan, error)
	UpdatePlanDescription(context.Context, string, string, string, domain.AuditEvent) (domain.Plan, error)
	UpdatePlanStatus(context.Context, string, string, string, domain.AuditEvent) (domain.Plan, error)
	DeletePlan(context.Context, string, string, domain.AuditEvent) error
	TransferPlanOwnership(context.Context, string, string, string, domain.AuditEvent) (domain.Plan, error)
	RebindPlanAccount(context.Context, string, string, string, []domain.QuotaSignal, time.Time, domain.AuditEvent) (domain.Plan, error)
	UpdatePlanPublication(context.Context, string, string, string, int, int, domain.AuditEvent) (domain.Plan, error)
	PlanQuotaCredential(context.Context, string, string) (domain.PlanQuotaCredential, error)
	PlanQuotaCredentialForMember(context.Context, string, string) (domain.PlanQuotaCredential, error)
}

type PlanCollaborationStore interface {
	CreateJoinApplication(context.Context, domain.JoinApplication, domain.AuditEvent) (domain.JoinApplication, error)
	ReviewJoinApplication(context.Context, string, string, string, bool, string, time.Time, domain.AuditEvent) (domain.JoinApplication, error)
	CreateInvite(context.Context, string, string, domain.Invite, domain.AuditEvent) error
	InvitePreview(context.Context, []byte, time.Time) (domain.InvitePreview, error)
	AcceptInvite(context.Context, []byte, domain.User, string, time.Time, domain.AuditEvent) (domain.Member, error)
	RevokeInvite(context.Context, string, string, string, domain.AuditEvent) (domain.Invite, error)
	UpdateMemberShare(context.Context, string, string, string, int, domain.AuditEvent) (domain.Member, error)
	RemovePlanMember(context.Context, string, string, string, domain.AuditEvent) error
}

type KeyNotificationStore interface {
	CreateAPIKey(context.Context, domain.APIKey, []domain.APIKeyRoute) error
	UpdateAPIKey(context.Context, string, domain.APIKey, []domain.APIKeyRoute) (domain.APIKey, error)
	ListAPIKeys(context.Context, string) ([]domain.APIKey, error)
	RevokeAPIKey(context.Context, string, string) error
	ListNotifications(context.Context, string) (domain.NotificationList, error)
	UpdateNotification(context.Context, string, string, bool, time.Time) (domain.Notification, error)
	ReadAllNotifications(context.Context, string, time.Time) (int64, error)
}

type AdminStore interface {
	AdminOverview(context.Context, time.Time) (domain.AdminOverview, error)
	AdminListUsers(context.Context) ([]domain.AdminUser, error)
	AdminUpdateUserStatus(context.Context, string, string) (domain.User, error)
	AdminListAccounts(context.Context) ([]domain.AdminAccount, error)
	AdminUpdateAccountStatus(context.Context, string, string, domain.AuditEvent) (domain.AdminAccount, error)
	RecordAuditEvent(context.Context, domain.AuditEvent) error
	AdminListPlans(context.Context, time.Time) ([]domain.AdminPlan, error)
	AdminPlanByID(context.Context, string) (domain.Plan, error)
	AdminListAPIKeys(context.Context) ([]domain.AdminAPIKey, error)
	AdminRevokeAPIKey(context.Context, string) error
}

type GatewayStore interface {
	ResolveGatewayRoutes(context.Context, []byte, time.Time) (domain.GatewayRouteSet, error)
	TouchAPIKey(context.Context, string, time.Time) error
	MemberQuotaExhausted(context.Context, string, string, string, int64, int, time.Time) (bool, error)
	AccountQuotaExhausted(context.Context, string, time.Time) (bool, error)
	RecordAccountQuotaSignals(context.Context, string, string, int64, []domain.QuotaSignal, time.Time) error
	RecordProbedAccountQuotaSignals(context.Context, string, string, int64, []domain.QuotaSignal, time.Time) error
	RecordQuotaResetSignals(context.Context, string, string, int64, []domain.QuotaSignal, time.Time) error
	RecordGatewayMetric(context.Context, domain.GatewayMetric) error
}
