package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Server) requireGatewayPricing(w http.ResponseWriter, ctx context.Context, access *application.GatewayAccess) bool {
	pricing, err := s.app.CurrentPricing(ctx)
	if err != nil {
		s.logger.Error("load gateway pricing", "error", err)
		writeGatewayErrorStatus(w, http.StatusServiceUnavailable, "pricing_unavailable", "platform pricing is temporarily unavailable")
		return false
	}
	access.Pricing = &pricing
	return true
}

func (s *Server) currentPricing(w http.ResponseWriter, r *http.Request) {
	version, err := s.app.CurrentPricing(r.Context())
	w.Header().Set("Cache-Control", "no-store")
	writeResult(w, version, err)
}

func (s *Server) pricingVersion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("versionID"), 10, 64)
	if err != nil {
		writeResult(w, nil, domain.ErrInvalidInput)
		return
	}
	version, err := s.app.PricingVersion(r.Context(), id)
	writeResult(w, version, err)
}

func (s *Server) pricingHistory(w http.ResponseWriter, r *http.Request) {
	var before int64
	if value := r.URL.Query().Get("before"); value != "" {
		var err error
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			writeResult(w, nil, domain.ErrInvalidInput)
			return
		}
	}
	versions, err := s.app.PricingHistory(r.Context(), before)
	w.Header().Set("Cache-Control", "no-store")
	writeResult(w, versions, err)
}

func (s *Server) publishPricing(w http.ResponseWriter, r *http.Request) {
	var input domain.PublishPricingInput
	if !decodeJSON(w, r, &input) {
		return
	}
	version, err := s.app.PublishPricing(r.Context(), currentUser(r), input)
	writeResult(w, version, err)
}
