package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/openai"
	"github.com/sharesub/sharesub/backend/internal/security"
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

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var input struct{ Username, Email, Password string }
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := s.app.Register(r.Context(), input.Username, input.Email, input.Password)
	writeResult(w, result, err)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct{ Email, Password string }
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := s.app.Login(r.Context(), input.Email, input.Password)
	writeResult(w, result, err)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	err := s.app.Logout(r.Context(), r.Context().Value(tokenContextKey{}).(string))
	writeResult(w, map[string]bool{"logged_out": err == nil}, err)
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}
func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.UpdateUsername(r.Context(), currentUser(r).ID, input.Username)
	writeResult(w, v, err)
}
func (s *Server) updateAvatar(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBody)
	if err := r.ParseMultipartForm(maxAvatarBody); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeErrorStatus(w, http.StatusRequestEntityTooLarge, "avatar_too_large", "avatar must not exceed 2 MiB")
		} else {
			writeErrorStatus(w, http.StatusBadRequest, "invalid_avatar", "avatar upload must be multipart form data")
		}
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("avatar")
	if err != nil {
		writeErrorStatus(w, http.StatusBadRequest, "invalid_avatar", "avatar file is required")
		return
	}
	defer file.Close()
	if header.Size < 1 || header.Size > application.MaxAvatarBytes {
		writeErrorStatus(w, http.StatusRequestEntityTooLarge, "avatar_too_large", "avatar must not exceed 2 MiB")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, application.MaxAvatarBytes+1))
	if err != nil {
		writeErrorStatus(w, http.StatusBadRequest, "invalid_avatar", "avatar could not be read")
		return
	}
	if len(data) > application.MaxAvatarBytes {
		writeErrorStatus(w, http.StatusRequestEntityTooLarge, "avatar_too_large", "avatar must not exceed 2 MiB")
		return
	}
	v, err := s.app.UpdateUserAvatar(r.Context(), currentUser(r).ID, data)
	writeResult(w, v, err)
}
func (s *Server) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.DeleteUserAvatar(r.Context(), currentUser(r).ID)
	writeResult(w, v, err)
}
func (s *Server) userAvatar(w http.ResponseWriter, r *http.Request) {
	avatar, err := s.app.UserAvatar(r.Context(), r.PathValue("userID"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", avatar.MediaType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(avatar.Data)
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.Dashboard(r.Context(), currentUser(r).ID, r.URL.Query().Get("timezone"))
	writeResult(w, v, err)
}
func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.ListAccounts(r.Context(), currentUser(r).ID)
	writeResult(w, v, err)
}
func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.StartOpenAIConnect(r.Context(), currentUser(r).ID)
	writeResult(w, v, err)
}
func (s *Server) oauthComplete(w http.ResponseWriter, r *http.Request) {
	var input struct {
		State  string                         `json:"state"`
		Code   string                         `json:"code"`
		Config application.AccountConfigInput `json:"config"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.CompleteOpenAIConnect(r.Context(), currentUser(r).ID, input.State, input.Code, input.Config)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidInput) || errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			writeError(w, err)
			return
		}
		s.logger.Error("complete OpenAI OAuth", "error", err)
		writeErrorStatus(w, http.StatusBadGateway, "openai_oauth_failed", "OpenAI OAuth authorization failed")
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) oauthReauthorizeStart(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.StartOpenAIReauthorize(r.Context(), currentUser(r).ID, r.PathValue("accountID"))
	writeResult(w, v, err)
}
func (s *Server) oauthReauthorizeComplete(w http.ResponseWriter, r *http.Request) {
	var input struct {
		State string `json:"state"`
		Code  string `json:"code"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.CompleteOpenAIReauthorize(r.Context(), currentUser(r).ID, r.PathValue("accountID"), input.State, input.Code)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidInput) || errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			writeError(w, err)
			return
		}
		s.logger.Error("reauthorize OpenAI account", "error", err)
		writeErrorStatus(w, http.StatusBadGateway, "openai_oauth_failed", "OpenAI OAuth authorization failed")
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	var input application.AccountConfigInput
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.UpdateAccountConfig(r.Context(), currentUser(r).ID, r.PathValue("accountID"), input)
	writeResult(w, v, err)
}
func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.ListPlans(r.Context(), currentUser(r).ID)
	writeResult(w, v, err)
}
func (s *Server) createPlan(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AccountID             string `json:"account_id"`
		Name                  string `json:"name"`
		AllocationMode        string `json:"allocation_mode"`
		OwnerShareBasisPoints int    `json:"owner_share_basis_points"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.CreatePlan(r.Context(), currentUser(r).ID, input.AccountID, input.Name, input.AllocationMode, input.OwnerShareBasisPoints)
	writeResult(w, v, err)
}
func (s *Server) planDetail(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.PlanDetail(r.Context(), currentUser(r).ID, r.PathValue("planID"))
	writeResult(w, v, err)
}
func (s *Server) renamePlan(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.RenamePlan(r.Context(), currentUser(r).ID, r.PathValue("planID"), input.Name)
	writeResult(w, v, err)
}
func (s *Server) updatePlanStatus(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.UpdatePlanStatus(r.Context(), currentUser(r).ID, r.PathValue("planID"), input.Status)
	writeResult(w, v, err)
}
func (s *Server) deletePlan(w http.ResponseWriter, r *http.Request) {
	err := s.app.DeletePlan(r.Context(), currentUser(r).ID, r.PathValue("planID"))
	writeResult(w, map[string]bool{"deleted": err == nil}, err)
}
func (s *Server) transferPlanOwnership(w http.ResponseWriter, r *http.Request) {
	var input struct {
		MemberID string `json:"member_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.TransferPlanOwnership(r.Context(), currentUser(r).ID, r.PathValue("planID"), input.MemberID)
	writeResult(w, v, err)
}
func (s *Server) rebindPlanAccount(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AccountID string `json:"account_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.RebindPlanAccount(r.Context(), currentUser(r).ID, r.PathValue("planID"), input.AccountID)
	writeResult(w, v, err)
}
func (s *Server) listPlanAuditEvents(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.ListPlanAuditEvents(r.Context(), currentUser(r).ID, r.PathValue("planID"))
	writeResult(w, v, err)
}
func (s *Server) manualQuotaRefresh(w http.ResponseWriter, r *http.Request) {
	ownerID := currentUser(r).ID
	planID := r.PathValue("planID")
	probe, err := s.app.PreparePlanQuotaProbe(r.Context(), ownerID, planID)
	if err != nil {
		writeError(w, err)
		return
	}
	signals, err := s.gateway.ProbeQuota(r.Context(), probe.AccessToken, probe.ChatGPTAccountID, probe.ProxyURL)
	if err != nil {
		s.logger.Error("probe OpenAI quota", "error", err, "plan_id", planID)
		writeErrorStatus(w, http.StatusBadGateway, "quota_probe_failed", "OpenAI quota query failed")
		return
	}
	if err := s.app.RecordManualQuotaSignals(r.Context(), ownerID, planID, signals); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account_id": probe.AccountID, "signals": signals})
}
func (s *Server) listPublicPlans(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.ListPublicPlans(r.Context(), currentUser(r).ID)
	writeResult(w, v, err)
}
func (s *Server) updatePlanPublication(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Visibility             string `json:"visibility"`
		PublicSlots            int    `json:"public_slots"`
		PublicShareBasisPoints int    `json:"public_share_basis_points"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.UpdatePlanPublication(r.Context(), currentUser(r).ID, r.PathValue("planID"), input.Visibility, input.PublicSlots, input.PublicShareBasisPoints)
	writeResult(w, v, err)
}
func (s *Server) createJoinApplication(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Message string `json:"message"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.ApplyToPublicPlan(r.Context(), currentUser(r).ID, r.PathValue("planID"), input.Message)
	writeResult(w, v, err)
}
func (s *Server) reviewJoinApplication(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Decision string `json:"decision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Decision != "approve" && input.Decision != "reject" {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	v, err := s.app.ReviewJoinApplication(r.Context(), currentUser(r).ID, r.PathValue("applicationID"), input.Decision == "approve")
	writeResult(w, v, err)
}
func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ShareBasisPoints int `json:"share_basis_points"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.Invite(r.Context(), currentUser(r).ID, r.PathValue("planID"), input.ShareBasisPoints)
	writeResult(w, v, err)
}
func (s *Server) previewInvite(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.PreviewInvite(r.Context(), input.Token)
	writeResult(w, v, err)
}
func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.AcceptInvite(r.Context(), currentUser(r), input.Token)
	writeResult(w, v, err)
}
func (s *Server) revokeInvite(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.RevokeInvite(r.Context(), currentUser(r).ID, r.PathValue("planID"), r.PathValue("inviteID"))
	writeResult(w, v, err)
}
func (s *Server) updateMember(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ShareBasisPoints int `json:"share_basis_points"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.UpdateMemberShare(r.Context(), currentUser(r).ID, r.PathValue("planID"), r.PathValue("memberID"), input.ShareBasisPoints)
	writeResult(w, v, err)
}
func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	err := s.app.RemovePlanMember(r.Context(), currentUser(r).ID, r.PathValue("planID"), r.PathValue("memberID"))
	writeResult(w, map[string]bool{"removed": err == nil}, err)
}
func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string               `json:"name"`
		Strategy string               `json:"strategy"`
		Routes   []domain.APIKeyRoute `json:"routes"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.CreateAPIKey(r.Context(), currentUser(r).ID, input.Name, input.Strategy, input.Routes)
	writeResult(w, v, err)
}
func (s *Server) updateKey(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string               `json:"name"`
		Strategy string               `json:"strategy"`
		Routes   []domain.APIKeyRoute `json:"routes"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.UpdateAPIKey(r.Context(), currentUser(r).ID, r.PathValue("keyID"), input.Name, input.Strategy, input.Routes)
	writeResult(w, v, err)
}
func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.ListAPIKeys(r.Context(), currentUser(r).ID)
	writeResult(w, v, err)
}
func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	err := s.app.RevokeAPIKey(r.Context(), currentUser(r).ID, r.PathValue("keyID"))
	writeResult(w, map[string]bool{"revoked": err == nil}, err)
}
func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.ListNotifications(r.Context(), currentUser(r).ID)
	writeResult(w, v, err)
}
func (s *Server) updateNotification(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Read bool `json:"read"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.UpdateNotification(r.Context(), currentUser(r).ID, r.PathValue("notificationID"), input.Read)
	writeResult(w, v, err)
}
func (s *Server) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	updated, err := s.app.ReadAllNotifications(r.Context(), currentUser(r).ID)
	writeResult(w, map[string]int64{"updated_count": updated}, err)
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	if err := s.app.AuthenticateGatewayKey(r.Context(), bearerToken(r)); err != nil {
		writeGatewayDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": openai.CodexModels})
}

func (s *Server) responses(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	apiKey := bearerToken(r)
	access, err := s.app.ResolveGatewayAccess(r.Context(), apiKey)
	if err != nil {
		writeGatewayDomainError(w, err)
		return
	}
	defer func() { releaseGatewayAccess(&access) }()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxGatewayBody))
	if err != nil {
		writeGatewayErrorStatus(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds 32 MiB")
		return
	}
	compact := strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/responses/compact")
	forwardBody, billingMetadata, err := openai.PrepareRequest(body, compact)
	if err != nil {
		writeGatewayErrorStatus(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	excludedAccountIDs := make([]string, 0, maxUpstreamAccountSwitches)
	var upstream *http.Response
	for switches := 0; ; {
		upstream, err = s.gateway.Forward(r.Context(), r, forwardBody, billingMetadata, access.AccessToken, access.Credential.Account.ChatGPTAccountID, access.Credential.APIKeyID, access.ProxyURL)
		if err != nil {
			requestID, _ := security.NewID()
			_ = s.app.RecordGatewayMetric(r.Context(), access, requestID, billingMetadata.Model, billingMetadata.ServiceTier, http.StatusBadGateway, 0, time.Since(startedAt), domain.TokenUsage{})
			writeGatewayErrorStatus(w, http.StatusBadGateway, "upstream_unavailable", err.Error())
			return
		}
		if !shouldSwitchUpstreamAccount(upstream.StatusCode) {
			break
		}
		if err := s.app.RecordGatewayAccountQuota(r.Context(), access, upstream.Header); err != nil {
			s.logger.Debug("upstream rejection did not include Codex quota signal", "account_id", access.Credential.Account.ID, "status", upstream.StatusCode)
		}
		if switches >= maxUpstreamAccountSwitches {
			break
		}

		excludedAccountIDs = append(excludedAccountIDs, access.Credential.Account.ID)
		releaseGatewayAccess(&access)
		next, resolveErr := s.app.ResolveGatewayAccess(r.Context(), apiKey, excludedAccountIDs...)
		if resolveErr != nil {
			break
		}
		drainAndCloseResponse(upstream)
		access = next
		switches++
	}
	defer upstream.Body.Close()
	requestID := upstream.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = upstream.Header.Get("Openai-Request-Id")
	}
	if requestID == "" {
		requestID, _ = security.NewID()
	}
	if upstream.StatusCode >= 200 && upstream.StatusCode < 300 {
		if err := s.app.RecordGatewayUsage(r.Context(), access, upstream.Header, requestID); err != nil {
			s.logger.Warn("record Codex quota signal", "request_id", requestID, "account_id", access.Credential.Account.ID, "error", err)
		}
	}
	metrics, copyErr := openai.CopyResponseForRequest(w, upstream, startedAt, billingMetadata.Stream && !compact)
	tokenUsage := domain.TokenUsage{InputTokens: metrics.InputTokens, OutputTokens: metrics.OutputTokens, CachedTokens: metrics.CachedTokens, TotalTokens: metrics.InputTokens + metrics.OutputTokens}
	metricStatus := upstream.StatusCode
	if copyErr != nil {
		metricStatus = http.StatusBadGateway
	}
	if err := s.app.RecordGatewayMetric(r.Context(), access, requestID, billingMetadata.Model, billingMetadata.ServiceTier, metricStatus, metrics.TTFT, metrics.Duration, tokenUsage); err != nil {
		s.logger.Warn("record gateway metric", "error", err)
	}
	if copyErr != nil {
		s.logger.Warn("copy upstream response", "error", copyErr)
	}
}

func shouldSwitchUpstreamAccount(status int) bool {
	return status == http.StatusTooManyRequests || status == 529
}

func releaseGatewayAccess(access *application.GatewayAccess) {
	if access != nil && access.Release != nil {
		access.Release()
		access.Release = nil
	}
}

func drainAndCloseResponse(response *http.Response) {
	if response == nil || response.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	_ = response.Body.Close()
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		user, err := s.app.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey{}, user)
		ctx = context.WithValue(ctx, tokenContextKey{}, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func currentUser(r *http.Request) domain.User {
	return r.Context().Value(userContextKey{}).(domain.User)
}
func bearerToken(r *http.Request) string {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(raw, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeErrorStatus(w, http.StatusBadRequest, "invalid_json", "request body must contain one JSON object")
		return false
	}
	return true
}
func writeResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		writeErrorStatus(w, 401, "unauthorized", err.Error())
	case errors.Is(err, domain.ErrForbidden):
		writeErrorStatus(w, 403, "forbidden", err.Error())
	case errors.Is(err, domain.ErrNotFound):
		writeErrorStatus(w, 404, "not_found", err.Error())
	case errors.Is(err, domain.ErrAccountAlreadyBound):
		writeErrorStatus(w, 409, "account_already_bound", err.Error())
	case errors.Is(err, domain.ErrConflict):
		writeErrorStatus(w, 409, "conflict", err.Error())
	case errors.Is(err, domain.ErrInvalidInput):
		writeErrorStatus(w, 400, "invalid_input", err.Error())
	case errors.Is(err, domain.ErrShareExceeded):
		writeErrorStatus(w, 409, "share_exceeded", err.Error())
	case errors.Is(err, domain.ErrQuotaExhausted):
		writeErrorStatus(w, 429, "quota_exhausted", err.Error())
	case errors.Is(err, domain.ErrAccountUnavailable):
		writeErrorStatus(w, 503, "account_unavailable", err.Error())
	case errors.Is(err, domain.ErrNoRouteAvailable):
		writeErrorStatus(w, 503, "no_route_available", err.Error())
	case errors.Is(err, domain.ErrPublicPlanFull):
		writeErrorStatus(w, 409, "public_plan_full", err.Error())
	case errors.Is(err, domain.ErrAccountConcurrency):
		writeErrorStatus(w, 429, "account_concurrency_limited", err.Error())
	case errors.Is(err, domain.ErrAccountRateLimited):
		writeErrorStatus(w, 429, "account_rate_limited", err.Error())
	default:
		writeErrorStatus(w, 500, "internal_error", "internal server error")
	}
}
func writeErrorStatus(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeGatewayDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		writeGatewayErrorStatus(w, http.StatusUnauthorized, "authentication_error", err.Error())
	case errors.Is(err, domain.ErrQuotaExhausted):
		writeGatewayErrorStatus(w, http.StatusTooManyRequests, "quota_exhausted", err.Error())
	case errors.Is(err, domain.ErrAccountConcurrency):
		writeGatewayErrorStatus(w, http.StatusTooManyRequests, "account_concurrency_limited", err.Error())
	case errors.Is(err, domain.ErrAccountRateLimited):
		writeGatewayErrorStatus(w, http.StatusTooManyRequests, "account_rate_limited", err.Error())
	case errors.Is(err, domain.ErrAccountUnavailable):
		writeGatewayErrorStatus(w, http.StatusServiceUnavailable, "account_unavailable", err.Error())
	case errors.Is(err, domain.ErrNoRouteAvailable):
		writeGatewayErrorStatus(w, http.StatusServiceUnavailable, "no_route_available", err.Error())
	default:
		writeGatewayErrorStatus(w, http.StatusInternalServerError, "server_error", "internal server error")
	}
}

func writeGatewayErrorStatus(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
		"type": code, "code": code, "message": message, "param": nil,
	}})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
	})
}
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("http panic", "value", recovered)
				writeErrorStatus(w, 500, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
