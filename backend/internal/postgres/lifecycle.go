package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func insertAuditEvent(ctx context.Context, tx pgx.Tx, event domain.AuditEvent) error {
	metadata := event.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(id,actor_user_id,action,resource_type,resource_id,metadata,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, metadata, event.CreatedAt)
	return err
}

func insertNotification(ctx context.Context, tx pgx.Tx, id, userID, kind, title, body, resourceType, resourceID string, createdAt time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO notifications(id,user_id,type,title,body,resource_type,resource_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, userID, kind, title, body, resourceType, resourceID, createdAt)
	return err
}

func notifyPlanMembers(ctx context.Context, tx pgx.Tx, eventID, planID, excludeUserID, kind, title, body string, createdAt time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO notifications(id,user_id,type,title,body,resource_type,resource_id,created_at)
		SELECT $1 || ':' || m.user_id,m.user_id,$4,$5,$6,'plan',$2,$7
		FROM plan_members m
		WHERE m.plan_id=$2 AND m.status='active' AND m.user_id<>$3`, eventID, planID, excludeUserID, kind, title, body, createdAt)
	return err
}

func (s *Store) UpdateAccountAuthorization(ctx context.Context, ownerID string, account domain.Account, event domain.AuditEvent) (domain.Account, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Account{}, err
	}
	defer tx.Rollback(ctx)
	var out domain.Account
	err = tx.QueryRow(ctx, `
		UPDATE openai_accounts SET email=$3,plan_type=$4,access_token_ciphertext=$5,refresh_token_ciphertext=$6,token_expires_at=$7,status='active',last_error='',updated_at=$8
		WHERE id=$1 AND owner_user_id=$2
		RETURNING id,owner_user_id,name,notes,email,chatgpt_account_id,plan_type,proxy_url_ciphertext,max_concurrency,rpm_limit,token_expires_at,status,last_error,created_at`,
		account.ID, ownerID, account.Email, account.PlanType, account.AccessTokenCiphertext, account.RefreshTokenCiphertext, account.TokenExpiresAt, event.CreatedAt,
	).Scan(&out.ID, &out.OwnerUserID, &out.Name, &out.Notes, &out.Email, &out.ChatGPTAccountID, &out.PlanType, &out.ProxyURLCiphertext, &out.MaxConcurrency, &out.RPMLimit, &out.TokenExpiresAt, &out.Status, &out.LastError, &out.CreatedAt)
	if err != nil {
		return domain.Account{}, mapError(err)
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.Account{}, err
	}
	if err := insertNotification(ctx, tx, event.ID+":owner", ownerID, "account_reauthorized", "OpenAI 账号已重新授权", out.Name+" 的凭据已更新", "account", account.ID, event.CreatedAt); err != nil {
		return domain.Account{}, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) InvitePreview(ctx context.Context, tokenHash []byte, now time.Time) (domain.InvitePreview, error) {
	var out domain.InvitePreview
	err := s.pool.QueryRow(ctx, `
		SELECT p.id,p.name,u.username,p.allocation_mode,i.share_basis_points,i.expires_at
		FROM plan_invites i
		JOIN shared_plans p ON p.id=i.plan_id AND p.status='active'
		JOIN users u ON u.id=p.owner_user_id
		WHERE i.token_hash=$1 AND i.status='pending' AND i.expires_at>$2`, tokenHash, now,
	).Scan(&out.PlanID, &out.PlanName, &out.OwnerUsername, &out.AllocationMode, &out.ShareBasisPoints, &out.ExpiresAt)
	if err != nil {
		return domain.InvitePreview{}, mapError(err)
	}
	return out, nil
}

func (s *Store) RevokeInvite(ctx context.Context, planID, ownerID, inviteID string, event domain.AuditEvent) (domain.Invite, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Invite{}, err
	}
	defer tx.Rollback(ctx)
	var actualOwner string
	if err := tx.QueryRow(ctx, `SELECT owner_user_id FROM shared_plans WHERE id=$1 FOR UPDATE`, planID).Scan(&actualOwner); err != nil {
		return domain.Invite{}, mapError(err)
	}
	if actualOwner != ownerID {
		return domain.Invite{}, domain.ErrForbidden
	}
	var invite domain.Invite
	err = tx.QueryRow(ctx, `
		UPDATE plan_invites SET status='revoked',revoked_at=$4,revoked_by_user_id=$3
		WHERE id=$2 AND plan_id=$1 AND status='pending' AND expires_at>$4
		RETURNING id,plan_id,share_basis_points,status,expires_at,accepted_by_user_id,accepted_at,revoked_at,created_at`,
		planID, inviteID, ownerID, event.CreatedAt,
	).Scan(&invite.ID, &invite.PlanID, &invite.ShareBasisPoints, &invite.Status, &invite.ExpiresAt, &invite.AcceptedByUserID, &invite.AcceptedAt, &invite.RevokedAt, &invite.CreatedAt)
	if err != nil {
		return domain.Invite{}, mapError(err)
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.Invite{}, err
	}
	return invite, tx.Commit(ctx)
}

func (s *Store) RenamePlan(ctx context.Context, planID, ownerID, name string, event domain.AuditEvent) (domain.Plan, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Plan{}, err
	}
	defer tx.Rollback(ctx)
	plan, err := scanPlan(tx.QueryRow(ctx, `UPDATE shared_plans SET name=$3,updated_at=$4 WHERE id=$1 AND owner_user_id=$2 RETURNING id,owner_user_id,account_id,name,status,visibility,public_slots,public_share_basis_points,allocation_mode,created_at,archived_at`, planID, ownerID, name, event.CreatedAt))
	if err != nil {
		return domain.Plan{}, err
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.Plan{}, err
	}
	if err := notifyPlanMembers(ctx, tx, event.ID, planID, ownerID, "plan_renamed", "Plan 名称已更新", "房主将 Plan 重命名为 "+name, event.CreatedAt); err != nil {
		return domain.Plan{}, err
	}
	return plan, tx.Commit(ctx)
}

func (s *Store) UpdatePlanStatus(ctx context.Context, planID, ownerID, status string, event domain.AuditEvent) (domain.Plan, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Plan{}, err
	}
	defer tx.Rollback(ctx)
	var currentOwner, currentStatus, accountID string
	if err := tx.QueryRow(ctx, `SELECT owner_user_id,status,account_id FROM shared_plans WHERE id=$1 FOR UPDATE`, planID).Scan(&currentOwner, &currentStatus, &accountID); err != nil {
		return domain.Plan{}, mapError(err)
	}
	if currentOwner != ownerID {
		return domain.Plan{}, domain.ErrForbidden
	}
	if currentStatus == status {
		return domain.Plan{}, domain.ErrConflict
	}
	if status == domain.StatusArchived && currentStatus != domain.StatusActive {
		return domain.Plan{}, domain.ErrConflict
	}
	if status == domain.StatusActive && currentStatus != domain.StatusArchived {
		return domain.Plan{}, domain.ErrConflict
	}
	if status == domain.StatusActive {
		var lockedAccountID string
		if err := tx.QueryRow(ctx, `SELECT id FROM openai_accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&lockedAccountID); err != nil {
			return domain.Plan{}, mapError(err)
		}
		var alreadyBound bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shared_plans WHERE account_id=$1 AND id<>$2)`, accountID, planID).Scan(&alreadyBound); err != nil {
			return domain.Plan{}, err
		}
		if alreadyBound {
			return domain.Plan{}, domain.ErrAccountAlreadyBound
		}
	}
	if status == domain.StatusArchived {
		if _, err := tx.Exec(ctx, `UPDATE plan_invites SET status='revoked',revoked_at=$2,revoked_by_user_id=$3 WHERE plan_id=$1 AND status='pending'`, planID, event.CreatedAt, ownerID); err != nil {
			return domain.Plan{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE plan_join_applications SET status='rejected',reviewed_by_user_id=$2,reviewed_at=$3 WHERE plan_id=$1 AND status='pending'`, planID, ownerID, event.CreatedAt); err != nil {
			return domain.Plan{}, err
		}
	}
	plan, err := scanPlan(tx.QueryRow(ctx, `
		UPDATE shared_plans SET status=$3,visibility=CASE WHEN $3='archived' THEN 'private' ELSE visibility END,public_slots=CASE WHEN $3='archived' THEN 0 ELSE public_slots END,public_share_basis_points=CASE WHEN $3='archived' THEN 0 ELSE public_share_basis_points END,archived_at=CASE WHEN $3='archived' THEN $4::timestamptz ELSE NULL END,updated_at=$4::timestamptz
		WHERE id=$1 AND owner_user_id=$2
		RETURNING id,owner_user_id,account_id,name,status,visibility,public_slots,public_share_basis_points,allocation_mode,created_at,archived_at`, planID, ownerID, status, event.CreatedAt))
	if err != nil {
		return domain.Plan{}, err
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.Plan{}, err
	}
	title, body, kind := "Plan 已恢复", "共享 Plan 已恢复使用", "plan_restored"
	if status == domain.StatusArchived {
		title, body, kind = "Plan 已归档", "共享 Plan 已暂停使用", "plan_archived"
	}
	if err := notifyPlanMembers(ctx, tx, event.ID, planID, ownerID, kind, title, body, event.CreatedAt); err != nil {
		return domain.Plan{}, err
	}
	return plan, tx.Commit(ctx)
}

