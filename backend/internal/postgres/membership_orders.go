package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

const membershipOrderColumns = `id,user_id,product,amount_cents,membership_revision,upgrade_expires_at,provider_pid,provider_base_url,provider_key_ciphertext,payment_method,status,trade_no,created_at,expires_at,paid_at,service_expires_at`

func scanMembershipOrder(row pgx.Row) (domain.MembershipOrder, error) {
	var order domain.MembershipOrder
	err := row.Scan(&order.ID, &order.UserID, &order.Product, &order.AmountCents, &order.MembershipRevision, &order.UpgradeExpiresAt, &order.ProviderPID, &order.ProviderBaseURL, &order.ProviderKeyCiphertext, &order.PaymentMethod, &order.Status, &order.TradeNo, &order.CreatedAt, &order.ExpiresAt, &order.PaidAt, &order.ServiceExpiresAt)
	return order, mapError(err)
}

func (s *Store) CreateMembershipOrder(ctx context.Context, order domain.MembershipOrder) (domain.MembershipOrder, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return order, err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 AND status='active' FOR UPDATE`, order.UserID).Scan(&userID); err != nil {
		return order, mapError(err)
	}
	member, err := readMembership(ctx, tx, userID, order.CreatedAt)
	if err != nil {
		return order, err
	}
	var enabled bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payment_settings WHERE enabled)`).Scan(&enabled); err != nil {
		return order, err
	}
	if !enabled || !member.BillingStarted {
		return order, domain.ErrPaymentUnavailable
	}
	switch order.Product {
	case "vip":
		if member.Active && member.Tier == "svip" {
			return order, domain.ErrMembershipProduct
		}
		order.AmountCents = 990
	case "svip":
		if member.Active && member.Tier == "vip" {
			return order, domain.ErrMembershipProduct
		}
		order.AmountCents = 1990
	case "upgrade":
		if !member.Active || member.Tier != "vip" {
			return order, domain.ErrMembershipProduct
		}
		order.AmountCents = 1000
		order.UpgradeExpiresAt = member.ExpiresAt
	default:
		return order, domain.ErrInvalidInput
	}
	order.MembershipRevision = member.Revision
	if _, err = tx.Exec(ctx, `UPDATE membership_payment_orders SET status='expired' WHERE user_id=$1 AND status='pending' AND expires_at<=$2`, userID, order.CreatedAt); err != nil {
		return order, err
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM membership_payment_orders WHERE user_id=$1 AND status='pending')`, userID).Scan(&pending); err != nil {
		return order, err
	}
	if pending {
		existing, readErr := scanMembershipOrder(tx.QueryRow(ctx, `SELECT `+membershipOrderColumns+` FROM membership_payment_orders WHERE user_id=$1 AND status='pending'`, userID))
		if readErr != nil {
			return order, readErr
		}
		if existing.Product != order.Product || (order.Product == "upgrade" && existing.MembershipRevision != member.Revision) {
			return order, domain.ErrPendingMembershipOrder
		}
		return existing, tx.Commit(ctx)
	}
	order, err = scanMembershipOrder(tx.QueryRow(ctx, `INSERT INTO membership_payment_orders(id,user_id,product,amount_cents,membership_revision,upgrade_expires_at,provider_pid,provider_base_url,provider_key_ciphertext,payment_method,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+membershipOrderColumns, order.ID, userID, order.Product, order.AmountCents, order.MembershipRevision, order.UpgradeExpiresAt, order.ProviderPID, order.ProviderBaseURL, order.ProviderKeyCiphertext, order.PaymentMethod, order.CreatedAt, order.ExpiresAt))
	if err != nil {
		return order, err
	}
	return order, tx.Commit(ctx)
}

func (s *Store) MembershipOrder(ctx context.Context, id string) (domain.MembershipOrder, error) {
	return scanMembershipOrder(s.pool.QueryRow(ctx, `SELECT `+membershipOrderColumns+` FROM membership_payment_orders WHERE id=$1`, id))
}
func (s *Store) MembershipOrders(ctx context.Context, userID string) ([]domain.MembershipOrder, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+membershipOrderColumns+` FROM membership_payment_orders WHERE user_id=$1 ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	orders := make([]domain.MembershipOrder, 0)
	for rows.Next() {
		order, err := scanMembershipOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}
func (s *Store) CancelMembershipOrder(ctx context.Context, id, userID string) error {
	result, err := s.pool.Exec(ctx, `UPDATE membership_payment_orders SET status='expired' WHERE id=$1 AND user_id=$2 AND status='pending'`, id, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}
func (s *Store) ClaimMembershipQuery(ctx context.Context, id string, now time.Time) (bool, error) {
	result, err := s.pool.Exec(ctx, `UPDATE membership_payment_orders SET last_checked_at=$2::timestamptz WHERE id=$1 AND status IN ('pending','expired') AND (last_checked_at IS NULL OR last_checked_at<$2::timestamptz-INTERVAL '15 seconds')`, id, now)
	return result.RowsAffected() == 1, err
}
func (s *Store) MembershipReconciliationBatch(ctx context.Context, now time.Time) ([]domain.MembershipOrder, error) {
	if _, err := s.pool.Exec(ctx, `UPDATE membership_payment_orders SET status='expired' WHERE status='pending' AND expires_at<=$1`, now); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `UPDATE membership_payment_orders SET last_checked_at=$1::timestamptz WHERE id IN (SELECT id FROM membership_payment_orders WHERE status IN ('pending','expired') AND created_at>$1::timestamptz-INTERVAL '7 days' AND (last_checked_at IS NULL OR last_checked_at<$1::timestamptz-INTERVAL '5 minutes') ORDER BY last_checked_at NULLS FIRST,created_at LIMIT 20 FOR UPDATE SKIP LOCKED) RETURNING `+membershipOrderColumns, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	orders := make([]domain.MembershipOrder, 0)
	for rows.Next() {
		order, err := scanMembershipOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}
