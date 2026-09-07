package domain

import (
	"errors"
	"fmt"
)

var (
	ErrMembershipRequired        = errors.New("平台会员未开通或已到期，请续费 VIP 或 SVIP；仅影响当前用户")
	ErrSVIPRequired              = errors.New("此操作需要有效 SVIP；已有 Plan 和成员不受影响")
	ErrOwnerLimit                = errors.New("未归档 Plan 房主数量已达到个人上限")
	ErrMembershipProduct         = errors.New("当前会员状态不允许购买此商品；有效 SVIP 不支持降级，VIP 请使用补差升级")
	ErrPendingMembershipOrder    = errors.New("已有其他商品的待付款订单，请先核实或取消该订单")
	ErrPaymentUnavailable        = errors.New("支付暂不可用，请联系管理员")
	ErrNotFound                  = errors.New("resource not found")
	ErrUnauthorized              = errors.New("authentication required")
	ErrForbidden                 = errors.New("operation forbidden")
	ErrPasswordChangeRequired    = errors.New("password change required")
	ErrEmailVerificationRequired = errors.New("email verification required")
	ErrEmailVerificationInvalid  = errors.New("email verification link is invalid or expired")
	ErrEmailDeliveryUnavailable  = errors.New("verification email could not be sent")
	ErrEmailResendTooSoon        = errors.New("verification email was sent too recently")
	ErrEmailVerificationLimited  = errors.New("too many verification emails requested")
	ErrConflict                  = errors.New("resource conflict")
	ErrInvalidInput              = errors.New("invalid input")
	ErrShareExceeded             = errors.New("allocated shares exceed 100 percent")
	ErrQuotaExhausted            = errors.New("member quota exhausted")
	ErrAccountUnavailable        = errors.New("OpenAI account unavailable")
	ErrAccountTokenRefresh       = errors.New("OpenAI account token refresh failed")
	ErrNoRouteAvailable          = errors.New("no configured Plan has available quota")
	ErrPublicPlanFull            = errors.New("public Plan has no available seats")
	ErrAccountConcurrency        = errors.New("OpenAI account concurrency limit reached")
	ErrAccountRateLimited        = errors.New("OpenAI account RPM limit reached")
	ErrAccountAlreadyBound       = fmt.Errorf("OpenAI account is already bound to another Plan: %w", ErrConflict)
	ErrQuotaResetUnavailable     = fmt.Errorf("no quota reset credit is currently available: %w", ErrConflict)
)