func (s *Store) DeletePlan(ctx context.Context, planID, ownerID string, event domain.AuditEvent) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var actualOwner, status string
	if err := tx.QueryRow(ctx, `SELECT owner_user_id,status FROM shared_plans WHERE id=$1 FOR UPDATE`, planID).Scan(&actualOwner, &status); err != nil {
		return mapError(err)
	}
	if actualOwner != ownerID {
		return domain.ErrForbidden
	}
	if status != domain.StatusArchived {
		return domain.ErrConflict
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return err
	}
	if err := notifyPlanMembers(ctx, tx, event.ID, planID, ownerID, "plan_deleted", "Plan 已删除", "房主删除了共享 Plan", event.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM shared_plans WHERE id=$1`, planID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) TransferPlanOwnership(ctx context.Context, planID, ownerID, memberID string, event domain.AuditEvent) (domain.Plan, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Plan{}, err
	}
	defer tx.Rollback(ctx)
	var actualOwner string
	if err := tx.QueryRow(ctx, `SELECT owner_user_id FROM shared_plans WHERE id=$1 FOR UPDATE`, planID).Scan(&actualOwner); err != nil {
		return domain.Plan{}, mapError(err)
	}
	if actualOwner != ownerID {
		return domain.Plan{}, domain.ErrForbidden
	}
	var targetUserID, targetRole, targetStatus string
	if err := tx.QueryRow(ctx, `SELECT user_id,role,status FROM plan_members WHERE id=$1 AND plan_id=$2 FOR UPDATE`, memberID, planID).Scan(&targetUserID, &targetRole, &targetStatus); err != nil {
		return domain.Plan{}, mapError(err)
	}
	if targetStatus != domain.StatusActive || targetRole != domain.RoleMember || targetUserID == ownerID {
		return domain.Plan{}, domain.ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE plan_members SET role='member',updated_at=$3 WHERE plan_id=$1 AND user_id=$2 AND role='owner' AND status='active'`, planID, ownerID, event.CreatedAt); err != nil {
		return domain.Plan{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE plan_members SET role='owner',updated_at=$3 WHERE id=$1 AND plan_id=$2 AND status='active'`, memberID, planID, event.CreatedAt); err != nil {
		return domain.Plan{}, err
	}
	plan, err := scanPlan(tx.QueryRow(ctx, `UPDATE shared_plans SET owner_user_id=$3,updated_at=$4 WHERE id=$1 AND owner_user_id=$2 RETURNING id,owner_user_id,account_id,name,status,visibility,public_slots,public_share_basis_points,allocation_mode,created_at,archived_at`, planID, ownerID, targetUserID, event.CreatedAt))
	if err != nil {
		return domain.Plan{}, err
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.Plan{}, err
	}
	if err := insertNotification(ctx, tx, event.ID+":new-owner", targetUserID, "plan_ownership_received", "你已成为 Plan 房主", "你现在可以管理成员和 Plan 设置", "plan", planID, event.CreatedAt); err != nil {
		return domain.Plan{}, err
	}
	if err := insertNotification(ctx, tx, event.ID+":old-owner", ownerID, "plan_ownership_transferred", "Plan 已转让", "你已成为该 Plan 的普通成员", "plan", planID, event.CreatedAt); err != nil {
		return domain.Plan{}, err
	}
	return plan, tx.Commit(ctx)
}

func (s *Store) RebindPlanAccount(ctx context.Context, planID, ownerID, accountID string, event domain.AuditEvent) (domain.Plan, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Plan{}, err
	}
	defer tx.Rollback(ctx)
	var actualOwner, currentAccountID string
	if err := tx.QueryRow(ctx, `SELECT owner_user_id,account_id FROM shared_plans WHERE id=$1 FOR UPDATE`, planID).Scan(&actualOwner, &currentAccountID); err != nil {
		return domain.Plan{}, mapError(err)
	}
	if actualOwner != ownerID {
		return domain.Plan{}, domain.ErrForbidden
	}
	if currentAccountID == accountID {
		return domain.Plan{}, domain.ErrConflict
	}
	var accountOwner, accountStatus string
	if err := tx.QueryRow(ctx, `SELECT owner_user_id,status FROM openai_accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&accountOwner, &accountStatus); err != nil {
		return domain.Plan{}, mapError(err)
	}
	if accountOwner != ownerID {
		return domain.Plan{}, domain.ErrForbidden
	}
	if accountStatus != domain.StatusActive {
		return domain.Plan{}, domain.ErrAccountUnavailable
	}
	var alreadyBound bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shared_plans WHERE account_id=$1 AND id<>$2)`, accountID, planID).Scan(&alreadyBound); err != nil {
		return domain.Plan{}, err
	}
	if alreadyBound {
		return domain.Plan{}, domain.ErrAccountAlreadyBound
	}
	plan, err := scanPlan(tx.QueryRow(ctx, `UPDATE shared_plans SET account_id=$3,updated_at=$4 WHERE id=$1 AND owner_user_id=$2 RETURNING id,owner_user_id,account_id,name,status,visibility,public_slots,public_share_basis_points,allocation_mode,created_at,archived_at`, planID, ownerID, accountID, event.CreatedAt))
	if err != nil {
		return domain.Plan{}, err
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return domain.Plan{}, err
	}
	if err := notifyPlanMembers(ctx, tx, event.ID, planID, ownerID, "plan_account_rebound", "Plan 账号已更换", "房主更换了 Plan 绑定的 OpenAI 账号", event.CreatedAt); err != nil {
		return domain.Plan{}, err
	}
	return plan, tx.Commit(ctx)
}

func (s *Store) RemovePlanMember(ctx context.Context, planID, actorUserID, memberID string, event domain.AuditEvent) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var ownerID string
	if err := tx.QueryRow(ctx, `SELECT owner_user_id FROM shared_plans WHERE id=$1 FOR UPDATE`, planID).Scan(&ownerID); err != nil {
		return mapError(err)
	}
	var targetUserID, role, status string
	if err := tx.QueryRow(ctx, `SELECT user_id,role,status FROM plan_members WHERE id=$1 AND plan_id=$2 FOR UPDATE`, memberID, planID).Scan(&targetUserID, &role, &status); err != nil {
		return mapError(err)
	}
	if actorUserID != ownerID && actorUserID != targetUserID {
		return domain.ErrForbidden
	}
	if role == domain.RoleOwner {
		return domain.ErrConflict
	}
	if status != domain.StatusActive {
		return domain.ErrConflict
	}
	if actorUserID == targetUserID {
		event.Action = "member.left"
	}
	if _, err := tx.Exec(ctx, `UPDATE plan_members SET status='removed',removed_at=$3,updated_at=$3 WHERE id=$1 AND plan_id=$2`, memberID, planID, event.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE api_key_plans r SET enabled=false FROM api_keys k WHERE r.api_key_id=k.id AND r.plan_id=$1 AND k.user_id=$2`, planID, targetUserID); err != nil {
		return err
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return err
	}
	if actorUserID == targetUserID {
		if err := insertNotification(ctx, tx, event.ID+":owner", ownerID, "member_left", "成员已退出 Plan", "一位成员主动退出了共享 Plan", "plan", planID, event.CreatedAt); err != nil {
			return err
		}
	} else if err := insertNotification(ctx, tx, event.ID+":member", targetUserID, "member_removed", "你已被移出 Plan", "房主结束了你的 Plan 成员资格", "plan", planID, event.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListPlanAuditEvents(ctx context.Context, planID, userID string) ([]domain.AuditEvent, error) {
	var allowed bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM plan_members WHERE plan_id=$1 AND user_id=$2 AND status='active')`, planID, userID).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, domain.ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT e.id,e.actor_user_id,COALESCE(u.username,''),e.action,e.resource_type,e.resource_id,e.metadata,e.created_at
		FROM audit_events e
		LEFT JOIN users u ON u.id=e.actor_user_id
		WHERE e.resource_type='plan' AND e.resource_id=$1
		ORDER BY e.created_at DESC,e.id DESC LIMIT 100`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.AuditEvent, 0)
	for rows.Next() {
		var event domain.AuditEvent
		if err := rows.Scan(&event.ID, &event.ActorUserID, &event.ActorUsername, &event.Action, &event.ResourceType, &event.ResourceID, &event.Metadata, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Store) PlanQuotaCredential(ctx context.Context, planID, ownerID string) (domain.PlanQuotaCredential, error) {
	var out domain.PlanQuotaCredential
	err := s.pool.QueryRow(ctx, `
		SELECT p.id,a.id,m.id,a.owner_user_id,a.chatgpt_account_id,a.access_token_ciphertext,a.refresh_token_ciphertext,a.proxy_url_ciphertext,a.token_expires_at
		FROM shared_plans p
		JOIN plan_members m ON m.plan_id=p.id AND m.user_id=$2 AND m.role='owner' AND m.status='active'
		JOIN openai_accounts a ON a.id=p.account_id AND a.status='active'
		WHERE p.id=$1 AND p.owner_user_id=$2 AND p.status='active'`, planID, ownerID,
	).Scan(&out.PlanID, &out.AccountID, &out.OwnerMemberID, &out.AccountOwnerUserID, &out.ChatGPTAccountID, &out.AccessTokenCiphertext, &out.RefreshTokenCiphertext, &out.ProxyURLCiphertext, &out.TokenExpiresAt)
	if err != nil {
		return domain.PlanQuotaCredential{}, mapError(err)
	}
	return out, nil
}

func (s *Store) ListNotifications(ctx context.Context, userID string) (domain.NotificationList, error) {
	out := domain.NotificationList{Items: make([]domain.Notification, 0)}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id=$1 AND read_at IS NULL`, userID).Scan(&out.UnreadCount); err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,user_id,type,title,body,resource_type,resource_id,read_at,created_at FROM notifications WHERE user_id=$1 ORDER BY created_at DESC,id DESC LIMIT 100`, userID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.Notification
		if err := rows.Scan(&item.ID, &item.UserID, &item.Type, &item.Title, &item.Body, &item.ResourceType, &item.ResourceID, &item.ReadAt, &item.CreatedAt); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}

func (s *Store) UpdateNotification(ctx context.Context, userID, notificationID string, read bool, now time.Time) (domain.Notification, error) {
	var out domain.Notification
	var readAt any
	if read {
		readAt = now
	}
	err := s.pool.QueryRow(ctx, `UPDATE notifications SET read_at=$3 WHERE id=$1 AND user_id=$2 RETURNING id,user_id,type,title,body,resource_type,resource_id,read_at,created_at`, notificationID, userID, readAt).Scan(&out.ID, &out.UserID, &out.Type, &out.Title, &out.Body, &out.ResourceType, &out.ResourceID, &out.ReadAt, &out.CreatedAt)
	if err != nil {
		return domain.Notification{}, mapError(err)
	}
	return out, nil
}

func (s *Store) ReadAllNotifications(ctx context.Context, userID string, now time.Time) (int64, error) {
	result, err := s.pool.Exec(ctx, `UPDATE notifications SET read_at=$2 WHERE user_id=$1 AND read_at IS NULL`, userID, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func scanPlan(row pgx.Row) (domain.Plan, error) {
	var plan domain.Plan
	err := row.Scan(&plan.ID, &plan.OwnerUserID, &plan.AccountID, &plan.Name, &plan.Status, &plan.Visibility, &plan.PublicSlots, &plan.PublicShareBasisPoints, &plan.AllocationMode, &plan.CreatedAt, &plan.ArchivedAt)
	return plan, mapError(err)
}
