package application

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/payment"
)

type MembershipCheckout struct {
	Order  domain.MembershipOrder `json:"order"`
	PayURL string                 `json:"pay_url"`
}

func (s *Service) Membership(ctx context.Context, user domain.User, userID string) (domain.Membership, error) {
	if user.ID != userID && user.Role != domain.RoleAdmin {
		return domain.Membership{}, domain.ErrForbidden
	}
	value, err := s.store.Membership(ctx, userID, s.now())
	if err != nil {
		return value, err
	}
	settings, _, err := s.paymentSettings(ctx)
	value.PaymentEnabled = settings.Enabled && value.BillingStarted
	return value, err
}
func (s *Service) MembershipOrders(ctx context.Context, user domain.User, userID string) ([]domain.MembershipOrder, error) {
	if user.ID != userID && user.Role != domain.RoleAdmin {
		return nil, domain.ErrForbidden
	}
	return s.store.MembershipOrders(ctx, userID)
}
func (s *Service) AdjustMembership(ctx context.Context, user domain.User, userID string, input domain.MembershipAdjustment) error {
	if user.Role != domain.RoleAdmin {
		return domain.ErrForbidden
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" || len(input.Reason) > 500 || (input.Tier != "none" && input.Tier != "vip" && input.Tier != "svip") || (input.Tier != "none" && input.ExpiresAt == nil) || (input.Tier == "none" && input.ExpiresAt != nil) || (input.OwnerLimitOverride != nil && (*input.OwnerLimitOverride < 0 || *input.OwnerLimitOverride > 10000)) {
		return domain.ErrInvalidInput
	}
	if input.ReviewOrderID != "" && (input.Tier == "none" || !input.ExpiresAt.After(s.now())) {
		return domain.ErrInvalidInput
	}
	before, err := s.store.Membership(ctx, userID, s.now())
	if err != nil {
		return err
	}
	event, err := s.newAuditEvent(user.ID, "membership.adjusted", "membership", userID, map[string]any{"before": before, "after": input})
	if err != nil {
		return err
	}
	return s.store.AdjustMembership(ctx, userID, input, event)
}
func (s *Service) CreateMembershipCheckout(ctx context.Context, user domain.User, product, method string) (MembershipCheckout, error) {
	settings, config, err := s.paymentSettings(ctx)
	if err != nil {
		return MembershipCheckout{}, err
	}
	if !settings.Enabled {
		return MembershipCheckout{}, domain.ErrPaymentUnavailable
	}
	if method != "alipay" && method != "wxpay" {
		return MembershipCheckout{}, domain.ErrInvalidInput
	}
	id, err := payment.NewOrderID()
	if err != nil {
		return MembershipCheckout{}, err
	}
	ciphertext, err := s.security.Encrypt(config.Key, []byte("membership-payment:"+id))
	if err != nil {
		return MembershipCheckout{}, err
	}
	now := s.now()
	order, err := s.store.CreateMembershipOrder(ctx, domain.MembershipOrder{ID: id, UserID: user.ID, Product: product, ProviderPID: config.PID, ProviderBaseURL: config.BaseURL, ProviderKeyCiphertext: ciphertext, PaymentMethod: method, CreatedAt: now, ExpiresAt: now.Add(30 * time.Minute)})
	if err != nil {
		return MembershipCheckout{}, err
	}
	provider, err := s.membershipOrderProvider(order)
	if err != nil {
		return MembershipCheckout{}, err
	}
	name := "平台 VIP 会员 · 30 天"
	if order.Product == "svip" {
		name = "平台 SVIP 会员 · 30 天"
	}
	if order.Product == "upgrade" {
		name = "VIP 升级 SVIP · 到期时间不变"
	}
	return MembershipCheckout{Order: order, PayURL: provider.CheckoutURL(order.ID, order.PaymentMethod, s.publicURL+"/api/payment/easypay/notify", s.publicURL+"/api/payment/return", order.AmountCents, name)}, nil
}
func (s *Service) membershipOrderProvider(order domain.MembershipOrder) (*payment.EasyPay, error) {
	key, err := s.security.Decrypt(order.ProviderKeyCiphertext, []byte("membership-payment:"+order.ID))
	if err != nil {
		return nil, err
	}
	return payment.New(payment.Config{BaseURL: order.ProviderBaseURL, PID: order.ProviderPID, Key: key})
}
func (s *Service) ConfirmEasyPay(ctx context.Context, values url.Values) error {
	order, err := s.store.MembershipOrder(ctx, values.Get("out_trade_no"))
	if err != nil {
		return err
	}
	provider, err := s.membershipOrderProvider(order)
	if err != nil {
		return err
	}
	confirmation, err := provider.Verify(values)
	if err != nil {
		return domain.ErrInvalidInput
	}
	if !confirmation.Paid {
		return nil
	}
	return s.store.ConfirmMembershipPayment(ctx, confirmation.OrderID, confirmation.TradeNo, confirmation.PID, confirmation.AmountCents, s.now())
}
func (s *Service) ValidateMembershipReturn(ctx context.Context, values url.Values) error {
	order, err := s.store.MembershipOrder(ctx, values.Get("out_trade_no"))
	if err != nil {
		return err
	}
	provider, err := s.membershipOrderProvider(order)
	if err != nil {
		return err
	}
	confirmation, err := provider.Verify(values)
	if err != nil || confirmation.OrderID != order.ID || confirmation.AmountCents != order.AmountCents {
		return domain.ErrInvalidInput
	}
	return nil
}
func (s *Service) CancelMembershipOrder(ctx context.Context, user domain.User, id string) error {
	return s.store.CancelMembershipOrder(ctx, id, user.ID)
}
func (s *Service) VerifyMembershipPayment(ctx context.Context, user domain.User, id string) (domain.MembershipOrder, error) {
	order, err := s.store.MembershipOrder(ctx, id)
	if err != nil {
		return order, err
	}
	if user.ID != order.UserID && user.Role != domain.RoleAdmin {
		return domain.MembershipOrder{}, domain.ErrForbidden
	}
	if order.Status == "pending" || order.Status == "expired" {
		claimed, err := s.store.ClaimMembershipQuery(ctx, id, s.now())
		if err != nil {
			return order, err
		}
		if claimed {
			if err = s.reconcileMembership(ctx, order); err != nil {
				return order, err
			}
		}
	}
	return s.store.MembershipOrder(ctx, id)
}
func (s *Service) reconcileMembership(ctx context.Context, order domain.MembershipOrder) error {
	provider, err := s.membershipOrderProvider(order)
	if err != nil {
		return err
	}
	confirmation, err := provider.Query(ctx, order.ID)
	if err != nil {
		return err
	}
	if !confirmation.Paid {
		return nil
	}
	return s.store.ConfirmMembershipPayment(ctx, confirmation.OrderID, confirmation.TradeNo, confirmation.PID, confirmation.AmountCents, s.now())
}
func (s *Service) RunMembershipMaintenance(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.store.SendMembershipReminders(ctx, s.now()); err != nil && ctx.Err() == nil {
			logger.Error("membership reminders failed", "error", err)
		}
		orders, err := s.store.MembershipReconciliationBatch(ctx, s.now())
		if err != nil && ctx.Err() == nil {
			logger.Error("membership reconciliation failed", "error", err)
		}
		for _, order := range orders {
			if ctx.Err() != nil {
				return
			}
			if err = s.reconcileMembership(ctx, order); err != nil {
				logger.Warn("membership order query failed", "order_id", order.ID, "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
