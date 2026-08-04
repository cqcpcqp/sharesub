package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

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

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.ChangePassword(r.Context(), currentUser(r), input.CurrentPassword, input.NewPassword, r.Context().Value(tokenContextKey{}).(string))
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
	v, err := s.app.PlanDetail(r.Context(), currentUser(r).ID, r.PathValue("planID"), r.URL.Query().Get("timezone"))
	writeResult(w, v, err)
}
func (s *Server) planPerformance(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.PlanPerformance(r.Context(), currentUser(r).ID, r.PathValue("planID"), r.URL.Query().Get("period"), r.URL.Query().Get("timezone"))
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
	userID := currentUser(r).ID
	planID := r.PathValue("planID")
	probe, shouldProbe, err := s.prepareQuotaRefresh(r, userID, planID)
	if err != nil {
		writeError(w, err)
		return
	}
	if !shouldProbe {
		writeJSON(w, http.StatusOK, map[string]any{"account_id": probe.AccountID, "signals": []domain.QuotaSignal{}})
		return
	}
	releaseSlot, ok := s.gateway.TryAcquire()
	if !ok {
		writeErrorStatus(w, http.StatusServiceUnavailable, "server_overloaded", "gateway concurrency limit reached")
		return
	}
	defer releaseSlot()
	signals, err := s.gateway.ProbeQuota(r.Context(), probe.AccessToken, probe.ChatGPTAccountID, probe.ProxyURL)
	if err != nil {
		s.logger.Error("probe OpenAI quota", "error", err, "plan_id", planID)
		writeErrorStatus(w, http.StatusBadGateway, "quota_probe_failed", "OpenAI quota query failed")
		return
	}
	if err := s.recordQuotaRefresh(r, userID, planID, signals); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account_id": probe.AccountID, "signals": signals})
}

func (s *Server) recordQuotaRefresh(r *http.Request, userID, planID string, signals []domain.QuotaSignal) error {
	if r.URL.Query().Get("automatic") == "true" {
		return s.app.RecordAutomaticQuotaSignals(r.Context(), userID, planID, signals)
	}
	return s.app.RecordManualQuotaSignals(r.Context(), userID, planID, signals)
}

func (s *Server) prepareQuotaRefresh(r *http.Request, userID, planID string) (application.PlanQuotaProbe, bool, error) {
	if r.URL.Query().Get("automatic") == "true" {
		return s.app.PrepareAutomaticPlanQuotaProbe(r.Context(), userID, planID)
	}
	probe, err := s.app.PreparePlanQuotaProbe(r.Context(), userID, planID)
	return probe, true, err
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
