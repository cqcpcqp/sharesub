package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/openai"
)

const maxJSONBody = 1 << 20
const maxGatewayBody = 32 << 20
const maxAvatarBody = application.MaxAvatarBytes + 64<<10
const maxUpstreamAccountSwitches = 3

type Server struct {
	app     *application.Service
	gateway *openai.Gateway
	logger  *slog.Logger
	mux     *http.ServeMux
}

type userContextKey struct{}
type tokenContextKey struct{}

func New(app *application.Service, gateway *openai.Gateway, logger *slog.Logger) *Server {
	s := &Server{app: app, gateway: gateway, logger: logger, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.recoverPanic(s.securityHeaders(s.requestLog(s.mux)))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("POST /api/auth/register", s.register)
	s.mux.HandleFunc("POST /api/auth/login", s.login)
	s.mux.Handle("POST /api/auth/logout", s.requireUser(http.HandlerFunc(s.logout)))
	s.mux.Handle("GET /api/me", s.requireUser(http.HandlerFunc(s.me)))
	s.mux.Handle("PATCH /api/me", s.requireUser(http.HandlerFunc(s.updateMe)))
	s.mux.Handle("PUT /api/me/avatar", s.requireUser(http.HandlerFunc(s.updateAvatar)))
	s.mux.Handle("DELETE /api/me/avatar", s.requireUser(http.HandlerFunc(s.deleteAvatar)))
	s.mux.HandleFunc("GET /api/users/{userID}/avatar", s.userAvatar)
	s.mux.Handle("GET /api/dashboard", s.requireUser(http.HandlerFunc(s.dashboard)))
	s.mux.Handle("GET /api/accounts", s.requireUser(http.HandlerFunc(s.listAccounts)))
	s.mux.Handle("PATCH /api/accounts/{accountID}", s.requireUser(http.HandlerFunc(s.updateAccount)))
	s.mux.Handle("POST /api/accounts/openai/oauth/start", s.requireUser(http.HandlerFunc(s.oauthStart)))
	s.mux.Handle("POST /api/accounts/openai/oauth/complete", s.requireUser(http.HandlerFunc(s.oauthComplete)))
	s.mux.Handle("POST /api/accounts/{accountID}/oauth/start", s.requireUser(http.HandlerFunc(s.oauthReauthorizeStart)))
	s.mux.Handle("POST /api/accounts/{accountID}/oauth/complete", s.requireUser(http.HandlerFunc(s.oauthReauthorizeComplete)))
	s.mux.Handle("GET /api/plans", s.requireUser(http.HandlerFunc(s.listPlans)))
	s.mux.Handle("POST /api/plans", s.requireUser(http.HandlerFunc(s.createPlan)))
	s.mux.Handle("GET /api/plans/{planID}", s.requireUser(http.HandlerFunc(s.planDetail)))
	s.mux.Handle("PATCH /api/plans/{planID}", s.requireUser(http.HandlerFunc(s.renamePlan)))
	s.mux.Handle("PATCH /api/plans/{planID}/status", s.requireUser(http.HandlerFunc(s.updatePlanStatus)))
	s.mux.Handle("DELETE /api/plans/{planID}", s.requireUser(http.HandlerFunc(s.deletePlan)))
	s.mux.Handle("PATCH /api/plans/{planID}/owner", s.requireUser(http.HandlerFunc(s.transferPlanOwnership)))
	s.mux.Handle("PATCH /api/plans/{planID}/account", s.requireUser(http.HandlerFunc(s.rebindPlanAccount)))
	s.mux.Handle("GET /api/plans/{planID}/audit-events", s.requireUser(http.HandlerFunc(s.listPlanAuditEvents)))
	s.mux.Handle("POST /api/plans/{planID}/quota/refresh", s.requireUser(http.HandlerFunc(s.manualQuotaRefresh)))
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
	s.mux.HandleFunc("GET /v1/models", s.models)
	s.mux.HandleFunc("POST /v1/responses", s.responses)
	s.mux.HandleFunc("POST /v1/responses/compact", s.responses)
	s.mux.HandleFunc("POST /responses", s.responses)
	s.mux.HandleFunc("POST /responses/compact", s.responses)
	s.mux.HandleFunc("POST /backend-api/codex/responses", s.responses)
	s.mux.HandleFunc("POST /backend-api/codex/responses/compact", s.responses)
}
