package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

const quotaResetExecutionLeaseTTL = 15 * time.Minute

func quotaResetVotePassed(mode string, supportCount, supportWeight, eligibleCount int) bool {
	if mode == "shared" {
		return supportCount*2 > eligibleCount
	}
	return supportWeight > 5000
}

func (s *Store) QuotaResetVote(ctx context.Context, planID, userID string, now time.Time) (*domain.QuotaResetVote, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var viewerExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM plan_members WHERE plan_id=$1 AND user_id=$2 AND status='active')`, planID, userID).Scan(&viewerExists); err != nil {
		return nil, err
	}
	if !viewerExists {
		return nil, domain.ErrForbidden
	}
	if err := reconcileQuotaResetVote(ctx, tx, planID, now); err != nil {
		return nil, err
	}
	vote, err := loadQuotaResetVote(ctx, tx, planID, "", userID)
	if err != nil && err != domain.ErrNotFound {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if err == domain.ErrNotFound {
		return nil, nil
	}
	return &vote, nil
}

func (s *Store) CreateQuotaResetVote(ctx context.Context, vote domain.QuotaResetVote, userID string, event domain.AuditEvent) (domain.QuotaResetVote, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	defer tx.Rollback(ctx)
	var status, mode, planName string
	var accountID *string
	var accountBindingGeneration int64
	if err := tx.QueryRow(ctx, `SELECT status,allocation_mode,name,account_id,account_binding_generation FROM shared_plans WHERE id=$1 FOR UPDATE`, vote.PlanID).Scan(&status, &mode, &planName, &accountID, &accountBindingGeneration); err != nil {
		return domain.QuotaResetVote{}, false, mapError(err)
	}
	if status != domain.StatusActive || accountID == nil {
		return domain.QuotaResetVote{}, false, domain.ErrConflict
	}
	if err := reconcileQuotaResetVote(ctx, tx, vote.PlanID, vote.CreatedAt); err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	if err := rejectActiveQuotaResetExecution(ctx, tx, vote.PlanID, vote.CreatedAt); err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	type eligibleMember struct {
		memberID, userID, username, avatarURL string
		weight                                int
	}
	rows, err := tx.Query(ctx, `
		SELECT m.id,m.user_id,u.username,u.avatar_updated_at,m.share_basis_points
		FROM plan_members m JOIN users u ON u.id=m.user_id
		WHERE m.plan_id=$1 AND m.status='active' AND ($2='shared' OR m.share_basis_points>0)
		ORDER BY CASE WHEN m.role='owner' THEN 0 ELSE 1 END,m.created_at`, vote.PlanID, mode)
	if err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	eligible := make([]eligibleMember, 0)
	initiatorFound := false
	eligibleWeight := 0
	for rows.Next() {
		var item eligibleMember
		var avatarUpdatedAt *time.Time
		if err := rows.Scan(&item.memberID, &item.userID, &item.username, &avatarUpdatedAt, &item.weight); err != nil {
			rows.Close()
			return domain.QuotaResetVote{}, false, err
		}
		item.avatarURL = userAvatarURL(item.userID, avatarUpdatedAt)
		if mode == "shared" {
			item.weight = 0
		}
		eligibleWeight += item.weight
		if item.userID == userID {
			vote.InitiatorMemberID = item.memberID
			initiatorFound = true
		}
		eligible = append(eligible, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.QuotaResetVote{}, false, err
	}
	rows.Close()
	if !initiatorFound {
		return domain.QuotaResetVote{}, false, domain.ErrForbidden
	}
	if mode == "fixed" && eligibleWeight <= 5000 {
		return domain.QuotaResetVote{}, false, domain.ErrConflict
	}
	vote.AllocationMode = mode
	vote.EligibleCount = len(eligible)
	vote.EligibleWeightBasisPoints = eligibleWeight
	if _, err := tx.Exec(ctx, `
		INSERT INTO quota_reset_votes(id,plan_id,account_id,account_binding_generation,initiator_member_id,initiator_user_id,allocation_mode,status,eligible_count,eligible_weight_basis_points,created_at,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,'active',$8,$9,$10,$11)`, vote.ID, vote.PlanID, *accountID, accountBindingGeneration, vote.InitiatorMemberID, userID, mode, len(eligible), eligibleWeight, vote.CreatedAt, vote.ExpiresAt); err != nil {
		return domain.QuotaResetVote{}, false, mapError(err)
	}
	for _, item := range eligible {
		choice := any(nil)
		votedAt := any(nil)
		if item.userID == userID {
			choice = "support"
			votedAt = vote.CreatedAt
		}
		if _, err := tx.Exec(ctx, `INSERT INTO quota_reset_vote_members(vote_id,member_id,user_id,username,avatar_url,weight_basis_points,choice,voted_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, vote.ID, item.memberID, item.userID, item.username, item.avatarURL, item.weight, choice, votedAt); err != nil {
			return domain.QuotaResetVote{}, false, err
		}
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO notifications(id,user_id,type,title,body,resource_type,resource_id,created_at)
		SELECT $1 || ':' || v.user_id,v.user_id,'quota_reset_vote_started','新的额度重置投票',$2 || ' 的投票将在 2 小时后截止，通过后会自动消耗一次重置机会','plan',$3,$4
		FROM quota_reset_vote_members v WHERE v.vote_id=$5 AND v.user_id<>$6`, event.ID, planName, vote.PlanID, vote.CreatedAt, vote.ID, userID); err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	passed := quotaResetVotePassed(mode, 1, func() int {
		for _, item := range eligible {
			if item.userID == userID {
				return item.weight
			}
		}
		return 0
	}(), len(eligible))
	if passed {
		if _, err := tx.Exec(ctx, `UPDATE quota_reset_votes SET status='executing',execution_started_at=$2 WHERE id=$1`, vote.ID, vote.CreatedAt); err != nil {
			return domain.QuotaResetVote{}, false, err
		}
	}
	out, err := loadQuotaResetVote(ctx, tx, vote.PlanID, vote.ID, userID)
	if err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	return out, passed, tx.Commit(ctx)
}

func (s *Store) CastQuotaResetVote(ctx context.Context, planID, voteID, userID, choice string, now time.Time, passedEvent domain.AuditEvent) (domain.QuotaResetVote, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	defer tx.Rollback(ctx)
	if err := reconcileQuotaResetVote(ctx, tx, planID, now); err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	var status, mode string
	var expiresAt time.Time
	var eligibleCount int
	if err := tx.QueryRow(ctx, `SELECT status,allocation_mode,eligible_count,expires_at FROM quota_reset_votes WHERE id=$1 AND plan_id=$2 FOR UPDATE`, voteID, planID).Scan(&status, &mode, &eligibleCount, &expiresAt); err != nil {
		return domain.QuotaResetVote{}, false, mapError(err)
	}
	if status != "active" || !expiresAt.After(now) {
		// Reconciliation may have expired or cancelled this vote in the current
		// transaction. Commit that terminal state before reporting the conflict.
		if err := tx.Commit(ctx); err != nil {
			return domain.QuotaResetVote{}, false, err
		}
		return domain.QuotaResetVote{}, false, domain.ErrConflict
	}
	result, err := tx.Exec(ctx, `UPDATE quota_reset_vote_members SET choice=$4,voted_at=$5 WHERE vote_id=$1 AND user_id=$2 AND EXISTS(SELECT 1 FROM plan_members m WHERE m.id=quota_reset_vote_members.member_id AND m.plan_id=$3 AND m.status='active')`, voteID, userID, planID, choice, now)
	if err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	if result.RowsAffected() != 1 {
		return domain.QuotaResetVote{}, false, domain.ErrForbidden
	}
	var supportCount, supportWeight int
	if err := tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum(weight_basis_points),0) FROM quota_reset_vote_members WHERE vote_id=$1 AND choice='support'`, voteID).Scan(&supportCount, &supportWeight); err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	passed := quotaResetVotePassed(mode, supportCount, supportWeight, eligibleCount)
	if passed {
		if _, err := tx.Exec(ctx, `UPDATE quota_reset_votes SET status='executing',execution_started_at=$2 WHERE id=$1 AND status='active'`, voteID, now); err != nil {
			return domain.QuotaResetVote{}, false, err
		}
		if err := insertAuditEvent(ctx, tx, passedEvent); err != nil {
			return domain.QuotaResetVote{}, false, err
		}
	}
	out, err := loadQuotaResetVote(ctx, tx, planID, voteID, userID)
	if err != nil {
		return domain.QuotaResetVote{}, false, err
	}
	return out, passed, tx.Commit(ctx)
}

