package httpapi

import (
	"net/http"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Server) membership(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := r.PathValue("userID")
	if id == "" {
		id = user.ID
	}
	value, err := s.app.Membership(r.Context(), user, id)
	writeResult(w, value, err)
}
func (s *Server) membershipOrders(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := r.PathValue("userID")
	if id == "" {
		id = user.ID
	}
	value, err := s.app.MembershipOrders(r.Context(), user, id)
	writeResult(w, value, err)
}
func (s *Server) adjustMembership(w http.ResponseWriter, r *http.Request) {
	var input domain.MembershipAdjustment
	if !decodeJSON(w, r, &input) {
		return
	}
	err := s.app.AdjustMembership(r.Context(), currentUser(r), r.PathValue("userID"), input)
	writeResult(w, map[string]bool{"updated": err == nil}, err)
}
func (s *Server) createMembershipCheckout(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Product string `json:"product"`
		Method  string `json:"payment_method"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	value, err := s.app.CreateMembershipCheckout(r.Context(), currentUser(r), input.Product, input.Method)
	writeResult(w, value, err)
}
func (s *Server) verifyMembershipPayment(w http.ResponseWriter, r *http.Request) {
	value, err := s.app.VerifyMembershipPayment(r.Context(), currentUser(r), r.PathValue("orderID"))
	writeResult(w, value, err)
}
func (s *Server) cancelMembershipOrder(w http.ResponseWriter, r *http.Request) {
	err := s.app.CancelMembershipOrder(r.Context(), currentUser(r), r.PathValue("orderID"))
	writeResult(w, map[string]bool{"cancelled": err == nil}, err)
}
func (s *Server) paymentSettings(w http.ResponseWriter, r *http.Request) {
	value, err := s.app.AdminPaymentSettings(r.Context(), currentUser(r))
	writeResult(w, value, err)
}
func (s *Server) savePaymentSettings(w http.ResponseWriter, r *http.Request) {
	var input application.PaymentSettingsInput
	if !decodeJSON(w, r, &input) {
		return
	}
	value, err := s.app.AdminSavePaymentSettings(r.Context(), currentUser(r), input)
	writeResult(w, value, err)
}
func (s *Server) easyPayNotify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if len(r.URL.RawQuery) > 16<<10 {
		http.Error(w, "fail", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "fail", http.StatusBadRequest)
		return
	}
	if err := s.app.ConfirmEasyPay(r.Context(), r.Form); err != nil {
		s.logger.Warn("membership payment notification rejected", "error", err)
		http.Error(w, "fail", http.StatusBadRequest)
		return
	}
	_, _ = w.Write([]byte("success"))
}
func (s *Server) paymentReturn(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if len(r.URL.RawQuery) > 16<<10 {
		http.Error(w, "invalid payment return", http.StatusBadRequest)
		return
	}
	if err := s.app.ValidateMembershipReturn(r.Context(), r.URL.Query()); err != nil {
		writeError(w, err)
		return
	}
	http.Redirect(w, r, "/profile?membership_payment=returned", http.StatusSeeOther)
}
