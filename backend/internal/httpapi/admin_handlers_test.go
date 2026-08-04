package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func TestRequireAdminRejectsRegularUser(t *testing.T) {
	server := &Server{}
	called := false
	handler := server.requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	for _, test := range []struct {
		name       string
		user       domain.User
		wantStatus int
		wantCalled bool
	}{
		{name: "member", user: domain.User{ID: "member"}, wantStatus: http.StatusForbidden},
		{name: "admin", user: domain.User{ID: "admin", IsAdmin: true}, wantStatus: http.StatusOK, wantCalled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			called = false
			request := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
			request = request.WithContext(context.WithValue(request.Context(), userContextKey{}, test.user))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus || called != test.wantCalled {
				t.Fatalf("status = %d, called = %t", response.Code, called)
			}
		})
	}
}

func TestForcedPasswordChangeAllowsOnlyRecoveryRoutes(t *testing.T) {
	tests := []struct {
		method, path string
		allowed      bool
	}{
		{http.MethodGet, "/api/me", true},
		{http.MethodPatch, "/api/me/password", true},
		{http.MethodPost, "/api/auth/logout", true},
		{http.MethodGet, "/api/admin/overview", false},
		{http.MethodPatch, "/api/me", false},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, nil)
		if passwordChangeAllowed(request) != test.allowed {
			t.Errorf("%s %s allowed = %t", test.method, test.path, !test.allowed)
		}
	}
}