func (s *Store) CompleteQuotaResetVote(ctx context.Context, voteID, status string, windowsReset int, resultCode string, now time.Time, event domain.AuditEvent) (domain.QuotaResetVote, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.QuotaResetVote{}, err
	}
	defer tx.Rollback(ctx)
	var planID string
	if err := tx.QueryRow(ctx, `UPDATE quota_reset_votes SET status=$2,windows_reset=$3,result_code=$4,completed_at=$5 WHERE id=$1 AND status='executing' RETURNING plan_id`, voteID, status, windowsReset, resultCode, now).Scan(&planID); err != nil {
		return domain.QuotaResetVote{}, mapError(err)
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.QuotaResetVote{}, err
	}
	title, body := "额度重置已完成", "投票已通过，系统已自动使用 1 次重置机会"
	if status == "succeeded_unsynced" {
		title, body = "额度重置已完成，数据待同步", "请稍后查询额度更新显示，不要重复发起重置"
	} else if status == "outcome_unknown" {
		title, body = "额度重置结果待确认", "OpenAI 返回结果无法确认，请先查询剩余重置次数，不要重复操作"
	} else if status == "cancelled" {
		title, body = "额度重置投票执行失败", "系统未开始消费重置机会，请刷新 Plan 后重新发起投票"
	}
	if err := notifyPlanMembers(ctx, tx, event.ID, planID, "", "quota_reset_vote_completed", title, body, now); err != nil {
		return domain.QuotaResetVote{}, err
	}
	out, err := loadQuotaResetVote(ctx, tx, planID, voteID, event.ActorUserID)
	if err != nil {
		return domain.QuotaResetVote{}, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) ReserveManualQuotaReset(ctx context.Context, lease domain.QuotaResetExecutionLease, reason string, event domain.AuditEvent) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var accountID *string
	var generation int64
	if err := tx.QueryRow(ctx, `SELECT status,account_id,account_binding_generation FROM shared_plans WHERE id=$1 FOR UPDATE`, lease.PlanID).Scan(&status, &accountID, &generation); err != nil {
		return mapError(err)
	}
	if status != domain.StatusActive || accountID == nil || *accountID != lease.AccountID || generation != lease.AccountBindingGeneration {
		return domain.ErrConflict
	}
	if err := reconcileQuotaResetVote(ctx, tx, lease.PlanID, lease.AcquiredAt); err != nil {
		return err
	}
	if err := rejectActiveQuotaResetExecution(ctx, tx, lease.PlanID, lease.AcquiredAt); err != nil {
		return err
	}
	var executing bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quota_reset_votes WHERE plan_id=$1 AND status='executing')`, lease.PlanID).Scan(&executing); err != nil {
		return err
	}
	if executing {
		return domain.ErrConflict
	}
	result, err := tx.Exec(ctx, `UPDATE quota_reset_votes SET status='cancelled',result_code=$2,completed_at=$3 WHERE plan_id=$1 AND status='active'`, lease.PlanID, reason, lease.AcquiredAt)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO quota_reset_execution_leases(plan_id,operation_id,account_id,account_binding_generation,vote_id,acquired_at) VALUES($1,$2,$3,$4,NULL,$5)`, lease.PlanID, lease.OperationID, lease.AccountID, lease.AccountBindingGeneration, lease.AcquiredAt); err != nil {
		return mapError(err)
	}
	if result.RowsAffected() != 0 {
		if err := insertAuditEvent(ctx, tx, event); err != nil {
			return err
		}
		if err := notifyPlanMembers(ctx, tx, event.ID, lease.PlanID, "", "quota_reset_vote_cancelled", "额度重置投票已取消", "房主或管理员已直接执行额度重置，本次投票不再有效", lease.AcquiredAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ReserveVotedQuotaReset(ctx context.Context, lease domain.QuotaResetExecutionLease) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var accountID *string
	var generation int64
	if err := tx.QueryRow(ctx, `SELECT status,account_id,account_binding_generation FROM shared_plans WHERE id=$1 FOR UPDATE`, lease.PlanID).Scan(&status, &accountID, &generation); err != nil {
		return mapError(err)
	}
	if status != domain.StatusActive || accountID == nil || *accountID != lease.AccountID || generation != lease.AccountBindingGeneration {
		return domain.ErrConflict
	}
	if err := rejectActiveQuotaResetExecution(ctx, tx, lease.PlanID, lease.AcquiredAt); err != nil {
		return err
	}
	var voteExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM quota_reset_votes
		WHERE id=$1 AND plan_id=$2 AND account_id=$3 AND account_binding_generation=$4 AND status='executing'
	)`, lease.VoteID, lease.PlanID, lease.AccountID, lease.AccountBindingGeneration).Scan(&voteExists); err != nil {
		return err
	}
	if !voteExists {
		return domain.ErrConflict
	}
	if _, err := tx.Exec(ctx, `INSERT INTO quota_reset_execution_leases(plan_id,operation_id,account_id,account_binding_generation,vote_id,acquired_at) VALUES($1,$2,$3,$4,$5,$6)`, lease.PlanID, lease.OperationID, lease.AccountID, lease.AccountBindingGeneration, lease.VoteID, lease.AcquiredAt); err != nil {
		return mapError(err)
	}
	return tx.Commit(ctx)
}

func (s *Store) ReleaseQuotaResetExecution(ctx context.Context, planID, operationID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM quota_reset_execution_leases WHERE plan_id=$1 AND operation_id=$2`, planID, operationID)
	return err
}

