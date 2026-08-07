package httpapi

import (
	"net/http"

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

func (s *Server) adminListPlans(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AdminListPlans(r.Context(), currentUser(r))
	writeResult(w, v, err)
}

func (s *Server) adminUpdatePlan(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if (input.Name == nil) == (input.Description == nil) {
		writeError(w, domain.ErrInvalidInput)
		return
	}
	var (
		v   domain.Plan
		err error
	)
	if input.Name != nil {
		v, err = s.app.AdminRenamePlan(r.Context(), currentUser(r), r.PathValue("planID"), *input.Name)
	} else {
		v, err = s.app.AdminUpdatePlanDescription(r.Context(), currentUser(r), r.PathValue("planID"), *input.Description)
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
