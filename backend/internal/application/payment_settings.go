package application

import (
	"context"
	"net/url"
	"strings"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/payment"
)

type PaymentSettingsView struct {
	domain.PaymentSettings
	KeyConfigured bool   `json:"key_configured"`
	Source        string `json:"source"`
	PublicURL     string `json:"public_url"`
	CallbackReady bool   `json:"callback_ready"`
}

type PaymentSettingsInput struct {
	BaseURL  string `json:"base_url"`
	PID      string `json:"pid"`
	Key      string `json:"key"`
	Enabled  bool   `json:"enabled"`
	Revision int64  `json:"revision"`
}

func (s *Service) InitializePayments(ctx context.Context, config payment.Config) error {
	settings, err := s.store.PaymentSettings(ctx)
	if err != nil {
		return err
	}
	if settings.Revision == 0 {
		if config.PID != "" || config.BaseURL != "" || config.Key != "" {
			if _, err := payment.New(config); err != nil {
				return err
			}
		}
		s.paymentConfig = config
		return nil
	}
	if settings.Enabled && !s.paymentCallbackReady() {
		return domain.ErrInvalidInput
	}
	return nil
}

func (s *Service) paymentSettings(ctx context.Context) (domain.PaymentSettings, payment.Config, error) {
	settings, err := s.store.PaymentSettings(ctx)
	if err != nil {
		return settings, payment.Config{}, err
	}
	if settings.Revision == 0 {
		settings.BaseURL, settings.PID = s.paymentConfig.BaseURL, s.paymentConfig.PID
		settings.Enabled = false
		return settings, s.paymentConfig, nil
	}
	key, err := s.security.Decrypt(settings.KeyCiphertext, []byte("payment-settings"))
	return settings, payment.Config{BaseURL: settings.BaseURL, PID: settings.PID, Key: key}, err
}

func (s *Service) paymentCallbackReady() bool {
	parsed, err := url.Parse(s.publicURL)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func (s *Service) AdminPaymentSettings(ctx context.Context, user domain.User) (PaymentSettingsView, error) {
	if user.Role != domain.RoleAdmin {
		return PaymentSettingsView{}, domain.ErrForbidden
	}
	settings, config, err := s.paymentSettings(ctx)
	if err != nil {
		return PaymentSettingsView{}, err
	}
	source := "database"
	if settings.Revision == 0 {
		source = "environment"
	}
	return PaymentSettingsView{PaymentSettings: settings, KeyConfigured: config.Key != "", Source: source, PublicURL: s.publicURL, CallbackReady: s.paymentCallbackReady()}, nil
}

func (s *Service) AdminSavePaymentSettings(ctx context.Context, user domain.User, input PaymentSettingsInput) (PaymentSettingsView, error) {
	if user.Role != domain.RoleAdmin {
		return PaymentSettingsView{}, domain.ErrForbidden
	}
	settings, previous, err := s.paymentSettings(ctx)
	if err != nil {
		return PaymentSettingsView{}, err
	}
	if input.Revision != settings.Revision {
		return PaymentSettingsView{}, domain.ErrConflict
	}
	config := payment.Config{BaseURL: strings.TrimRight(strings.TrimSpace(input.BaseURL), "/"), PID: strings.TrimSpace(input.PID), Key: strings.TrimSpace(input.Key)}
	if config.Key == "" {
		config.Key = previous.Key
	}
	if _, err = payment.New(config); err != nil {
		return PaymentSettingsView{}, domain.ErrInvalidInput
	}
	if input.Enabled && !s.paymentCallbackReady() {
		return PaymentSettingsView{}, domain.ErrInvalidInput
	}
	ciphertext, err := s.security.Encrypt(config.Key, []byte("payment-settings"))
	if err != nil {
		return PaymentSettingsView{}, err
	}
	event, err := s.newAuditEvent(user.ID, "payment.settings_updated", "payment_settings", "easypay", map[string]any{"enabled": input.Enabled, "key_changed": input.Key != ""})
	if err != nil {
		return PaymentSettingsView{}, err
	}
	err = s.store.SavePaymentSettings(ctx, domain.PaymentSettings{BaseURL: config.BaseURL, PID: config.PID, KeyCiphertext: ciphertext, Enabled: input.Enabled, Revision: input.Revision}, event)
	if err != nil {
		return PaymentSettingsView{}, err
	}
	return s.AdminPaymentSettings(ctx, user)
}
