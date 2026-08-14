package application

import (
	"context"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type OAuthFlow struct {
	ID              string
	UserID          string
	StateHash       []byte
	CodeVerifier    string
	RedirectURI     string
	Purpose         string
	TargetAccountID string
	ExpiresAt       time.Time
}

type OAuthToken struct {
	AccessToken      string
	RefreshToken     string
	IDToken          string
	ExpiresAt        time.Time
	Email            string
	ChatGPTAccountID string
	PlanType         string
}

type OpenAIOAuth interface {
	AuthorizationURL(state, challenge, redirectURI string) string
	Exchange(context.Context, string, string, string) (OAuthToken, error)
	Refresh(context.Context, string) (OAuthToken, error)
	SubscriptionExpiresAt(context.Context, string, string, string) (*time.Time, error)
}

type QuotaProber interface {
	ProbeQuota(context.Context, string, string, string) ([]domain.QuotaSignal, error)
}
