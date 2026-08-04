package httpapi

import (
	"net/http"

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
