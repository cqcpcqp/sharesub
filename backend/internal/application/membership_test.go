package application

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
	"github.com/sharesub/sharesub/backend/internal/security"
)

type membershipStoreStub struct {
	Store
	settings  domain.PaymentSettings
	member    domain.Membership
	order     domain.MembershipOrder
	confirmed int
	adjusted  bool
	event     domain.AuditEvent
}

func (stub *membershipStoreStub) PaymentSettings(context.Context) (domain.PaymentSettings, error) {
	return stub.settings, nil
}
func (stub *membershipStoreStub) SavePaymentSettings(_ context.Context, value domain.PaymentSettings, _ domain.AuditEvent) error {
	value.Revision++
	stub.settings = value
	return nil
}
func (stub *membershipStoreStub) Membership(_ context.Context, id string, _ time.Time) (domain.Membership, error) {
	value := stub.member
	value.UserID = id
	return value, nil
}
func (stub *membershipStoreStub) MembershipOrders(context.Context, string) ([]domain.MembershipOrder, error) {
	return []domain.MembershipOrder{stub.order}, nil
}
func (stub *membershipStoreStub) MembershipOrder(_ context.Context, id string) (domain.MembershipOrder, error) {
	if id != stub.order.ID {
		return domain.MembershipOrder{}, domain.ErrNotFound
	}
	return stub.order, nil
}
func (stub *membershipStoreStub) CreateMembershipOrder(_ context.Context, order domain.MembershipOrder) (domain.MembershipOrder, error) {
	order.AmountCents = 990
	order.Status = "pending"
	stub.order = order
	return order, nil
}
func (stub *membershipStoreStub) ConfirmMembershipPayment(_ context.Context, id, trade, pid string, amount int, _ time.Time) error {
	if id != stub.order.ID || trade != "trade" || pid != stub.order.ProviderPID || amount != 990 {
		return domain.ErrInvalidInput
	}
	stub.confirmed++
	return nil
}
func (stub *membershipStoreStub) AdjustMembership(_ context.Context, _ string, _ domain.MembershipAdjustment, event domain.AuditEvent) error {
	stub.adjusted = true
	stub.event = event
	return nil
}
func newMembershipService(t *testing.T) (*Service, *membershipStoreStub) {
	t.Helper()
	manager, err := security.New(make([]byte, 32), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	stub := &membershipStoreStub{member: domain.Membership{Tier: "none", BillingStarted: true}}
	service := NewService(stub, manager, nil, time.Hour, "", "https://share.example.test")
	_, err = service.AdminSavePaymentSettings(context.Background(), domain.User{ID: "admin", Role: domain.RoleAdmin}, PaymentSettingsInput{BaseURL: "https://pay.example.test", PID: "merchant", Key: "merchant-secret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return service, stub
}
func membershipCallback(id string) url.Values {
	values := url.Values{"pid": {"merchant"}, "out_trade_no": {id}, "trade_no": {"trade"}, "money": {"9.90"}, "trade_status": {"TRADE_SUCCESS"}, "sign_type": {"MD5"}}
	keys := make([]string, 0)
	for key := range values {
		if key != "sign_type" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0)
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	digest := md5.Sum([]byte(strings.Join(parts, "&") + "merchant-secret"))
	values.Set("sign", hex.EncodeToString(digest[:]))
	return values
}
func TestMembershipApplicationSecurity(t *testing.T) {
	service, stub := newMembershipService(t)
	ctx := context.Background()
	user := domain.User{ID: "user"}
	admin := domain.User{ID: "admin", Role: domain.RoleAdmin}
	if _, err := service.Membership(ctx, user, "other"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := service.MembershipOrders(ctx, user, "other"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	if err := service.AdjustMembership(ctx, user, "user", domain.MembershipAdjustment{}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := service.AdminPaymentSettings(ctx, user); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	view, err := service.AdminPaymentSettings(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "merchant-secret") || strings.Contains(string(encoded), "ciphertext") {
		t.Fatal("settings secret exposed")
	}
	checkout, err := service.CreateMembershipCheckout(ctx, user, "vip", "alipay")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(checkout.PayURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(checkout.Order.ID) != 32 || parsed.Query().Get("money") != "9.90" || parsed.Query().Get("return_url") != "https://share.example.test/api/payment/return" || !strings.Contains(parsed.Query().Get("name"), "VIP") {
		t.Fatal(checkout)
	}
	encoded, _ = json.Marshal(checkout)
	if strings.Contains(string(encoded), "merchant-secret") || strings.Contains(string(encoded), "ciphertext") {
		t.Fatal("checkout secret exposed")
	}
	if _, err = service.VerifyMembershipPayment(ctx, domain.User{ID: "other"}, checkout.Order.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	values := membershipCallback(checkout.Order.ID)
	if err = service.ValidateMembershipReturn(ctx, values); err != nil || stub.confirmed != 0 {
		t.Fatal("return fulfilled order", err)
	}
	_, err = service.AdminSavePaymentSettings(ctx, admin, PaymentSettingsInput{BaseURL: "https://new-pay.example.test", PID: "new", Key: "new-secret", Revision: stub.settings.Revision, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ConfirmEasyPay(ctx, values); err != nil || stub.confirmed != 1 {
		t.Fatal("rotation or disable broke old order", err)
	}
	values.Set("money", "0.01")
	if err = service.ConfirmEasyPay(ctx, values); !errors.Is(err, domain.ErrInvalidInput) || stub.confirmed != 1 {
		t.Fatal("tampered notification accepted", err)
	}
	if _, err = service.CreateMembershipCheckout(ctx, user, "vip", "alipay"); !errors.Is(err, domain.ErrPaymentUnavailable) {
		t.Fatal("disabled checkout allowed", err)
	}
	end := time.Now().Add(time.Hour)
	input := domain.MembershipAdjustment{Tier: "svip", ExpiresAt: &end, Reason: "补偿"}
	if err = service.AdjustMembership(ctx, admin, "user", input); err != nil || !stub.adjusted || !strings.Contains(string(stub.event.Metadata), "before") {
		t.Fatal("missing admin audit", err)
	}
	input.Reason = ""
	if err = service.AdjustMembership(ctx, admin, "user", input); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal(err)
	}
}
