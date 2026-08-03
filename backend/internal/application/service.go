package application

import (
	"strings"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

const oauthFlowTTL = 15 * time.Minute
const MaxAvatarBytes = 2 << 20

type Service struct {
	store       Store
	security    *security.Manager
	oauth       OpenAIOAuth
	sessionTTL  time.Duration
	redirectURI string
	publicURL   string
	now         func() time.Time
	traffic     *accountTrafficController
}

type AuthResult struct {
	User  domain.User `json:"user"`
	Token string      `json:"token"`
}

type OAuthStart struct {
	AuthorizationURL string `json:"authorization_url"`
	FlowID           string `json:"flow_id"`
}

type CreatedInvite struct {
	Invite    domain.Invite `json:"invite"`
	InviteURL string        `json:"invite_url"`
}

type CreatedAPIKey struct {
	APIKey domain.APIKey `json:"api_key"`
	Key    string        `json:"key"`
}

type AccountConfigInput struct {
	Name           string                  `json:"name"`
	Notes          string                  `json:"notes"`
	ProxyURL       string                  `json:"proxy_url"`
	MaxConcurrency int                     `json:"max_concurrency"`
	RPMLimit       int                     `json:"rpm_limit"`
	FastPolicy     []domain.FastPolicyRule `json:"fast_policy"`
	Status         string                  `json:"status"`
}

func NewService(store Store, securityManager *security.Manager, oauth OpenAIOAuth, sessionTTL time.Duration, redirectURI, publicURL string) *Service {
	return &Service{store: store, security: securityManager, oauth: oauth, sessionTTL: sessionTTL, redirectURI: redirectURI, publicURL: strings.TrimRight(publicURL, "/"), now: time.Now, traffic: newAccountTrafficController()}
}
