package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

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
		if user.MustChangePassword && !passwordChangeAllowed(r) {
			writeError(w, domain.ErrPasswordChangeRequired)
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func passwordChangeAllowed(r *http.Request) bool {
	return (r.Method == http.MethodGet && r.URL.Path == "/api/me") ||
		(r.Method == http.MethodPatch && r.URL.Path == "/api/me/password") ||
		(r.Method == http.MethodPost && r.URL.Path == "/api/auth/logout")
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
	case errors.Is(err, domain.ErrPasswordChangeRequired):
		writeErrorStatus(w, 403, "password_change_required", err.Error())
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
