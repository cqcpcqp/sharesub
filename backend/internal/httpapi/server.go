package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/buildinfo"
	"github.com/sharesub/sharesub/backend/internal/openai"
)

const maxJSONBody = 1 << 20
const maxGatewayBody = 256 << 20
const maxTextGatewayBody = 32 << 20
const maxAvatarBody = application.MaxAvatarBytes + 64<<10
const maxUpstreamAccountSwitches = 3
const maxRequestScopedCapacityRetries = 3

const gatewayBodyTooLargeMessage = "request body exceeds 256 MiB"
const textGatewayBodyTooLargeMessage = "request body exceeds 32 MiB"

type Server struct {
	app                     *application.Service
	gateway                 *openai.Gateway
	responsesWebSocket      *openai.ResponsesWebSocketSession
	webSocketConfig         ResponsesWebSocketConfig
	webSocketIngress        *responsesWebSocketIngressLimiter
	webSocketSessions       *responsesWebSocketSessionRegistry
	protections             *gatewayProtectionState
	logger                  *slog.Logger
	mux                     *http.ServeMux
	closeOnce               sync.Once
	requestScopedRetryDelay func(int) time.Duration
}

type userContextKey struct{}
type tokenContextKey struct{}

func New(app *application.Service, gateway *openai.Gateway, logger *slog.Logger, webSocketConfig ...ResponsesWebSocketConfig) *Server {
	config := DefaultResponsesWebSocketConfig()
	if len(webSocketConfig) > 0 {
		config = webSocketConfig[0]
	}
	s := &Server{
		app: app, gateway: gateway, logger: logger, mux: http.NewServeMux(),
		webSocketConfig:         config,
		webSocketIngress:        newResponsesWebSocketIngressLimiter(config.MaxConnectionsPerAPIKey),
		webSocketSessions:       newResponsesWebSocketSessionRegistry(),
		protections:             newGatewayProtectionState(config.MaxRequestsPerMinutePerAPIKey),
		requestScopedRetryDelay: requestScopedCapacityDelay,
	}
	s.responsesWebSocket = openai.NewResponsesWebSocketSession(openai.ResponsesWebSocketOptions{
		OutboundProxyURL: config.OutboundProxyURL,
		DialTimeout:      config.DialTimeout, ReadTimeout: config.ReadTimeout,
		WriteTimeout: config.WriteTimeout, InterTurnIdleTimeout: config.InterTurnIdleTimeout,
		UpstreamDrainTimeout: config.UpstreamDrainTimeout, UpstreamReadLimit: config.UpstreamReadLimitBytes,
		ReplayMemoryLimitBytes: config.ReplayMemoryLimitBytes, FirstOutputTimeout: config.FirstOutputTimeout,
	})
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.recoverPanic(s.securityHeaders(s.requestLog(s.mux)))
}

