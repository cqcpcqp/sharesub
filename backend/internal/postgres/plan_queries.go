package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Store) CreatePlan(ctx context.Context, plan domain.Plan, owner domain.Member, event domain.AuditEvent) error {
	if plan.AllocationMode == domain.AllocationFixed && (owner.ShareBasisPoints < 1 || owner.ShareBasisPoints > domain.MaxShareBPS) {
		return domain.ErrInvalidInput
	}
	if plan.AllocationMode == domain.AllocationShared && owner.ShareBasisPoints != 0 {
		return domain.ErrInvalidInput
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var accountOwner, accountStatus string
	if err = tx.QueryRow(ctx, `SELECT owner_user_id,status FROM openai_accounts WHERE id=$1 FOR UPDATE`, plan.AccountID).Scan(&accountOwner, &accountStatus); err != nil {
		return mapError(err)
	}
	if accountOwner != plan.OwnerUserID {
		return domain.ErrForbidden
	}
	if accountStatus != domain.StatusActive {
		return domain.ErrAccountUnavailable
	}
	var alreadyBound bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shared_plans WHERE account_id=$1)`, plan.AccountID).Scan(&alreadyBound); err != nil {
		return err
	}
	if alreadyBound {
		return domain.ErrAccountAlreadyBound
	}
	_, err = tx.Exec(ctx, `INSERT INTO shared_plans(id,owner_user_id,account_id,name,status,visibility,public_slots,public_share_basis_points,allocation_mode,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`, plan.ID, plan.OwnerUserID, plan.AccountID, plan.Name, plan.Status, plan.Visibility, plan.PublicSlots, plan.PublicShareBasisPoints, plan.AllocationMode, plan.CreatedAt)
	if err != nil {
		return mapError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO plan_members(id,plan_id,user_id,role,status,share_basis_points,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7)`, owner.ID, owner.PlanID, owner.UserID, owner.Role, owner.Status, owner.ShareBasisPoints, owner.CreatedAt)
	if err != nil {
		return mapError(err)
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListPlans(ctx context.Context, userID string) ([]domain.Plan, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id,p.owner_user_id,p.account_id,p.name,p.status,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode,p.created_at,p.archived_at FROM shared_plans p JOIN plan_members m ON m.plan_id=p.id WHERE m.user_id=$1 AND m.status='active' ORDER BY p.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Plan, 0)
	for rows.Next() {
		var p domain.Plan
		if err := rows.Scan(&p.ID, &p.OwnerUserID, &p.AccountID, &p.Name, &p.Status, &p.Visibility, &p.PublicSlots, &p.PublicShareBasisPoints, &p.AllocationMode, &p.CreatedAt, &p.ArchivedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PlanDetail(ctx context.Context, planID, userID string) (domain.PlanDetail, error) {
	out := domain.PlanDetail{
		Members:      make([]domain.Member, 0),
		Invites:      make([]domain.Invite, 0),
		Applications: make([]domain.JoinApplication, 0),
		Insights: domain.PlanInsights{
			AccountWindows: make([]domain.QuotaWindow, 0),
			MemberQuotas:   make([]domain.MemberQuota, 0),
			WindowUsage:    make([]domain.WindowUsage, 0),
			MemberRanking:  make([]domain.MemberUsageRank, 0),
		},
	}
	err := s.pool.QueryRow(ctx, `SELECT p.id,p.owner_user_id,p.account_id,p.name,p.status,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode,p.created_at,p.archived_at,a.id,a.owner_user_id,a.name,a.notes,a.email,a.chatgpt_account_id,a.plan_type,a.proxy_url_ciphertext,a.max_concurrency,a.rpm_limit,a.token_expires_at,a.status,a.last_error,a.created_at FROM shared_plans p JOIN plan_members viewer ON viewer.plan_id=p.id AND viewer.user_id=$2 AND viewer.status='active' JOIN openai_accounts a ON a.id=p.account_id WHERE p.id=$1`, planID, userID).Scan(&out.Plan.ID, &out.Plan.OwnerUserID, &out.Plan.AccountID, &out.Plan.Name, &out.Plan.Status, &out.Plan.Visibility, &out.Plan.PublicSlots, &out.Plan.PublicShareBasisPoints, &out.Plan.AllocationMode, &out.Plan.CreatedAt, &out.Plan.ArchivedAt, &out.Account.ID, &out.Account.OwnerUserID, &out.Account.Name, &out.Account.Notes, &out.Account.Email, &out.Account.ChatGPTAccountID, &out.Account.PlanType, &out.Account.ProxyURLCiphertext, &out.Account.MaxConcurrency, &out.Account.RPMLimit, &out.Account.TokenExpiresAt, &out.Account.Status, &out.Account.LastError, &out.Account.CreatedAt)
	if err != nil {
		return out, mapError(err)
	}
	isOwner := out.Plan.OwnerUserID == userID
	rows, err := s.pool.Query(ctx, `SELECT m.id,m.plan_id,m.user_id,u.username,u.email,u.avatar_updated_at,m.role,m.status,m.share_basis_points,m.created_at FROM plan_members m JOIN users u ON u.id=m.user_id WHERE m.plan_id=$1 AND m.status='active' ORDER BY CASE WHEN m.role='owner' THEN 0 ELSE 1 END,m.created_at`, planID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var m domain.Member
		var avatarUpdatedAt *time.Time
		if err := rows.Scan(&m.ID, &m.PlanID, &m.UserID, &m.Username, &m.Email, &avatarUpdatedAt, &m.Role, &m.Status, &m.ShareBasisPoints, &m.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		m.AvatarURL = userAvatarURL(m.UserID, avatarUpdatedAt)
		if !isOwner && m.UserID != userID {
			m.Email = ""
		}
		out.Members = append(out.Members, m)
	}
	rows.Close()
	if isOwner {
		invites, err := s.listInvites(ctx, planID)
		if err != nil {
			return out, err
		}
		out.Invites = invites
		applications, err := s.listJoinApplications(ctx, planID)
		if err != nil {
			return out, err
		}
		out.Applications = applications
	}
	insights, err := s.planInsights(ctx, planID, out.Plan.AccountID, out.Members)
	if err != nil {
		return out, err
	}
	out.Insights = insights
	return out, nil
}

func (s *Store) planInsights(ctx context.Context, planID, accountID string, members []domain.Member) (domain.PlanInsights, error) {
	out := domain.PlanInsights{
		AccountWindows: make([]domain.QuotaWindow, 0),
		MemberQuotas:   make([]domain.MemberQuota, 0, len(members)),
		WindowUsage:    make([]domain.WindowUsage, 0, 2),
		MemberRanking:  make([]domain.MemberUsageRank, 0, len(members)),
	}
	memberIndexes := make(map[string]int, len(members))
	for _, member := range members {
		memberIndexes[member.ID] = len(out.MemberQuotas)
		out.MemberQuotas = append(out.MemberQuotas, domain.MemberQuota{MemberID: member.ID, Windows: make([]domain.QuotaWindow, 0)})
	}

	rows, err := s.pool.Query(ctx, `SELECT window_type,used_micros,used_micros,reset_at FROM account_quota_snapshots WHERE account_id=$1 AND reset_at>now() ORDER BY window_type`, accountID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var window domain.QuotaWindow
		if err := rows.Scan(&window.WindowType, &window.UsedMicros, &window.AccountUsedMicros, &window.ResetAt); err != nil {
			rows.Close()
			return out, err
		}
		out.AccountWindows = append(out.AccountWindows, window)
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `SELECT member_id,window_type,used_micros,account_used_micros,reset_at FROM member_quota_windows WHERE member_id IN (SELECT id FROM plan_members WHERE plan_id=$1 AND status='active') AND account_id=$2 AND reset_at>now() ORDER BY member_id,window_type`, planID, accountID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var memberID string
		var window domain.QuotaWindow
		if err := rows.Scan(&memberID, &window.WindowType, &window.UsedMicros, &window.AccountUsedMicros, &window.ResetAt); err != nil {
			rows.Close()
			return out, err
		}
		if index, ok := memberIndexes[memberID]; ok {
			out.MemberQuotas[index].Windows = append(out.MemberQuotas[index].Windows, window)
		}
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT q.window_type,q.window_start,q.reset_at,
			count(g.id),COALESCE(sum(g.input_tokens),0),COALESCE(sum(g.output_tokens),0),COALESCE(sum(g.cached_tokens),0),COALESCE(sum(g.estimated_cost_micros),0)
		FROM account_quota_snapshots q
		LEFT JOIN gateway_request_metrics g ON g.plan_id=$1 AND g.account_id=q.account_id AND g.created_at>=q.window_start AND g.created_at<q.reset_at
		WHERE q.account_id=$2 AND q.reset_at>now()
		GROUP BY q.window_type,q.window_start,q.reset_at
		ORDER BY q.window_type`, planID, accountID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var usage domain.WindowUsage
		if err := rows.Scan(&usage.WindowType, &usage.WindowStart, &usage.WindowEnd, &usage.RequestCount, &usage.TokenUsage.InputTokens, &usage.TokenUsage.OutputTokens, &usage.TokenUsage.CachedTokens, &usage.EstimatedCostMicros); err != nil {
			rows.Close()
			return out, err
		}
		usage.TokenUsage.TotalTokens = usage.TokenUsage.InputTokens + usage.TokenUsage.OutputTokens
		out.WindowUsage = append(out.WindowUsage, usage)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT m.id,u.username,count(g.id),COALESCE(sum(g.input_tokens),0),COALESCE(sum(g.output_tokens),0),COALESCE(sum(g.cached_tokens),0),COALESCE(sum(g.estimated_cost_micros),0)
		FROM gateway_request_metrics g
		JOIN plan_members m ON m.id=g.member_id
		JOIN users u ON u.id=m.user_id
		WHERE g.plan_id=$1 AND g.created_at>=now()-interval '7 days'
		GROUP BY m.id,u.username
		ORDER BY COALESCE(sum(g.input_tokens+g.output_tokens),0) DESC,count(g.id) DESC,u.username`, planID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var rank domain.MemberUsageRank
		if err := rows.Scan(&rank.MemberID, &rank.Username, &rank.RequestCount, &rank.TokenUsage.InputTokens, &rank.TokenUsage.OutputTokens, &rank.TokenUsage.CachedTokens, &rank.EstimatedCostMicros); err != nil {
			rows.Close()
			return out, err
		}
		rank.TokenUsage.TotalTokens = rank.TokenUsage.InputTokens + rank.TokenUsage.OutputTokens
		out.MemberRanking = append(out.MemberRanking, rank)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	err = s.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE status_code BETWEEN 200 AND 299),COALESCE(avg(ttft_ms),0)::float8,COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY ttft_ms),0)::float8,COALESCE(avg(duration_ms),0)::float8,COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms),0)::float8 FROM gateway_request_metrics WHERE plan_id=$1 AND created_at>=now()-interval '24 hours'`, planID).Scan(&out.Performance.RequestCount, &out.Performance.SuccessCount, &out.Performance.AverageTTFTMs, &out.Performance.P95TTFTMs, &out.Performance.AverageDurationMs, &out.Performance.P95DurationMs)
	if err != nil {
		return out, err
	}
	return out, nil
}

func (s *Store) listJoinApplications(ctx context.Context, planID string) ([]domain.JoinApplication, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.id,a.plan_id,a.user_id,u.username,u.email,u.avatar_updated_at,a.message,a.status,a.member_id,a.reviewed_at,a.created_at FROM plan_join_applications a JOIN users u ON u.id=a.user_id WHERE a.plan_id=$1 ORDER BY CASE WHEN a.status='pending' THEN 0 ELSE 1 END,a.created_at DESC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.JoinApplication, 0)
	for rows.Next() {
		var application domain.JoinApplication
		var avatarUpdatedAt *time.Time
		if err := rows.Scan(&application.ID, &application.PlanID, &application.UserID, &application.Username, &application.Email, &avatarUpdatedAt, &application.Message, &application.Status, &application.MemberID, &application.ReviewedAt, &application.CreatedAt); err != nil {
			return nil, err
		}
		application.AvatarURL = userAvatarURL(application.UserID, avatarUpdatedAt)
		out = append(out, application)
	}
	return out, rows.Err()
}

func (s *Store) listInvites(ctx context.Context, planID string) ([]domain.Invite, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,plan_id,share_basis_points,CASE WHEN status='pending' AND expires_at<=now() THEN 'expired' ELSE status END,expires_at,accepted_by_user_id,accepted_at,revoked_at,created_at FROM plan_invites WHERE plan_id=$1 ORDER BY created_at DESC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Invite, 0)
	for rows.Next() {
		var v domain.Invite
		if err := rows.Scan(&v.ID, &v.PlanID, &v.ShareBasisPoints, &v.Status, &v.ExpiresAt, &v.AcceptedByUserID, &v.AcceptedAt, &v.RevokedAt, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