func rejectActiveQuotaResetExecution(ctx context.Context, tx pgx.Tx, planID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM quota_reset_execution_leases WHERE plan_id=$1 AND acquired_at<=$2::timestamptz`, planID, now.Add(-quotaResetExecutionLeaseTTL)); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quota_reset_execution_leases WHERE plan_id=$1)`, planID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return domain.ErrConflict
	}
	return nil
}

func reconcileQuotaResetVote(ctx context.Context, tx pgx.Tx, planID string, now time.Time) error {
	var interruptedVoteID string
	err := tx.QueryRow(ctx, `
		UPDATE quota_reset_votes SET status='outcome_unknown',result_code='execution_interrupted',completed_at=$2
		WHERE plan_id=$1 AND status='executing' AND execution_started_at<=$2::timestamptz-INTERVAL '15 minutes'
		RETURNING id`, planID, now).Scan(&interruptedVoteID)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil {
		if _, err := tx.Exec(ctx, `INSERT INTO audit_events(id,actor_user_id,action,resource_type,resource_id,metadata,created_at) VALUES($1,NULL,'plan.quota_reset_vote_interrupted','plan',$2,'{}',$3) ON CONFLICT DO NOTHING`, interruptedVoteID+":interrupted", planID, now); err != nil {
			return err
		}
		if err := notifyPlanMembers(ctx, tx, interruptedVoteID+":interrupted", planID, "", "quota_reset_vote_interrupted", "额度重置结果待确认", "执行记录意外中断，请先查询剩余重置次数，不要重复操作", now); err != nil {
			return err
		}
	}
	var expiredVoteID string
	err = tx.QueryRow(ctx, `UPDATE quota_reset_votes SET status='expired',completed_at=$2 WHERE plan_id=$1 AND status='active' AND expires_at<=$2 RETURNING id`, planID, now).Scan(&expiredVoteID)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil {
		if _, err := tx.Exec(ctx, `INSERT INTO audit_events(id,actor_user_id,action,resource_type,resource_id,metadata,created_at) VALUES($1,NULL,'plan.quota_reset_vote_expired','plan',$2,'{}',$3) ON CONFLICT DO NOTHING`, expiredVoteID+":expired", planID, now); err != nil {
			return err
		}
		if err := notifyPlanMembers(ctx, tx, expiredVoteID+":expired", planID, "", "quota_reset_vote_expired", "额度重置投票已过期", "两小时内未达到严格多数，本次没有消耗重置机会", now); err != nil {
			return err
		}
	}
	var cancelledVoteID string
	err = tx.QueryRow(ctx, `
		UPDATE quota_reset_votes v SET status='cancelled',result_code='plan_changed',completed_at=$2
		WHERE v.plan_id=$1 AND v.status='active' AND (
			NOT EXISTS (
				SELECT 1 FROM shared_plans p
				WHERE p.id=v.plan_id AND p.status='active' AND p.account_id=v.account_id
					AND p.account_binding_generation=v.account_binding_generation AND p.allocation_mode=v.allocation_mode
			)
			OR EXISTS (
				(SELECT m.id,CASE WHEN v.allocation_mode='shared' THEN 0 ELSE m.share_basis_points END
				 FROM plan_members m WHERE m.plan_id=v.plan_id AND m.status='active' AND (v.allocation_mode='shared' OR m.share_basis_points>0)
				 EXCEPT SELECT vm.member_id,vm.weight_basis_points FROM quota_reset_vote_members vm WHERE vm.vote_id=v.id)
				UNION ALL
				(SELECT vm.member_id,vm.weight_basis_points FROM quota_reset_vote_members vm WHERE vm.vote_id=v.id
				 EXCEPT SELECT m.id,CASE WHEN v.allocation_mode='shared' THEN 0 ELSE m.share_basis_points END
				 FROM plan_members m WHERE m.plan_id=v.plan_id AND m.status='active' AND (v.allocation_mode='shared' OR m.share_basis_points>0))
			)
		) RETURNING v.id`, planID, now).Scan(&cancelledVoteID)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil {
		if _, err := tx.Exec(ctx, `INSERT INTO audit_events(id,actor_user_id,action,resource_type,resource_id,metadata,created_at) VALUES($1,NULL,'plan.quota_reset_vote_cancelled','plan',$2,'{"reason":"plan_changed"}',$3) ON CONFLICT DO NOTHING`, cancelledVoteID+":cancelled", planID, now); err != nil {
			return err
		}
		if err := notifyPlanMembers(ctx, tx, cancelledVoteID+":cancelled", planID, "", "quota_reset_vote_cancelled", "额度重置投票已取消", "Plan 状态或成员配置发生变化，本次投票不再有效", now); err != nil {
			return err
		}
	}
	return nil
}