// Shutdown stops accepting Responses WebSocket upgrades, closes active
// sessions, and waits for their handlers to finish. The caller controls the
// maximum wait through ctx.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	defer s.closeResponsesWebSocketTransport()
	if s.webSocketSessions != nil {
		if err := s.webSocketSessions.shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Close immediately stops active Responses WebSocket sessions and releases
// idle transports. It is safe to call more than once.
func (s *Server) Close() {
	if s == nil {
		return
	}
	if s.webSocketSessions != nil {
		s.webSocketSessions.closeNow()
	}
	s.closeResponsesWebSocketTransport()
}

func (s *Server) closeResponsesWebSocketTransport() {
	s.closeOnce.Do(func() {
		if s.responsesWebSocket != nil {
			s.responsesWebSocket.Close()
		}
	})
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, buildinfo.Current())
	})
	s.mux.HandleFunc("POST /api/auth/register", s.register)
	s.mux.HandleFunc("POST /api/auth/email/verify", s.verifyEmail)
	s.mux.HandleFunc("POST /api/auth/email/resend", s.resendEmailVerification)
	s.mux.HandleFunc("POST /api/auth/login", s.login)
	s.mux.Handle("POST /api/auth/logout", s.requireUser(http.HandlerFunc(s.logout)))
	s.mux.Handle("GET /api/me", s.requireUser(http.HandlerFunc(s.me)))
	s.mux.Handle("PATCH /api/me", s.requireUser(http.HandlerFunc(s.updateMe)))
	s.mux.Handle("PATCH /api/me/password", s.requireUser(http.HandlerFunc(s.changePassword)))
	s.mux.Handle("PUT /api/me/avatar", s.requireUser(http.HandlerFunc(s.updateAvatar)))
	s.mux.Handle("DELETE /api/me/avatar", s.requireUser(http.HandlerFunc(s.deleteAvatar)))
	s.mux.HandleFunc("GET /api/users/{userID}/avatar", s.userAvatar)
	s.mux.Handle("GET /api/dashboard", s.requireUser(http.HandlerFunc(s.dashboard)))
	s.mux.Handle("GET /api/accounts", s.requireUser(http.HandlerFunc(s.listAccounts)))
	s.mux.Handle("PATCH /api/accounts/{accountID}", s.requireUser(http.HandlerFunc(s.updateAccount)))
	s.mux.Handle("POST /api/accounts/{accountID}/token/refresh", s.requireUser(http.HandlerFunc(s.manualAccountTokenRefresh)))
	s.mux.Handle("POST /api/accounts/openai/oauth/start", s.requireUser(http.HandlerFunc(s.oauthStart)))
	s.mux.Handle("POST /api/accounts/openai/oauth/complete", s.requireUser(http.HandlerFunc(s.oauthComplete)))
	s.mux.Handle("POST /api/accounts/{accountID}/oauth/start", s.requireUser(http.HandlerFunc(s.oauthReauthorizeStart)))
	s.mux.Handle("POST /api/accounts/{accountID}/oauth/complete", s.requireUser(http.HandlerFunc(s.oauthReauthorizeComplete)))
	s.mux.Handle("GET /api/plans", s.requireUser(http.HandlerFunc(s.listPlans)))
	s.mux.Handle("POST /api/plans", s.requireUser(http.HandlerFunc(s.createPlan)))
	s.mux.Handle("GET /api/plans/{planID}", s.requireUser(http.HandlerFunc(s.planDetail)))
	s.mux.Handle("GET /api/plans/{planID}/performance", s.requireUser(http.HandlerFunc(s.planPerformance)))
	s.mux.Handle("GET /api/plans/{planID}/errors", s.requireUser(http.HandlerFunc(s.planRequestErrors)))
	s.mux.Handle("PATCH /api/plans/{planID}", s.requireUser(http.HandlerFunc(s.updatePlan)))
	s.mux.Handle("PATCH /api/plans/{planID}/status", s.requireUser(http.HandlerFunc(s.updatePlanStatus)))
	s.mux.Handle("DELETE /api/plans/{planID}", s.requireUser(http.HandlerFunc(s.deletePlan)))
	s.mux.Handle("PATCH /api/plans/{planID}/owner", s.requireUser(http.HandlerFunc(s.transferPlanOwnership)))
	s.mux.Handle("PATCH /api/plans/{planID}/account", s.requireUser(http.HandlerFunc(s.rebindPlanAccount)))
	s.mux.Handle("GET /api/plans/{planID}/audit-events", s.requireUser(http.HandlerFunc(s.listPlanAuditEvents)))
	s.mux.Handle("POST /api/plans/{planID}/quota/refresh", s.requireUser(http.HandlerFunc(s.manualQuotaRefresh)))
	s.mux.Handle("GET /api/plans/{planID}/quota/reset-credits", s.requireUser(http.HandlerFunc(s.planQuotaResetCredits)))
	s.mux.Handle("POST /api/plans/{planID}/quota/reset", s.requireUser(http.HandlerFunc(s.resetPlanQuota)))
	s.mux.Handle("PATCH /api/plans/{planID}/publication", s.requireUser(http.HandlerFunc(s.updatePlanPublication)))
	s.mux.Handle("GET /api/public-plans", s.requireUser(http.HandlerFunc(s.listPublicPlans)))
	s.mux.Handle("POST /api/public-plans/{planID}/applications", s.requireUser(http.HandlerFunc(s.createJoinApplication)))
	s.mux.Handle("PATCH /api/join-applications/{applicationID}", s.requireUser(http.HandlerFunc(s.reviewJoinApplication)))
	s.mux.Handle("POST /api/plans/{planID}/invites", s.requireUser(http.HandlerFunc(s.createInvite)))
	s.mux.HandleFunc("POST /api/invites/preview", s.previewInvite)
	s.mux.Handle("POST /api/invites/accept", s.requireUser(http.HandlerFunc(s.acceptInvite)))
	s.mux.Handle("DELETE /api/plans/{planID}/invites/{inviteID}", s.requireUser(http.HandlerFunc(s.revokeInvite)))
	s.mux.Handle("PATCH /api/plans/{planID}/members/{memberID}", s.requireUser(http.HandlerFunc(s.updateMember)))
	s.mux.Handle("DELETE /api/plans/{planID}/members/{memberID}", s.requireUser(http.HandlerFunc(s.removeMember)))
	s.mux.Handle("POST /api/keys", s.requireUser(http.HandlerFunc(s.createKey)))
	s.mux.Handle("PATCH /api/keys/{keyID}", s.requireUser(http.HandlerFunc(s.updateKey)))
	s.mux.Handle("GET /api/keys", s.requireUser(http.HandlerFunc(s.listKeys)))
	s.mux.Handle("DELETE /api/keys/{keyID}", s.requireUser(http.HandlerFunc(s.revokeKey)))
	s.mux.Handle("GET /api/notifications", s.requireUser(http.HandlerFunc(s.listNotifications)))
	s.mux.Handle("PATCH /api/notifications/{notificationID}", s.requireUser(http.HandlerFunc(s.updateNotification)))
	s.mux.Handle("POST /api/notifications/read-all", s.requireUser(http.HandlerFunc(s.readAllNotifications)))
	admin := func(handler http.HandlerFunc) http.Handler { return s.requireUser(s.requireAdmin(handler)) }
	s.mux.Handle("GET /api/admin/overview", admin(s.adminOverview))
	s.mux.Handle("GET /api/admin/users", admin(s.adminListUsers))
	s.mux.Handle("PATCH /api/admin/users/{userID}/status", admin(s.adminUpdateUserStatus))
	s.mux.Handle("GET /api/admin/accounts", admin(s.adminListAccounts))
	s.mux.Handle("GET /api/admin/accounts/{accountID}", admin(s.adminAccount))
	s.mux.Handle("PATCH /api/admin/accounts/{accountID}", admin(s.adminUpdateAccount))
	s.mux.Handle("PATCH /api/admin/accounts/{accountID}/status", admin(s.adminUpdateAccountStatus))
	s.mux.Handle("POST /api/admin/accounts/{accountID}/token/refresh", admin(s.adminManualAccountTokenRefresh))
	s.mux.Handle("POST /api/admin/accounts/{accountID}/oauth/start", admin(s.adminOAuthReauthorizeStart))
	s.mux.Handle("POST /api/admin/accounts/{accountID}/oauth/complete", admin(s.adminOAuthReauthorizeComplete))
	s.mux.Handle("GET /api/admin/plans", admin(s.adminListPlans))
	s.mux.Handle("GET /api/admin/plans/{planID}", admin(s.adminPlanDetail))
	s.mux.Handle("GET /api/admin/plans/{planID}/performance", admin(s.adminPlanPerformance))
	s.mux.Handle("GET /api/admin/plans/{planID}/errors", admin(s.adminPlanRequestErrors))
	s.mux.Handle("GET /api/admin/plans/{planID}/audit-events", admin(s.adminListPlanAuditEvents))
	s.mux.Handle("PATCH /api/admin/plans/{planID}", admin(s.adminUpdatePlan))
	s.mux.Handle("PATCH /api/admin/plans/{planID}/status", admin(s.adminUpdatePlanStatus))
	s.mux.Handle("DELETE /api/admin/plans/{planID}", admin(s.adminDeletePlan))
	s.mux.Handle("PATCH /api/admin/plans/{planID}/owner", admin(s.adminTransferPlanOwnership))
	s.mux.Handle("PATCH /api/admin/plans/{planID}/account", admin(s.adminRebindPlanAccount))
	s.mux.Handle("PATCH /api/admin/plans/{planID}/publication", admin(s.adminUpdatePlanPublication))
	s.mux.Handle("POST /api/admin/plans/{planID}/invites", admin(s.adminCreateInvite))
	s.mux.Handle("DELETE /api/admin/plans/{planID}/invites/{inviteID}", admin(s.adminRevokeInvite))
	s.mux.Handle("PATCH /api/admin/plans/{planID}/applications/{applicationID}", admin(s.adminReviewJoinApplication))
	s.mux.Handle("PATCH /api/admin/plans/{planID}/members/{memberID}", admin(s.adminUpdateMember))
	s.mux.Handle("DELETE /api/admin/plans/{planID}/members/{memberID}", admin(s.adminRemoveMember))
	s.mux.Handle("POST /api/admin/plans/{planID}/quota/refresh", admin(s.adminManualQuotaRefresh))
	s.mux.Handle("GET /api/admin/plans/{planID}/quota/reset-credits", admin(s.adminPlanQuotaResetCredits))
	s.mux.Handle("POST /api/admin/plans/{planID}/quota/reset", admin(s.adminResetPlanQuota))
	s.mux.Handle("GET /api/admin/keys", admin(s.adminListAPIKeys))
	s.mux.Handle("DELETE /api/admin/keys/{keyID}", admin(s.adminRevokeAPIKey))
	s.mux.HandleFunc("GET /v1/models", s.models)
	s.mux.HandleFunc("GET /models", s.models)
	s.mux.HandleFunc("GET /backend-api/codex/models", s.codexModels)
	s.mux.HandleFunc("POST /v1/responses", s.responses)
	s.mux.HandleFunc("POST /v1/responses/compact", s.responses)
	s.mux.HandleFunc("POST /responses", s.responses)
	s.mux.HandleFunc("POST /responses/compact", s.responses)
	s.mux.HandleFunc("POST /backend-api/codex/responses", s.responses)
	s.mux.HandleFunc("POST /backend-api/codex/responses/compact", s.responses)
	s.mux.HandleFunc("GET /v1/responses", s.responsesWebSocketHandler)
	s.mux.HandleFunc("GET /responses", s.responsesWebSocketHandler)
	s.mux.HandleFunc("GET /backend-api/codex/responses", s.responsesWebSocketHandler)
	s.mux.HandleFunc("POST /v1/alpha/search", s.alphaSearch)
	s.mux.HandleFunc("POST /alpha/search", s.alphaSearch)
	s.mux.HandleFunc("POST /backend-api/codex/alpha/search", s.alphaSearch)
	s.mux.HandleFunc("POST /v1/images/generations", s.images)
	s.mux.HandleFunc("POST /v1/images/edits", s.images)
	s.mux.HandleFunc("POST /images/generations", s.images)
	s.mux.HandleFunc("POST /images/edits", s.images)
}
