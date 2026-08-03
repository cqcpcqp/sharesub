package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Store) ListPublicPlans(ctx context.Context, userID string) ([]domain.PublicPlan, error) {
	rows, err := s.pool.Query(ctx, `
			SELECT p.id,p.owner_user_id,p.account_id,p.name,p.status,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode,p.created_at,p.archived_at,
				u.username,u.avatar_updated_at,a.plan_type,
				(SELECT count(*) FROM plan_members m WHERE m.plan_id=p.id AND m.status='active'),
				GREATEST(p.public_slots-(SELECT count(*) FROM plan_join_applications j JOIN plan_members jm ON jm.id=j.member_id AND jm.status='active' WHERE j.plan_id=p.id AND j.status='approved'),0),
			COALESCE((SELECT j.status FROM plan_join_applications j WHERE j.plan_id=p.id AND j.user_id=$1 ORDER BY j.created_at DESC LIMIT 1),'')
		FROM shared_plans p
		JOIN users u ON u.id=p.owner_user_id
		JOIN openai_accounts a ON a.id=p.account_id
		WHERE p.visibility='public' AND p.status='active'
		ORDER BY CASE WHEN p.owner_user_id=$1 THEN 0 ELSE 1 END,p.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.PublicPlan, 0)
	for rows.Next() {
		var item domain.PublicPlan
		var avatarUpdatedAt *time.Time
		if err := rows.Scan(&item.Plan.ID, &item.Plan.OwnerUserID, &item.Plan.AccountID, &item.Plan.Name, &item.Plan.Status, &item.Plan.Visibility, &item.Plan.PublicSlots, &item.Plan.PublicShareBasisPoints, &item.Plan.AllocationMode, &item.Plan.CreatedAt, &item.Plan.ArchivedAt, &item.OwnerUsername, &avatarUpdatedAt, &item.PlanType, &item.MemberCount, &item.AvailableSlots, &item.ApplicationStatus); err != nil {
			return nil, err
		}
		item.OwnerAvatarURL = userAvatarURL(item.Plan.OwnerUserID, avatarUpdatedAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpdatePlanPublication(ctx context.Context, ownerID, planID, visibility string, slots, share int, event domain.AuditEvent) (domain.Plan, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Plan{}, err
	}
	defer tx.Rollback(ctx)
	var plan domain.Plan
	err = tx.QueryRow(ctx, `SELECT id,owner_user_id,account_id,name,status,visibility,public_slots,public_share_basis_points,allocation_mode,created_at,archived_at FROM shared_plans WHERE id=$1 FOR UPDATE`, planID).Scan(&plan.ID, &plan.OwnerUserID, &plan.AccountID, &plan.Name, &plan.Status, &plan.Visibility, &plan.PublicSlots, &plan.PublicShareBasisPoints, &plan.AllocationMode, &plan.CreatedAt, &plan.ArchivedAt)
	if err != nil {
		return domain.Plan{}, mapError(err)
	}
	if plan.OwnerUserID != ownerID {
		return domain.Plan{}, domain.ErrForbidden
	}
	if plan.Status != domain.StatusActive {
		return domain.Plan{}, domain.ErrConflict
	}
	if visibility == domain.VisibilityPrivate {
		slots = 0
		share = 0
	} else if plan.AllocationMode == domain.AllocationShared {
		if share != 0 {
			return domain.Plan{}, domain.ErrInvalidInput
		}
	} else if share < 1 {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	var approved int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM plan_join_applications j JOIN plan_members m ON m.id=j.member_id AND m.status='active' WHERE j.plan_id=$1 AND j.status='approved'`, planID).Scan(&approved)
	if err != nil {
		return domain.Plan{}, err
	}
	if slots < approved {
		return domain.Plan{}, domain.ErrInvalidInput
	}
	if plan.AllocationMode == domain.AllocationFixed {
		var allocated int
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT sum(share_basis_points) FROM plan_members WHERE plan_id=$1 AND status='active'),0)+COALESCE((SELECT sum(share_basis_points) FROM plan_invites WHERE plan_id=$1 AND status='pending' AND expires_at>now()),0)`, planID).Scan(&allocated); err != nil {
			return domain.Plan{}, err
		}
		if allocated+(slots-approved)*share > domain.MaxShareBPS {
			return domain.Plan{}, domain.ErrShareExceeded
		}
	}
	err = tx.QueryRow(ctx, `UPDATE shared_plans SET visibility=$3,public_slots=$4,public_share_basis_points=$5,updated_at=now() WHERE id=$1 AND owner_user_id=$2 RETURNING id,owner_user_id,account_id,name,status,visibility,public_slots,public_share_basis_points,allocation_mode,created_at,archived_at`, planID, ownerID, visibility, slots, share).Scan(&plan.ID, &plan.OwnerUserID, &plan.AccountID, &plan.Name, &plan.Status, &plan.Visibility, &plan.PublicSlots, &plan.PublicShareBasisPoints, &plan.AllocationMode, &plan.CreatedAt, &plan.ArchivedAt)
	if err != nil {
		return domain.Plan{}, mapError(err)
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.Plan{}, err
	}
	return plan, tx.Commit(ctx)
}

func (s *Store) CreateJoinApplication(ctx context.Context, application domain.JoinApplication, event domain.AuditEvent) (domain.JoinApplication, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.JoinApplication{}, err
	}
	defer tx.Rollback(ctx)
	var ownerID string
	var slots, approved int
	err = tx.QueryRow(ctx, `SELECT p.owner_user_id,p.public_slots,(SELECT count(*) FROM plan_join_applications j JOIN plan_members m ON m.id=j.member_id AND m.status='active' WHERE j.plan_id=p.id AND j.status='approved') FROM shared_plans p WHERE p.id=$1 AND p.status='active' AND p.visibility='public' FOR UPDATE`, application.PlanID).Scan(&ownerID, &slots, &approved)
	if err != nil {
		return domain.JoinApplication{}, mapError(err)
	}
	if ownerID == application.UserID {
		return domain.JoinApplication{}, domain.ErrForbidden
	}
	if approved >= slots {
		return domain.JoinApplication{}, domain.ErrPublicPlanFull
	}
	var alreadyMember bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM plan_members WHERE plan_id=$1 AND user_id=$2 AND status='active')`, application.PlanID, application.UserID).Scan(&alreadyMember); err != nil {
		return domain.JoinApplication{}, err
	}
	if alreadyMember {
		return domain.JoinApplication{}, domain.ErrConflict
	}
	err = tx.QueryRow(ctx, `INSERT INTO plan_join_applications(id,plan_id,user_id,message,status,created_at) VALUES($1,$2,$3,$4,$5,$6) RETURNING id,plan_id,user_id,message,status,member_id,reviewed_at,created_at`, application.ID, application.PlanID, application.UserID, application.Message, application.Status, application.CreatedAt).Scan(&application.ID, &application.PlanID, &application.UserID, &application.Message, &application.Status, &application.MemberID, &application.ReviewedAt, &application.CreatedAt)
	if err != nil {
		return domain.JoinApplication{}, mapError(err)
	}
	var avatarUpdatedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT username,email,avatar_updated_at FROM users WHERE id=$1`, application.UserID).Scan(&application.Username, &application.Email, &avatarUpdatedAt); err != nil {
		return domain.JoinApplication{}, err
	}
	application.AvatarURL = userAvatarURL(application.UserID, avatarUpdatedAt)
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.JoinApplication{}, err
	}
	if err := insertNotification(ctx, tx, event.ID+":owner", ownerID, "join_application", "新的加入申请", application.Username+" 申请加入你的 Plan", "plan", application.PlanID, application.CreatedAt); err != nil {
		return domain.JoinApplication{}, err
	}
	return application, tx.Commit(ctx)
}

func (s *Store) ReviewJoinApplication(ctx context.Context, ownerID, applicationID string, approve bool, memberID string, now time.Time, event domain.AuditEvent) (domain.JoinApplication, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.JoinApplication{}, err
	}
	defer tx.Rollback(ctx)
	var application domain.JoinApplication
	var actualOwner, visibility, allocationMode string
	var slots, share int
	var avatarUpdatedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT j.id,j.plan_id,j.user_id,u.username,u.email,u.avatar_updated_at,j.message,j.status,j.member_id,j.reviewed_at,j.created_at,p.owner_user_id,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode FROM plan_join_applications j JOIN users u ON u.id=j.user_id JOIN shared_plans p ON p.id=j.plan_id WHERE j.id=$1 FOR UPDATE OF j,p`, applicationID).Scan(&application.ID, &application.PlanID, &application.UserID, &application.Username, &application.Email, &avatarUpdatedAt, &application.Message, &application.Status, &application.MemberID, &application.ReviewedAt, &application.CreatedAt, &actualOwner, &visibility, &slots, &share, &allocationMode)
	if err != nil {
		return domain.JoinApplication{}, mapError(err)
	}
	application.AvatarURL = userAvatarURL(application.UserID, avatarUpdatedAt)
	if actualOwner != ownerID {
		return domain.JoinApplication{}, domain.ErrForbidden
	}
	if application.Status != "pending" {
		return domain.JoinApplication{}, domain.ErrConflict
	}
	status := "rejected"
	if approve {
		if visibility != domain.VisibilityPublic {
			return domain.JoinApplication{}, domain.ErrConflict
		}
		var approved int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM plan_join_applications j JOIN plan_members m ON m.id=j.member_id AND m.status='active' WHERE j.plan_id=$1 AND j.status='approved'`, application.PlanID).Scan(&approved); err != nil {
			return domain.JoinApplication{}, err
		}
		if approved >= slots {
			return domain.JoinApplication{}, domain.ErrPublicPlanFull
		}
		if allocationMode == domain.AllocationShared && share != 0 {
			return domain.JoinApplication{}, domain.ErrInvalidInput
		}
		if allocationMode == domain.AllocationFixed && share < 1 {
			return domain.JoinApplication{}, domain.ErrInvalidInput
		}
		var existingStatus string
		err = tx.QueryRow(ctx, `SELECT status FROM plan_members WHERE plan_id=$1 AND user_id=$2 FOR UPDATE`, application.PlanID, application.UserID).Scan(&existingStatus)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return domain.JoinApplication{}, err
		}
		if err == nil && existingStatus == domain.StatusActive {
			return domain.JoinApplication{}, domain.ErrConflict
		}
		var actualMemberID string
		err = tx.QueryRow(ctx, `
			INSERT INTO plan_members(id,plan_id,user_id,role,status,share_basis_points,created_at,updated_at,removed_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$7,NULL)
			ON CONFLICT(plan_id,user_id) DO UPDATE SET role='member',status='active',share_basis_points=EXCLUDED.share_basis_points,removed_at=NULL,updated_at=EXCLUDED.updated_at
			RETURNING id`, memberID, application.PlanID, application.UserID, domain.RoleMember, domain.StatusActive, share, now).Scan(&actualMemberID)
		if err != nil {
			return domain.JoinApplication{}, mapError(err)
		}
		application.MemberID = &actualMemberID
		status = "approved"
	}
	err = tx.QueryRow(ctx, `UPDATE plan_join_applications SET status=$2,member_id=$3,reviewed_by_user_id=$4,reviewed_at=$5 WHERE id=$1 RETURNING status,member_id,reviewed_at`, application.ID, status, application.MemberID, ownerID, now).Scan(&application.Status, &application.MemberID, &application.ReviewedAt)
	if err != nil {
		return domain.JoinApplication{}, err
	}
	event.ResourceType = "plan"
	event.ResourceID = application.PlanID
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.JoinApplication{}, err
	}
	title := "加入申请已拒绝"
	body := "你的 Plan 加入申请未通过"
	notificationType := "application_rejected"
	if approve {
		title = "已加入 Plan"
		body = "你的加入申请已通过，可以开始配置 API Key"
		notificationType = "application_approved"
	}
	if err := insertNotification(ctx, tx, event.ID+":applicant", application.UserID, notificationType, title, body, "plan", application.PlanID, now); err != nil {
		return domain.JoinApplication{}, err
	}
	return application, tx.Commit(ctx)
}

func (s *Store) CreateInvite(ctx context.Context, planID, ownerID string, invite domain.Invite, event domain.AuditEvent) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var actualOwner, allocationMode string
	if err = tx.QueryRow(ctx, `SELECT owner_user_id,allocation_mode FROM shared_plans WHERE id=$1 AND status='active' FOR UPDATE`, planID).Scan(&actualOwner, &allocationMode); err != nil {
		return mapError(err)
	}
	if actualOwner != ownerID {
		return domain.ErrForbidden
	}
	if _, err := tx.Exec(ctx, `UPDATE plan_invites SET status='expired' WHERE plan_id=$1 AND status='pending' AND expires_at<=$2`, planID, invite.CreatedAt); err != nil {
		return err
	}
	if allocationMode == domain.AllocationShared {
		if invite.ShareBasisPoints != 0 {
			return domain.ErrInvalidInput
		}
	} else {
		if invite.ShareBasisPoints < 1 {
			return domain.ErrInvalidInput
		}
		var allocated int
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT sum(share_basis_points) FROM plan_members WHERE plan_id=p.id AND status='active'),0)+COALESCE((SELECT sum(share_basis_points) FROM plan_invites WHERE plan_id=p.id AND status='pending' AND expires_at>now()),0)+GREATEST(p.public_slots-(SELECT count(*) FROM plan_join_applications j JOIN plan_members jm ON jm.id=j.member_id AND jm.status='active' WHERE j.plan_id=p.id AND j.status='approved'),0)*p.public_share_basis_points FROM shared_plans p WHERE p.id=$1`, planID).Scan(&allocated); err != nil {
			return err
		}
		if allocated+invite.ShareBasisPoints > domain.MaxShareBPS {
			return domain.ErrShareExceeded
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO plan_invites(id,plan_id,token_hash,share_basis_points,status,expires_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, invite.ID, invite.PlanID, invite.TokenHash, invite.ShareBasisPoints, invite.Status, invite.ExpiresAt, invite.CreatedAt)
	if err != nil {
		return mapError(err)
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AcceptInvite(ctx context.Context, tokenHash []byte, user domain.User, memberID string, now time.Time, event domain.AuditEvent) (domain.Member, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Member{}, err
	}
	defer tx.Rollback(ctx)
	var invite domain.Invite
	var planOwnerID, planStatus string
	err = tx.QueryRow(ctx, `SELECT i.id,i.plan_id,i.share_basis_points,i.status,i.expires_at,p.owner_user_id,p.status FROM plan_invites i JOIN shared_plans p ON p.id=i.plan_id WHERE i.token_hash=$1 FOR UPDATE OF i,p`, tokenHash).Scan(&invite.ID, &invite.PlanID, &invite.ShareBasisPoints, &invite.Status, &invite.ExpiresAt, &planOwnerID, &planStatus)
	if err != nil {
		return domain.Member{}, mapError(err)
	}
	if invite.Status == "pending" && !invite.ExpiresAt.After(now) {
		if _, err := tx.Exec(ctx, `UPDATE plan_invites SET status='expired' WHERE id=$1`, invite.ID); err != nil {
			return domain.Member{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Member{}, err
		}
		return domain.Member{}, domain.ErrConflict
	}
	if invite.Status != "pending" || planStatus != domain.StatusActive {
		return domain.Member{}, domain.ErrConflict
	}
	member := domain.Member{ID: memberID, PlanID: invite.PlanID, UserID: user.ID, Username: user.Username, AvatarURL: user.AvatarURL, Email: user.Email, Role: domain.RoleMember, Status: domain.StatusActive, ShareBasisPoints: invite.ShareBasisPoints, CreatedAt: now}
	var existingStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM plan_members WHERE plan_id=$1 AND user_id=$2 FOR UPDATE`, invite.PlanID, user.ID).Scan(&existingStatus)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.Member{}, err
	}
	if err == nil && existingStatus == domain.StatusActive {
		return domain.Member{}, domain.ErrConflict
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO plan_members(id,plan_id,user_id,role,status,share_basis_points,created_at,updated_at,removed_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$7,NULL)
		ON CONFLICT(plan_id,user_id) DO UPDATE SET role='member',status='active',share_basis_points=EXCLUDED.share_basis_points,removed_at=NULL,updated_at=EXCLUDED.updated_at
		RETURNING id,created_at`, member.ID, member.PlanID, member.UserID, member.Role, member.Status, member.ShareBasisPoints, member.CreatedAt).Scan(&member.ID, &member.CreatedAt)
	if err != nil {
		return domain.Member{}, mapError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE plan_invites SET status='accepted',accepted_by_user_id=$2,accepted_at=$3 WHERE id=$1`, invite.ID, user.ID, now)
	if err != nil {
		return domain.Member{}, err
	}
	event.ResourceType = "plan"
	event.ResourceID = invite.PlanID
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.Member{}, err
	}
	if err := insertNotification(ctx, tx, event.ID+":owner", planOwnerID, "invite_accepted", "邀请已接受", user.Username+" 已加入你的 Plan", "plan", invite.PlanID, now); err != nil {
		return domain.Member{}, err
	}
	if err := insertNotification(ctx, tx, event.ID+":member", user.ID, "plan_joined", "已加入 Plan", "创建 API Key 后即可开始使用", "plan", invite.PlanID, now); err != nil {
		return domain.Member{}, err
	}
	return member, tx.Commit(ctx)
}

func (s *Store) UpdateMemberShare(ctx context.Context, planID, ownerID, memberID string, share int, event domain.AuditEvent) (domain.Member, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Member{}, err
	}
	defer tx.Rollback(ctx)
	var actualOwner, allocationMode string
	if err = tx.QueryRow(ctx, `SELECT owner_user_id,allocation_mode FROM shared_plans WHERE id=$1 FOR UPDATE`, planID).Scan(&actualOwner, &allocationMode); err != nil {
		return domain.Member{}, mapError(err)
	}
	if actualOwner != ownerID {
		return domain.Member{}, domain.ErrForbidden
	}
	if allocationMode == domain.AllocationShared {
		if share != 0 {
			return domain.Member{}, domain.ErrInvalidInput
		}
	} else {
		if share < 1 {
			return domain.Member{}, domain.ErrInvalidInput
		}
		var others int
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT sum(share_basis_points) FROM plan_members WHERE plan_id=p.id AND status='active' AND id<>$2),0)+COALESCE((SELECT sum(share_basis_points) FROM plan_invites WHERE plan_id=p.id AND status='pending' AND expires_at>now()),0)+GREATEST(p.public_slots-(SELECT count(*) FROM plan_join_applications j JOIN plan_members jm ON jm.id=j.member_id AND jm.status='active' WHERE j.plan_id=p.id AND j.status='approved'),0)*p.public_share_basis_points FROM shared_plans p WHERE p.id=$1`, planID, memberID).Scan(&others); err != nil {
			return domain.Member{}, err
		}
		if others+share > domain.MaxShareBPS {
			return domain.Member{}, domain.ErrShareExceeded
		}
	}
	var m domain.Member
	var avatarUpdatedAt *time.Time
	err = tx.QueryRow(ctx, `UPDATE plan_members m SET share_basis_points=$3,updated_at=now() FROM users u WHERE m.id=$2 AND m.plan_id=$1 AND m.status='active' AND u.id=m.user_id RETURNING m.id,m.plan_id,m.user_id,u.username,u.email,u.avatar_updated_at,m.role,m.status,m.share_basis_points,m.created_at`, planID, memberID, share).Scan(&m.ID, &m.PlanID, &m.UserID, &m.Username, &m.Email, &avatarUpdatedAt, &m.Role, &m.Status, &m.ShareBasisPoints, &m.CreatedAt)
	if err != nil {
		return domain.Member{}, mapError(err)
	}
	m.AvatarURL = userAvatarURL(m.UserID, avatarUpdatedAt)
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.Member{}, err
	}
	if m.UserID != ownerID {
		if err := insertNotification(ctx, tx, event.ID+":member", m.UserID, "member_share_updated", "额度份额已更新", "房主调整了你在 Plan 中的额度份额", "plan", planID, event.CreatedAt); err != nil {
			return domain.Member{}, err
		}
	}
	return m, tx.Commit(ctx)
}