func loadQuotaResetVote(ctx context.Context, tx pgx.Tx, planID, voteID, currentUserID string) (domain.QuotaResetVote, error) {
	query := `
		SELECT v.id,v.plan_id,v.initiator_member_id,v.initiator_user_id,im.username,v.allocation_mode,v.status,
			v.eligible_count,v.eligible_weight_basis_points,v.windows_reset,v.result_code,v.created_at,v.expires_at,v.execution_started_at,v.completed_at
		FROM quota_reset_votes v JOIN quota_reset_vote_members im ON im.vote_id=v.id AND im.member_id=v.initiator_member_id
		WHERE v.plan_id=$1`
	args := []any{planID}
	if voteID != "" {
		query += ` AND v.id=$2`
		args = append(args, voteID)
	}
	query += ` ORDER BY v.created_at DESC LIMIT 1`
	var out domain.QuotaResetVote
	err := tx.QueryRow(ctx, query, args...).Scan(&out.ID, &out.PlanID, &out.InitiatorMemberID, &out.InitiatorUserID, &out.InitiatorUsername, &out.AllocationMode, &out.Status, &out.EligibleCount, &out.EligibleWeightBasisPoints, &out.WindowsReset, &out.ResultCode, &out.CreatedAt, &out.ExpiresAt, &out.ExecutionStartedAt, &out.CompletedAt)
	if err != nil {
		return domain.QuotaResetVote{}, mapError(err)
	}
	rows, err := tx.Query(ctx, `SELECT member_id,user_id,username,avatar_url,weight_basis_points,COALESCE(choice,''),voted_at FROM quota_reset_vote_members WHERE vote_id=$1 ORDER BY CASE choice WHEN 'support' THEN 0 WHEN 'oppose' THEN 1 ELSE 2 END,username`, out.ID)
	if err != nil {
		return domain.QuotaResetVote{}, err
	}
	defer rows.Close()
	out.Members = make([]domain.QuotaResetVoteMember, 0, out.EligibleCount)
	for rows.Next() {
		var member domain.QuotaResetVoteMember
		if err := rows.Scan(&member.MemberID, &member.UserID, &member.Username, &member.AvatarURL, &member.WeightBasisPoints, &member.Choice, &member.VotedAt); err != nil {
			return domain.QuotaResetVote{}, err
		}
		switch member.Choice {
		case "support":
			out.SupportCount++
			out.SupportWeightBasisPoints += member.WeightBasisPoints
		case "oppose":
			out.OpposeCount++
		}
		if member.UserID == currentUserID {
			out.CurrentUserChoice = member.Choice
			out.CanVote = out.Status == "active"
		}
		out.Members = append(out.Members, member)
	}
	return out, rows.Err()
}
