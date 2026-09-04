package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminOverview(r.Context(), currentUser(r))
	writeResult(w, v, err)
}

func (s *Server) adminListUsers(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminListUsers(r.Context(), currentUser(r))
	writeResult(w, v, err)
}

func (s *Server) adminUpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.AdminUpdateUserStatus(r.Context(), currentUser(r), r.PathValue("userID"), input.Status)
	writeResult(w, v, err)
}

func (s *Server) adminListAccounts(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminListAccounts(r.Context(), currentUser(r))
	writeResult(w, v, err)
}

func (s *Server) adminAccount(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminAccount(r.Context(), currentUser(r), r.PathValue("accountID"))
	writeResult(w, v, err)
}

func (s *Server) adminOAuthReauthorizeStart(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminStartOpenAIReauthorize(r.Context(), currentUser(r), r.PathValue("accountID"))
	writeResult(w, v, err)
}

func (s *Server) adminOAuthReauthorizeComplete(w http.ResponseWriter, r *http.Request) {
	var input struct {
		State string `json:"state"`
		Code  string `json:"code"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.AdminCompleteOpenAIReauthorize(r.Context(), currentUser(r), r.PathValue("accountID"), input.State, input.Code)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidInput) || errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			writeError(w, err)
			return
		}
		s.logger.Error("admin reauthorize OpenAI account", "error", err)
		writeErrorStatus(w, http.StatusBadGateway, "openai_oauth_failed", "OpenAI OAuth authorization failed")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) adminUpdateAccount(w http.ResponseWriter, r *http.Request) {
	var input application.AccountConfigInput
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.AdminUpdateAccountConfig(r.Context(), currentUser(r), r.PathValue("accountID"), input)
	writeResult(w, v, err)
}

func (s *Server) adminUpdateAccountStatus(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.AdminUpdateAccountStatus(r.Context(), currentUser(r), r.PathValue("accountID"), input.Status)
	writeResult(w, v, err)
}

func (s *Server) adminManualAccountTokenRefresh(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminManualRefreshAccountToken(r.Context(), currentUser(r), r.PathValue("accountID"))
	writeResult(w, v, err)
}

func (s *Server) adminListPlans(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminListPlans(r.Context(), currentUser(r))
	writeResult(w, v, err)
}

func (s *Server) adminPlanDetail(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminPlanDetail(r.Context(), currentUser(r), r.PathValue("planID"), r.URL.Query().Get("timezone"))
	writeResult(w, v, err)
}

func (s *Server) adminPlanPerformance(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminPlanPerformance(r.Context(), currentUser(r), r.PathValue("planID"), r.URL.Query().Get("period"), r.URL.Query().Get("timezone"))
	writeResult(w, v, err)
}

func (s *Server) adminPlanRequestErrors(w http.ResponseWriter, r *http.Request) {
	page, pageSize := 1, 20
	var err error
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil {
			writeError(w, domain.ErrInvalidInput)
			return
		}
	}
	if raw := r.URL.Query().Get("page_size"); raw != "" {
		pageSize, err = strconv.Atoi(raw)
		if err != nil {
			writeError(w, domain.ErrInvalidInput)
			return
		}
	}
	v, err := s.app.AdminPlanRequestErrors(r.Context(), currentUser(r), r.PathValue("planID"), r.URL.Query().Get("period"), r.URL.Query().Get("timezone"), page, pageSize)
	writeResult(w, v, err)
}

func (s *Server) adminListPlanAuditEvents(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminListPlanAuditEvents(r.Context(), currentUser(r), r.PathValue("planID"))
	writeResult(w, v, err)
}

func (s *Server) adminUpdatePlan(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name              *string                        `json:"name"`
		Description       *string                        `json:"description"`
		AllocationMode    *string                        `json:"allocation_mode"`
		MemberAllocations []domain.MemberShareAllocation `json:"member_allocations"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	operationCount := 0
	if input.Name != nil {
		operationCount++
	}
	if input.Description != nil {
		operationCount++
	}
	if input.AllocationMode != nil {
		operationCount++
	}
	invalidAllocationPayload := (input.AllocationMode == nil) != (input.MemberAllocations == nil)
	if operationCount != 1 || invalidAllocationPayload {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	var (
		v   domain.Plan
		err error
	)
	if input.Name != nil {
		v, err = s.app.AdminRenamePlan(r.Context(), currentUser(r), r.PathValue("planID"), *input.Name)
	} else if input.Description != nil {
		v, err = s.app.AdminUpdatePlanDescription(r.Context(), currentUser(r), r.PathValue("planID"), *input.Description)
	} else if *input.AllocationMode == domain.AllocationFixed {
		v, err = s.app.AdminConvertPlanToFixed(r.Context(), currentUser(r), r.PathValue("planID"), input.MemberAllocations)
	} else {
		err = domain.ErrInvalidInput
	}
	writeResult(w, v, err)
}

func (s *Server) adminUpdatePlanStatus(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.AdminUpdatePlanStatus(r.Context(), currentUser(r), r.PathValue("planID"), input.Status)
	writeResult(w, v, err)
}

func (s *Server) adminRebindPlanAccount(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AccountID string `json:"account_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.AdminRebindPlanAccount(r.Context(), currentUser(r), r.PathValue("planID"), input.AccountID)
	writeResult(w, v, err)
}

func (s *Server) adminUpdatePlanPublication(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Visibility             string `json:"visibility"`
		PublicSlots            int    `json:"public_slots"`
		PublicShareBasisPoints int    `json:"public_share_basis_points"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.AdminUpdatePlanPublication(r.Context(), currentUser(r), r.PathValue("planID"), input.Visibility, input.PublicSlots, input.PublicShareBasisPoints)
	writeResult(w, v, err)
}

func (s *Server) adminDeletePlan(w http.ResponseWriter, r *http.Request) {
	err := s.app.AdminDeletePlan(r.Context(), currentUser(r), r.PathValue("planID"))
	writeResult(w, map[string]bool{"deleted": err == nil}, err)
}

func (s *Server) adminTransferPlanOwnership(w http.ResponseWriter, r *http.Request) {
	var input struct {
		MemberID string `json:"member_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.AdminTransferPlanOwnership(r.Context(), currentUser(r), r.PathValue("planID"), input.MemberID)
	writeResult(w, v, err)
}

func (s *Server) adminCreateInvite(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ShareBasisPoints int `json:"share_basis_points"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.AdminInvite(r.Context(), currentUser(r), r.PathValue("planID"), input.ShareBasisPoints)
	writeResult(w, v, err)
}

func (s *Server) adminRevokeInvite(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminRevokeInvite(r.Context(), currentUser(r), r.PathValue("planID"), r.PathValue("inviteID"))
	writeResult(w, v, err)
}

func (s *Server) adminReviewJoinApplication(w http.ResponseWriter, r *http.Request) {
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
	v, err := s.app.AdminReviewJoinApplication(r.Context(), currentUser(r), r.PathValue("planID"), r.PathValue("applicationID"), input.Decision == "approve")
	writeResult(w, v, err)
}

func (s *Server) adminUpdateMember(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ShareBasisPoints int `json:"share_basis_points"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	v, err := s.app.AdminUpdateMemberShare(r.Context(), currentUser(r), r.PathValue("planID"), r.PathValue("memberID"), input.ShareBasisPoints)
	writeResult(w, v, err)
}

func (s *Server) adminRemoveMember(w http.ResponseWriter, r *http.Request) {
	err := s.app.AdminRemovePlanMember(r.Context(), currentUser(r), r.PathValue("planID"), r.PathValue("memberID"))
	writeResult(w, map[string]bool{"removed": err == nil}, err)
}

func (s *Server) adminManualQuotaRefresh(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("planID")
	probe, release, err := s.app.AdminReservePlanQuotaProbe(r.Context(), currentUser(r), planID)
	if err != nil {
		writeError(w, err)
		return
	}
	defer release()
	signals, err := s.gateway.ProbeQuota(r.Context(), probe.AccessToken, probe.ChatGPTAccountID, probe.ProxyURL)
	if err != nil {
		s.logger.Error("admin probe OpenAI quota", "error", err, "plan_id", planID)
		writeErrorStatus(w, http.StatusBadGateway, "quota_probe_failed", "OpenAI quota query failed")
		return
	}
	if err := s.app.RecordManualQuotaSignals(r.Context(), probe, signals); err != nil {
		writeError(w, err)
		return
	}
	if err := s.app.AdminRecordPlanAction(r.Context(), currentUser(r), planID, "plan.quota_refreshed"); err != nil {
		s.logger.Error("admin record manual OpenAI quota refresh audit", "error", err, "plan_id", planID)
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account_id": probe.AccountID, "signals": signals})
}

func (s *Server) adminPlanQuotaResetCredits(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("planID")
	probe, err := s.app.AdminPreparePlanQuotaProbe(r.Context(), currentUser(r), planID)
	if err != nil {
		writeError(w, err)
		return
	}
	credits, err := s.gateway.QueryQuotaResetCredits(r.Context(), probe.AccessToken, probe.ChatGPTAccountID, probe.ProxyURL)
	if err != nil {
		s.logQuotaResetCreditsError("admin query OpenAI quota reset credits", planID, err)
		writeQuotaResetCreditsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, credits)
}

func (s *Server) adminResetPlanQuota(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("planID")
	probe, release, err := s.app.AdminQuiescePlanQuota(r.Context(), currentUser(r), planID)
	if err != nil {
		writeError(w, err)
		return
	}
	defer release()
	operationID, err := s.app.ReserveManualQuotaReset(r.Context(), probe, currentUser(r).ID, "admin_reset")
	if err != nil {
		s.logger.Error("reserve admin quota reset", "error", err, "plan_id", planID)
		writeError(w, err)
		return
	}
	defer s.releaseQuotaResetExecution(r.Context(), planID, operationID)
	reset, err := s.gateway.ConsumeQuotaResetCredit(r.Context(), probe.AccessToken, probe.ChatGPTAccountID, probe.ProxyURL)
	if err != nil {
		s.logger.Error("admin reset OpenAI quota", "error", err, "plan_id", planID)
		writeErrorStatus(w, http.StatusBadGateway, "quota_reset_failed", "OpenAI quota reset failed")
		return
	}
	if err := s.app.AdminRecordPlanAction(r.Context(), currentUser(r), planID, "plan.quota_reset"); err != nil {
		// The external reset has already consumed a credit. Returning an error here
		// could prompt a destructive retry, so preserve the successful reset result.
		s.logger.Error("admin record OpenAI quota reset audit", "error", err, "plan_id", planID)
	}
	result := domain.PlanQuotaResetResult{Code: reset.Code, Credit: reset.Credit, WindowsReset: reset.WindowsReset, QuotaRefreshed: false, Signals: make([]domain.QuotaSignal, 0)}
	signals, err := s.gateway.ProbeQuota(r.Context(), probe.AccessToken, probe.ChatGPTAccountID, probe.ProxyURL)
	if err != nil {
		s.logger.Error("admin refresh OpenAI quota after reset", "error", err, "plan_id", planID)
		writeJSON(w, http.StatusOK, result)
		return
	}
	if err := s.app.RecordResetQuotaSignals(r.Context(), probe, signals); err != nil {
		s.logger.Error("admin record OpenAI quota after reset", "error", err, "plan_id", planID)
		writeJSON(w, http.StatusOK, result)
		return
	}
	result.QuotaRefreshed = true
	result.Signals = signals
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) adminListAPIKeys(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminListAPIKeys(r.Context(), currentUser(r))
	writeResult(w, v, err)
}

func (s *Server) adminRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	err := s.app.AdminRevokeAPIKey(r.Context(), currentUser(r), r.PathValue("keyID"))
	writeResult(w, map[string]bool{"revoked": err == nil}, err)
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin {
			writeError(w, domain.ErrForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
