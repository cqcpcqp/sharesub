package postgres

import (
	"context"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Store) ConfirmMembershipPayment(ctx context.Context, id, tradeNo, pid string, amount int, now time.Time) error {
	if tradeNo == "" {
		return domain.ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	if err = tx.QueryRow(ctx, `SELECT u.id FROM users u JOIN membership_payment_orders o ON o.user_id=u.id WHERE o.id=$1 FOR UPDATE OF u`, id).Scan(&userID); err != nil {
		return mapError(err)
	}
	order, err := scanMembershipOrder(tx.QueryRow(ctx, `SELECT `+membershipOrderColumns+` FROM membership_payment_orders WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if order.ProviderPID != pid || order.AmountCents != amount {
		return domain.ErrInvalidInput
	}
	if order.Status == "paid" || order.Status == "review_required" {
		if *order.TradeNo != tradeNo {
			return domain.ErrConflict
		}
		return nil
	}
	member, err := readMembership(ctx, tx, userID, now)
	if err != nil {
		return err
	}
	status, tier := "paid", order.Product
	var expiresAt *time.Time
	if order.Product == "upgrade" {
		tier = "svip"
		if !member.Active || member.Tier != "vip" || member.Revision != order.MembershipRevision || order.UpgradeExpiresAt == nil || !order.UpgradeExpiresAt.After(now) {
			status = "review_required"
		} else {
			expiresAt = order.UpgradeExpiresAt
		}
	} else if member.Active && member.Tier != tier {
		status = "review_required"
	} else {
		start := now
		if member.Active {
			start = *member.ExpiresAt
		}
		end := start.Add(720 * time.Hour)
		expiresAt = &end
	}
	if status == "paid" {
		_, err = tx.Exec(ctx, `INSERT INTO user_memberships(user_id,tier,expires_at,revision,source,updated_at) VALUES($1,$2,$3,1,'payment',$4) ON CONFLICT(user_id) DO UPDATE SET tier=$2,expires_at=$3,revision=user_memberships.revision+1,source='payment',updated_at=$4`, userID, tier, expiresAt, now)
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE membership_payment_orders SET status=$2,trade_no=$3,paid_at=$4,service_expires_at=$5 WHERE id=$1`, id, status, tradeNo, now, expiresAt); err != nil {
		return mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_user_id,action,resource_type,resource_id,metadata,created_at) VALUES($1,$2,'membership.payment_confirmed','membership',$2,jsonb_build_object('order_id',$3::text,'product',$4::text,'amount_cents',$5::integer,'status',$6::text),$7)`, "membership-payment:"+id, userID, id, order.Product, amount, status, now); err != nil {
		return err
	}
	title, body := "会员付款已到账", "会员权益已更新，请在个人设置的会员中心查看。"
	if status == "review_required" {
		title = "付款已到账，需人工处理"
		body = "付款期间会员状态或升级期限发生变化，未覆盖现有权益。请凭订单号联系管理员处理。"
	}
	if err = insertNotification(ctx, tx, "membership-payment:"+id, userID, "membership_payment", title, body, "membership", userID, now); err != nil {
		return err
	}
	if status == "review_required" {
		_, err = tx.Exec(ctx, `INSERT INTO notifications(id,user_id,type,title,body,resource_type,resource_id,created_at) SELECT 'membership-review:'||$1||':'||id,id,'membership_review','会员付款待人工处理','用户付款已到账，但会员状态变化，需要核对订单并人工处理。','membership',$2,$3 FROM users WHERE role='admin' AND id<>$2`, id, userID, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
