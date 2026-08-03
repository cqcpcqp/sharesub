package application

import (
	"encoding/json"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

func (s *Service) newAuditEvent(actorUserID, action, resourceType, resourceID string, metadata any) (domain.AuditEvent, error) {
	id, err := security.NewID()
	if err != nil {
		return domain.AuditEvent{}, err
	}
	body := []byte("{}")
	if metadata != nil {
		body, err = json.Marshal(metadata)
		if err != nil {
			return domain.AuditEvent{}, err
		}
	}
	return domain.AuditEvent{ID: id, ActorUserID: actorUserID, Action: action, ResourceType: resourceType, ResourceID: resourceID, Metadata: body, CreatedAt: s.now()}, nil
}

func normalizeEmail(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func normalizeAccountConfig(config AccountConfigInput) (AccountConfigInput, error) {
	config.Name = strings.TrimSpace(config.Name)
	config.Notes = strings.TrimSpace(config.Notes)
	config.ProxyURL = strings.TrimSpace(config.ProxyURL)
	if config.FastPolicy == nil {
		config.FastPolicy = make([]domain.FastPolicyRule, 0)
	}
	if utf8.RuneCountInString(config.Name) < 1 || utf8.RuneCountInString(config.Name) > 100 || utf8.RuneCountInString(config.Notes) > 2000 {
		return AccountConfigInput{}, domain.ErrInvalidInput
	}
	if config.MaxConcurrency < 0 || config.MaxConcurrency > 100 || config.RPMLimit < 0 || config.RPMLimit > 10_000 {
		return AccountConfigInput{}, domain.ErrInvalidInput
	}
	if config.Status != domain.StatusActive && config.Status != domain.StatusDisabled && config.Status != domain.StatusRefreshRequired {
		return AccountConfigInput{}, domain.ErrInvalidInput
	}
	if len(config.FastPolicy) > 50 {
		return AccountConfigInput{}, domain.ErrInvalidInput
	}
	validTiers := map[string]bool{"all": true, "priority": true, "flex": true}
	validActions := map[string]bool{"pass": true, "filter": true, "block": true, "force_priority": true}
	for index := range config.FastPolicy {
		rule := &config.FastPolicy[index]
		if rule.UserIDs == nil {
			rule.UserIDs = make([]string, 0)
		}
		if rule.ModelWhitelist == nil {
			rule.ModelWhitelist = make([]string, 0)
		}
		rule.ServiceTier = strings.ToLower(strings.TrimSpace(rule.ServiceTier))
		rule.Action = strings.ToLower(strings.TrimSpace(rule.Action))
		rule.ErrorMessage = strings.TrimSpace(rule.ErrorMessage)
		rule.FallbackAction = strings.ToLower(strings.TrimSpace(rule.FallbackAction))
		rule.FallbackErrorMessage = strings.TrimSpace(rule.FallbackErrorMessage)
		if !validTiers[rule.ServiceTier] || !validActions[rule.Action] || !validActions[rule.FallbackAction] || utf8.RuneCountInString(rule.ErrorMessage) > 500 || utf8.RuneCountInString(rule.FallbackErrorMessage) > 500 {
			return AccountConfigInput{}, domain.ErrInvalidInput
		}
		if len(rule.UserIDs) > 500 || len(rule.ModelWhitelist) > 100 {
			return AccountConfigInput{}, domain.ErrInvalidInput
		}
		seenUsers := make(map[string]struct{}, len(rule.UserIDs))
		for userIndex := range rule.UserIDs {
			rule.UserIDs[userIndex] = strings.TrimSpace(rule.UserIDs[userIndex])
			if rule.UserIDs[userIndex] == "" {
				return AccountConfigInput{}, domain.ErrInvalidInput
			}
			if _, exists := seenUsers[rule.UserIDs[userIndex]]; exists {
				return AccountConfigInput{}, domain.ErrInvalidInput
			}
			seenUsers[rule.UserIDs[userIndex]] = struct{}{}
		}
		seenModels := make(map[string]struct{}, len(rule.ModelWhitelist))
		for modelIndex := range rule.ModelWhitelist {
			rule.ModelWhitelist[modelIndex] = strings.TrimSpace(rule.ModelWhitelist[modelIndex])
			pattern := rule.ModelWhitelist[modelIndex]
			if pattern == "" || utf8.RuneCountInString(pattern) > 100 || strings.Count(pattern, "*") > 1 || (strings.Contains(pattern, "*") && !strings.HasSuffix(pattern, "*")) {
				return AccountConfigInput{}, domain.ErrInvalidInput
			}
			if _, exists := seenModels[pattern]; exists {
				return AccountConfigInput{}, domain.ErrInvalidInput
			}
			seenModels[pattern] = struct{}{}
		}
	}
	if config.ProxyURL != "" {
		parsed, err := url.Parse(config.ProxyURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "socks5") {
			return AccountConfigInput{}, domain.ErrInvalidInput
		}
	}
	return config, nil
}

func (s *Service) setAccountProxy(account *domain.Account, proxyURL string) error {
	account.ProxyURL = proxyURL
	account.ProxyURLCiphertext = nil
	if proxyURL == "" {
		return nil
	}
	ciphertext, err := s.security.Encrypt(proxyURL, []byte(account.OwnerUserID+":"+account.ChatGPTAccountID+":proxy"))
	if err != nil {
		return err
	}
	account.ProxyURLCiphertext = ciphertext
	return nil
}

func (s *Service) hydrateAccountProxy(account *domain.Account) error {
	if len(account.ProxyURLCiphertext) == 0 {
		account.ProxyURL = ""
		return nil
	}
	proxyURL, err := s.security.Decrypt(account.ProxyURLCiphertext, []byte(account.OwnerUserID+":"+account.ChatGPTAccountID+":proxy"))
	if err != nil {
		return err
	}
	account.ProxyURL = proxyURL
	return nil
}

func validAllocationMode(value string) bool {
	return value == domain.AllocationFixed || value == domain.AllocationShared
}

func credentialQuotaLoad(credential domain.GatewayCredential) (int64, int64) {
	if credential.Plan.AllocationMode == domain.AllocationShared {
		return credential.AccountUsageMicros, domain.MaxShareBPS
	}
	return credential.UsageMicros, int64(credential.Member.ShareBasisPoints)
}

func validEmail(value string) bool {
	at := strings.LastIndexByte(value, '@')
	return at > 0 && at < len(value)-1 && strings.Contains(value[at+1:], ".") && len(value) <= 254
}

func validUsername(value string) bool {
	length := utf8.RuneCountInString(value)
	if length < 2 || length > 32 {
		return false
	}
	for _, char := range value {
		if !unicode.IsLetter(char) && !unicode.IsNumber(char) && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func validStrategy(value string) bool {
	return value == domain.RoutePriority || value == domain.RouteBalanced
}

func validRoutes(routes []domain.APIKeyRoute) bool {
	if len(routes) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		if route.PlanID == "" || route.Priority < 1 || route.Priority > 10_000 {
			return false
		}
		if _, exists := seen[route.PlanID]; exists {
			return false
		}
		seen[route.PlanID] = struct{}{}
	}
	return true
}
