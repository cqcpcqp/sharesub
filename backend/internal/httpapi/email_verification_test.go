package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

type emailVerificationHTTPStore struct {
	application.Store
	created bool
}

func (s *emailVerificationHTTPStore) CreateUserWithEmailVerification(context.Context, domain.User, domain.AgreementAcceptance, domain.EmailVerificationToken) error {
	s.created = true
	return nil
}

type successfulVerificationSender struct{}

func (successfulVerificationSender) SendEmailVerification(context.Context, string, string) error {
	return nil
}

func newEmailVerificationHTTPServer(t *testing.T, store application.Store) *Server {
	t.Helper()
	key := make([]byte, 32)
	manager, err := security.New(key, key)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithEmailVerification(store, manager, nil, time.Hour, "", "https://share.underelay.com", successfulVerificationSender{}, time.Hour, time.Minute)
	return New(service, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestRegisterReturnsPendingVerificationContract(t *testing.T) {
	store := &emailVerificationHTTPStore{}
	server := newEmailVerificationHTTPServer(t, store)
	body := `{"username":"member","email":"member@example.com","password":"strong-password","agreement":{"accepted":true,"terms_version":"2026-08-05","privacy_policy_version":"2026-08-17","acceptable_use_version":"2026-08-05"}}`
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !store.created {
		t.Fatalf("status = %d, body = %s, created = %v", response.Code, response.Body.String(), store.created)
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["email"] != "member@example.com" || result["verification_expires_at"] == nil || result["resend_available_at"] == nil {
		t.Fatalf("registration response = %+v", result)
	}
	if result["token"] != nil || result["user"] != nil {
		t.Fatalf("registration exposed an authenticated result: %+v", result)
	}
}

func TestVerifyEmailRejectsMalformedTokenWithStableError(t *testing.T) {
	server := newEmailVerificationHTTPServer(t, &emailVerificationHTTPStore{})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/email/verify", strings.NewReader(`{"token":"invalid"}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"email_verification_invalid"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
