package httpapi

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

type avatarHTTPStore struct {
	application.Store
	saved domain.UserAvatar
}

func (s *avatarHTTPStore) UpdateUserAvatar(_ context.Context, userID string, avatar domain.UserAvatar, _ time.Time) (domain.User, error) {
	s.saved = avatar
	return domain.User{ID: userID, Username: "tester", AvatarURL: "/api/users/tester/avatar?v=1"}, nil
}

func (s *avatarHTTPStore) UserAvatar(context.Context, string) (domain.UserAvatar, error) {
	return s.saved, nil
}

func TestAvatarUploadAndReadHandlers(t *testing.T) {
	store := &avatarHTTPStore{}
	service := application.NewService(store, nil, nil, 0, "", "")
	server := &Server{app: service}
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("avatar", "avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/me/avatar", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request = request.WithContext(context.WithValue(request.Context(), userContextKey{}, domain.User{ID: "tester"}))
	response := httptest.NewRecorder()
	server.updateAvatar(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.saved.MediaType != "image/png" || !bytes.Equal(store.saved.Data, png) {
		t.Fatalf("saved avatar = %+v", store.saved)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/users/tester/avatar", nil)
	request.SetPathValue("userID", "tester")
	response = httptest.NewRecorder()
	server.userAvatar(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" || !bytes.Equal(response.Body.Bytes(), png) {
		t.Fatalf("avatar response = %d, %q, %x", response.Code, response.Header().Get("Content-Type"), response.Body.Bytes())
	}
}
