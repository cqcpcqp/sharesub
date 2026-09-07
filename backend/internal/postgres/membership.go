package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

type membershipQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readMembership(ctx context.Context, query membershipQuerier, userID string, now time.Time) (domain.Membership, error) {
	var value domain.Membership
	err := query.QueryRow(ctx, `SELECT u.id,COALESCE(m.tier,'none'),m.expires_at,m.owner_limit_override,COALESCE(m.revision,0),COALESCE(m.source,'none'),EXISTS(SELECT 1 FROM membership_rollout),(SELECT count(*) FROM shared_plans p WHERE p.owner_user_id=u.id AND p.status<>'archived') FROM users u LEFT JOIN user_memberships m ON m.user_id=u.id WHERE u.id=$1`, userID).Scan(&value.UserID, &value.Tier, &value.ExpiresAt, &value.OwnerLimitOverride, &value.Revision, &value.Source, &value.BillingStarted, &value.OwnedPlans)
	value.Active = value.Tier != "none" && value.ExpiresAt != nil && value.ExpiresAt.After(now)
	value.OwnerLimit = 2
	if value.OwnerLimitOverride != nil {
		value.OwnerLimit = *value.OwnerLimitOverride
	}
	return value, mapError(err)
}

func (s *Store) Membership(ctx context.Context, userID string, now time.Time) (domain.Membership, error) {
	return readMembership(ctx, s.pool, userID, now)
}

func requireOwnerMembership(ctx context.Context, tx pgx.Tx, userID string, addingPlan bool) error {
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE`, userID).Scan(&locked); err != nil {
		return mapError(err)
	}
	value, err := readMembership(ctx, tx, userID, time.Now())
	if err != nil {
		return err
	}
	if !value.BillingStarted {
		return nil
	}
	if !value.Active || value.Tier != "svip" {
		return domain.ErrSVIPRequired
	}
	if addingPlan && value.OwnedPlans >= value.OwnerLimit {
		return domain.ErrOwnerLimit
	}
	return nil
}

func activateMemberships(ctx context.Context, tx pgx.Tx, now time.Time) error {
	var started bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM membership_rollout)`).Scan(&started); err != nil {
		return err
	}
	if started {
		return nil
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE shared_plans,plan_members IN SHARE MODE`); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `INSERT INTO membership_rollout(singleton,started_at) VALUES(true,$1) ON CONFLICT DO NOTHING`, now)
	if err != nil || result.RowsAffected() == 0 {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_memberships(user_id,tier,expires_at,owner_limit_override,revision,source,updated_at)
	SELECT u.id,CASE WHEN owned.total>0 THEN 'svip' ELSE 'vip' END,$1::timestamptz+INTERVAL '720 hours',CASE WHEN owned.total>2 THEN owned.total::integer ELSE NULL END,1,'rollout',$1
	FROM users u CROSS JOIN LATERAL (SELECT count(*) total FROM shared_plans p WHERE p.owner_user_id=u.id AND p.status<>'archived') owned
	WHERE owned.total>0 OR EXISTS(SELECT 1 FROM plan_members pm JOIN shared_plans p ON p.id=pm.plan_id WHERE pm.user_id=u.id AND pm.status='active' AND p.status<>'archived')
	ON CONFLICT(user_id) DO UPDATE SET tier=CASE WHEN user_memberships.tier='svip' AND user_memberships.expires_at>$1 THEN 'svip' ELSE EXCLUDED.tier END,expires_at=GREATEST(user_memberships.expires_at,EXCLUDED.expires_at),owner_limit_override=CASE WHEN EXCLUDED.owner_limit_override IS NULL THEN user_memberships.owner_limit_override ELSE GREATEST(user_memberships.owner_limit_override,EXCLUDED.owner_limit_override) END,revision=user_memberships.revision+1,source='rollout',updated_at=$1`, now)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO notifications(id,user_id,type,title,body,resource_type,resource_id,created_at) SELECT 'membership-rollout:'||user_id,user_id,'membership_granted','已获赠 30 天平台会员',CASE WHEN tier='svip' THEN '已赠送 SVIP；房主名额可在会员中心查看。' ELSE '已赠送 VIP；到期后仅停止你自己的调用。' END,'membership',user_id,$1 FROM user_memberships WHERE source='rollout' AND updated_at=$1`, now)
	return err
}

func (s *Store) AdjustMembership(ctx context.Context, userID string, input domain.MembershipAdjustment, event domain.AuditEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&locked); err != nil {
		return mapError(err)
	}
	value, err := readMembership(ctx, tx, userID, event.CreatedAt)
	if err != nil {
		return err
	}
	if value.Revision != input.Revision {
		return domain.ErrConflict
	}
	if input.ReviewOrderID != "" {
		result, updateErr := tx.Exec(ctx, `UPDATE membership_payment_orders SET status='paid',service_expires_at=$3 WHERE id=$1 AND user_id=$2 AND status='review_required'`, input.ReviewOrderID, userID, input.ExpiresAt)
		if updateErr != nil {
			return updateErr
		}
		if result.RowsAffected() != 1 {
			return domain.ErrConflict
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_memberships(user_id,tier,expires_at,owner_limit_override,revision,source,updated_at) VALUES($1,$2,$3,$4,1,'admin',$5) ON CONFLICT(user_id) DO UPDATE SET tier=$2,expires_at=$3,owner_limit_override=$4,revision=user_memberships.revision+1,source='admin',updated_at=$5`, userID, input.Tier, input.ExpiresAt, input.OwnerLimitOverride, event.CreatedAt)
	if err != nil {
		return err
	}
	if err = insertAuditEvent(ctx, tx, event); err != nil {
		return err
	}
	if err = insertNotification(ctx, tx, event.ID, userID, "membership_adjusted", "会员权益已调整", "管理员已调整你的会员或房主名额，请在会员中心查看。此操作不执行扣款或退款。", "membership", userID, event.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SendMembershipReminders(ctx context.Context, now time.Time) error {
	_, err := s.pool.Exec(ctx, `WITH due AS (INSERT INTO membership_reminders(user_id,expires_at,days_before) SELECT user_id,expires_at,CASE WHEN expires_at<=$1 THEN 0 WHEN expires_at<=$1+INTERVAL '1 day' THEN 1 ELSE 3 END FROM user_memberships WHERE tier<>'none' AND expires_at<=$1+INTERVAL '3 days' AND EXISTS(SELECT 1 FROM membership_rollout) ON CONFLICT DO NOTHING RETURNING *) INSERT INTO notifications(id,user_id,type,title,body,resource_type,resource_id,created_at) SELECT 'membership:'||user_id||':'||extract(epoch FROM expires_at)::text||':'||days_before::text,user_id,'membership_expiring',CASE WHEN days_before=0 THEN '平台会员已到期' ELSE '平台会员即将到期' END,'请前往个人设置中的会员中心续费。到期仅停止你自己的调用，不影响其他有效会员。','membership',user_id,$1 FROM due`, now)
	return err
}
