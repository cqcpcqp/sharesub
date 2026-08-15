package application

import (
	"context"
	"strings"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

const oauthFlowTTL = 15 * time.Minute
const MaxAvatarBytes = 2 << 20

type Service struct {
	store         Store
	security      *security.Manager
	oauth         OpenAIOAuth
	quotaProber   QuotaProber
	sessionTTL    time.Duration
	redirectURI   string
	publicURL     string
	now           func() time.Time
	traffic       *accountTrafficController
	runtimeStatus RuntimeStatusProvider
}

type RuntimeStatusProvider interface {
	Snapshot(context.Context) domain.AdminRuntimeStatus
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
	Name                 string                  `json:"name"`
	Notes                string                  `json:"notes"`
	ProxyURL             string                  `json:"proxy_url"`
	MaxConcurrency       int                     `json:"max_concurrency"`
	RPMLimit             int                     `json:"rpm_limit"`
	FastPolicy           []domain.FastPolicyRule `json:"fast_policy"`
	CodexFingerprintMode string                  `json:"codex_fingerprint_mode"`
	Status               string                  `json:"status"`
}

func NewService(store Store, securityManager *security.Manager, oauth OpenAIOAuth, sessionTTL time.Duration, redirectURI, publicURL string, quotaProber ...QuotaProber) *Service {
	service := &Service{store: store, security: securityManager, oauth: oauth, sessionTTL: sessionTTL, redirectURI: redirectURI, publicURL: strings.TrimRight(publicURL, "/"), now: time.Now, traffic: newAccountTrafficController()}
	if len(quotaProber) > 0 {
		service.quotaProber = quotaProber[0]
	}
	return service
}

func (s *Service) SetRuntimeStatusProvider(provider RuntimeStatusProvider) {
	s.runtimeStatus = provider
}

func (s *Service) decorateUser(user domain.User) domain.User {
	user.IsAdmin = user.Role == domain.RoleAdmin
	return user
}
