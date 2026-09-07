package domain

import "time"

type Membership struct {
	UserID             string     `json:"user_id"`
	Tier               string     `json:"tier"`
	ExpiresAt          *time.Time `json:"expires_at"`
	Active             bool       `json:"active"`
	OwnerLimitOverride *int       `json:"owner_limit_override"`
	OwnerLimit         int        `json:"owner_limit"`
	OwnedPlans         int        `json:"owned_plans"`
	Revision           int64      `json:"revision"`
	Source             string     `json:"source"`
	BillingStarted     bool       `json:"billing_started"`
	PaymentEnabled     bool       `json:"payment_enabled"`
}

type MembershipOrder struct {
	ID                    string     `json:"id"`
	UserID                string     `json:"user_id"`
	Product               string     `json:"product"`
	AmountCents           int        `json:"amount_cents"`
	MembershipRevision    int64      `json:"-"`
	UpgradeExpiresAt      *time.Time `json:"upgrade_expires_at"`
	ProviderPID           string     `json:"-"`
	ProviderBaseURL       string     `json:"-"`
	ProviderKeyCiphertext []byte     `json:"-"`
	PaymentMethod         string     `json:"payment_method"`
	Status                string     `json:"status"`
	TradeNo               *string    `json:"trade_no"`
	CreatedAt             time.Time  `json:"created_at"`
	ExpiresAt             time.Time  `json:"expires_at"`
	PaidAt                *time.Time `json:"paid_at"`
	ServiceExpiresAt      *time.Time `json:"service_expires_at"`
}

type MembershipAdjustment struct {
	Tier               string     `json:"tier"`
	ExpiresAt          *time.Time `json:"expires_at"`
	OwnerLimitOverride *int       `json:"owner_limit_override"`
	Revision           int64      `json:"revision"`
	Reason             string     `json:"reason"`
	ReviewOrderID      string     `json:"review_order_id"`
}
