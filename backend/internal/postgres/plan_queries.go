package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Store) CreatePlan(ctx context.Context, plan domain.Plan, owner domain.Member, event domain.AuditEvent) error {
	if plan.AllocationMode == domain.AllocationFixed && (owner.ShareBasisPoints < 0 || owner.ShareBasisPoints > domain.MaxShareBPS) {
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
	if plan.AccountID != "" {
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
	}
	var accountID any
	if plan.AccountID != "" {
		accountID = plan.AccountID
	}
	_, err = tx.Exec(ctx, `INSERT INTO shared_plans(id,owner_user_id,account_id,name,description,status,visibility,public_slots,public_share_basis_points,allocation_mode,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)`, plan.ID, plan.OwnerUserID, accountID, plan.Name, plan.Description, plan.Status, plan.Visibility, plan.PublicSlots, plan.PublicShareBasisPoints, plan.AllocationMode, plan.CreatedAt)
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
	rows, err := s.pool.Query(ctx, `SELECT p.id,p.owner_user_id,p.account_id,p.name,p.description,p.status,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode,p.created_at,p.archived_at FROM shared_plans p JOIN plan_members m ON m.plan_id=p.id WHERE m.user_id=$1 AND m.status='active' ORDER BY p.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Plan, 0)
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PlanDetail(ctx context.Context, planID, userID string, todayStart, now time.Time) (domain.PlanDetail, error) {
	out := domain.PlanDetail{
		Members:      make([]domain.Member, 0),
		Invites:      make([]domain.Invite, 0),
		Applications: make([]domain.JoinApplication, 0),
		Insights: domain.PlanInsights{
			AccountWindows: make([]domain.QuotaWindow, 0),
			MemberQuotas:   make([]domain.MemberQuota, 0),
			WindowUsage:    make([]domain.WindowUsage, 0),
			MemberRanking:  make([]domain.MemberUsageRank, 0),
			MemberRankings: make([]domain.MemberRankingPeriod, 0),
			ModelUsage:     make([]domain.ModelUsage, 0),
			TokenTrend:     make([]domain.DashboardTrendPoint, 0),
			RecentUsage:    make([]domain.MemberUsageTrend, 0),
		},
	}
	plan, err := scanPlan(s.pool.QueryRow(ctx, `SELECT p.id,p.owner_user_id,p.account_id,p.name,p.description,p.status,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode,p.created_at,p.archived_at FROM shared_plans p JOIN plan_members viewer ON viewer.plan_id=p.id AND viewer.user_id=$2 AND viewer.status='active' WHERE p.id=$1`, planID, userID))
	if err != nil {
		return out, mapError(err)
	}
	out.Plan = plan
	if out.Plan.AccountID != "" {
		account, err := s.AccountByID(ctx, out.Plan.AccountID)
		if err != nil {
			return out, err
		}
		out.Account = &account
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
	if out.Account != nil {
		insights, err := s.planInsights(ctx, planID, userID, out.Plan.AccountID, out.Account.CreatedAt, out.Members, todayStart, now)
		if err != nil {
			return out, err
		}
		out.Insights = insights
	}
	return out, nil
}

func (s *Store) planInsights(ctx context.Context, planID, userID, accountID string, accountCreatedAt time.Time, members []domain.Member, todayStart, now time.Time) (domain.PlanInsights, error) {
	out := domain.PlanInsights{
		AccountWindows: make([]domain.QuotaWindow, 0),
		MemberQuotas:   make([]domain.MemberQuota, 0, len(members)),
		WindowUsage:    make([]domain.WindowUsage, 0, 2),
		MemberRanking:  make([]domain.MemberUsageRank, 0, len(members)),
		MemberRankings: make([]domain.MemberRankingPeriod, 0, 4),
		ModelUsage:     make([]domain.ModelUsage, 0),
		TokenTrend:     make([]domain.DashboardTrendPoint, 0, 24),
		RecentUsage:    make([]domain.MemberUsageTrend, 0, 12),
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

	rows, err = s.pool.Query(ctx, `
		SELECT m.id,q.window_type,
			CASE WHEN costs.total_cost_micros=0 THEN 0::bigint
				ELSE floor(costs.member_cost_micros::numeric*q.used_micros/costs.total_cost_micros)::bigint END,
			q.used_micros,q.reset_at
		FROM account_quota_snapshots q
		JOIN plan_members m ON m.plan_id=$1 AND m.status='active'
		CROSS JOIN LATERAL (
			SELECT
				COALESCE(sum(g.estimated_cost_micros) FILTER (WHERE g.member_id=m.id),0)::bigint AS member_cost_micros,
				COALESCE(sum(g.estimated_cost_micros),0)::bigint AS total_cost_micros
			FROM gateway_request_metrics g
			WHERE g.plan_id=$1 AND g.account_id=q.account_id
				AND g.created_at>=q.window_start AND g.created_at<q.reset_at
		) costs
		WHERE q.account_id=$2 AND q.reset_at>now()
		ORDER BY m.id,q.window_type`, planID, accountID)
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
			count(g.id),COALESCE(sum(g.input_tokens),0),COALESCE(sum(g.output_tokens),0),COALESCE(sum(g.cached_tokens),0),
			COALESCE(sum(g.cache_creation_tokens),0),COALESCE(sum(g.image_input_tokens),0),COALESCE(sum(g.image_output_tokens),0),
			COALESCE(sum(g.image_count),0),COALESCE(sum(g.web_search_calls),0),COALESCE(sum(g.estimated_cost_micros),0)
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
		if err := rows.Scan(&usage.WindowType, &usage.WindowStart, &usage.WindowEnd, &usage.RequestCount,
			&usage.TokenUsage.InputTokens, &usage.TokenUsage.OutputTokens, &usage.TokenUsage.CachedTokens,
			&usage.TokenUsage.CacheCreationTokens, &usage.TokenUsage.ImageInputTokens, &usage.TokenUsage.ImageOutputTokens,
			&usage.TokenUsage.ImageCount, &usage.WebSearchCalls, &usage.EstimatedCostMicros); err != nil {
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

	type rankingWindow struct {
		period string
		start  time.Time
		end    time.Time
	}
	rankingWindows := []rankingWindow{
		{period: "today", start: todayStart, end: now},
		{period: "last_7_days", start: now.Add(-7 * 24 * time.Hour), end: now},
	}
	for _, usage := range out.WindowUsage {
		if usage.WindowType == domain.Window7D {
			rankingWindows = append(rankingWindows, rankingWindow{period: "account_7d", start: usage.WindowStart, end: usage.WindowEnd})
			break
		}
	}
	rankingWindows = append(rankingWindows, rankingWindow{period: "account_lifecycle", start: accountCreatedAt, end: now})
	for _, window := range rankingWindows {
		ranking, err := s.memberUsageRanking(ctx, planID, window.start, window.end)
		if err != nil {
			return out, err
		}
		period := domain.MemberRankingPeriod{Period: window.period, WindowStart: window.start, WindowEnd: window.end, Members: ranking}
		out.MemberRankings = append(out.MemberRankings, period)
		if window.period == "last_7_days" {
			out.MemberRanking = ranking
		}
	}

	performance, err := s.PlanPerformance(ctx, planID, userID, now.Add(-24*time.Hour), now, time.Hour)
	if err != nil {
		return out, err
	}
	out.Performance = performance.PerformanceSummary
	out.ModelUsage = performance.ModelUsage
	out.TokenTrend = performance.TokenTrend
	out.RecentUsage = performance.RecentUsage
	return out, nil
}

func (s *Store) planModelUsage(ctx context.Context, planID string, windowStart, windowEnd time.Time) ([]domain.ModelUsage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT requested_model,count(*),sum(input_tokens),sum(output_tokens),sum(cached_tokens),
			sum(cache_creation_tokens),sum(image_input_tokens),sum(image_output_tokens),sum(image_count),sum(web_search_calls),sum(estimated_cost_micros)
		FROM gateway_request_metrics
		WHERE plan_id=$1 AND created_at>=$2 AND created_at<=$3
		GROUP BY requested_model
		ORDER BY sum(input_tokens+output_tokens) DESC,count(*) DESC,requested_model`, planID, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ModelUsage, 0)
	for rows.Next() {
		var usage domain.ModelUsage
		if err := rows.Scan(&usage.Model, &usage.RequestCount, &usage.TokenUsage.InputTokens, &usage.TokenUsage.OutputTokens, &usage.TokenUsage.CachedTokens,
			&usage.TokenUsage.CacheCreationTokens, &usage.TokenUsage.ImageInputTokens, &usage.TokenUsage.ImageOutputTokens,
			&usage.TokenUsage.ImageCount, &usage.WebSearchCalls, &usage.EstimatedCostMicros); err != nil {
			return nil, err
		}
		usage.TokenUsage.TotalTokens = usage.TokenUsage.InputTokens + usage.TokenUsage.OutputTokens
		out = append(out, usage)
	}
	return out, rows.Err()
}

func (s *Store) planTokenTrend(ctx context.Context, planID string, trendStart, now time.Time, bucketSize time.Duration) ([]domain.DashboardTrendPoint, error) {
	rows, err := s.pool.Query(ctx, `
		WITH buckets AS (
			SELECT generate_series($2::timestamptz,$3::timestamptz - INTERVAL '1 microsecond',$4::interval) AS bucket_start
		), usage AS (
			SELECT date_bin($4::interval,created_at,$2::timestamptz) AS bucket_start,
				sum(input_tokens) AS input_tokens,sum(output_tokens) AS output_tokens,sum(cached_tokens) AS cached_tokens,
				sum(cache_creation_tokens) AS cache_creation_tokens,sum(image_input_tokens) AS image_input_tokens,
				sum(image_output_tokens) AS image_output_tokens,sum(image_count) AS image_count,sum(web_search_calls) AS web_search_calls
			FROM gateway_request_metrics
			WHERE plan_id=$1 AND created_at>=$2 AND created_at<=$3
			GROUP BY 1
		)
		SELECT b.bucket_start,COALESCE(u.input_tokens,0),COALESCE(u.output_tokens,0),COALESCE(u.cached_tokens,0),
			COALESCE(u.cache_creation_tokens,0),COALESCE(u.image_input_tokens,0),COALESCE(u.image_output_tokens,0),
			COALESCE(u.image_count,0),COALESCE(u.web_search_calls,0)
		FROM buckets b LEFT JOIN usage u ON u.bucket_start=b.bucket_start
		ORDER BY b.bucket_start`, planID, trendStart, now, bucketSize.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.DashboardTrendPoint, 0, 24)
	for rows.Next() {
		var point domain.DashboardTrendPoint
		if err := rows.Scan(&point.BucketStart, &point.InputTokens, &point.OutputTokens, &point.CachedTokens,
			&point.CacheCreationTokens, &point.ImageInputTokens, &point.ImageOutputTokens, &point.ImageCount, &point.WebSearchCalls); err != nil {
			return nil, err
		}
		out = append(out, point)
	}
	return out, rows.Err()
}

func (s *Store) planRecentUsage(ctx context.Context, planID string, trendStart, now time.Time, bucketSize time.Duration) ([]domain.MemberUsageTrend, error) {
	rows, err := s.pool.Query(ctx, `
		WITH top_members AS (
			SELECT g.member_id,u.username,sum(g.input_tokens+g.output_tokens) AS total_tokens
			FROM gateway_request_metrics g
			JOIN plan_members m ON m.id=g.member_id
			JOIN users u ON u.id=m.user_id
			WHERE g.plan_id=$1 AND g.created_at>=$2 AND g.created_at<=$3
			GROUP BY g.member_id,u.username
			ORDER BY total_tokens DESC,g.member_id
			LIMIT 12
		), buckets AS (
			SELECT generate_series($2::timestamptz,$3::timestamptz - INTERVAL '1 microsecond',$4::interval) AS bucket_start
		), usage AS (
			SELECT g.member_id,date_bin($4::interval,g.created_at,$2::timestamptz) AS bucket_start,
				sum(g.input_tokens) AS input_tokens,sum(g.output_tokens) AS output_tokens,sum(g.cached_tokens) AS cached_tokens,
				sum(g.cache_creation_tokens) AS cache_creation_tokens,sum(g.image_input_tokens) AS image_input_tokens,
				sum(g.image_output_tokens) AS image_output_tokens,sum(g.image_count) AS image_count,sum(g.web_search_calls) AS web_search_calls
			FROM gateway_request_metrics g
			JOIN top_members top ON top.member_id=g.member_id
			WHERE g.plan_id=$1 AND g.created_at>=$2 AND g.created_at<=$3
			GROUP BY g.member_id,2
		)
		SELECT top.member_id,top.username,b.bucket_start,COALESCE(u.input_tokens,0),COALESCE(u.output_tokens,0),COALESCE(u.cached_tokens,0),
			COALESCE(u.cache_creation_tokens,0),COALESCE(u.image_input_tokens,0),COALESCE(u.image_output_tokens,0),
			COALESCE(u.image_count,0),COALESCE(u.web_search_calls,0)
		FROM top_members top CROSS JOIN buckets b
		LEFT JOIN usage u ON u.member_id=top.member_id AND u.bucket_start=b.bucket_start
		ORDER BY top.total_tokens DESC,top.member_id,b.bucket_start`, planID, trendStart, now, bucketSize.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.MemberUsageTrend, 0, 12)
	memberIndexes := make(map[string]int, 12)
	for rows.Next() {
		var memberID, username string
		var point domain.DashboardTrendPoint
		if err := rows.Scan(&memberID, &username, &point.BucketStart, &point.InputTokens, &point.OutputTokens, &point.CachedTokens,
			&point.CacheCreationTokens, &point.ImageInputTokens, &point.ImageOutputTokens, &point.ImageCount, &point.WebSearchCalls); err != nil {
			return nil, err
		}
		index, ok := memberIndexes[memberID]
		if !ok {
			index = len(out)
			memberIndexes[memberID] = index
			out = append(out, domain.MemberUsageTrend{MemberID: memberID, Username: username, Trend: make([]domain.DashboardTrendPoint, 0, 24)})
		}
		out[index].Trend = append(out[index].Trend, point)
	}
	return out, rows.Err()
}

func (s *Store) PlanPerformance(ctx context.Context, planID, userID string, windowStart, windowEnd time.Time, bucketSize time.Duration) (domain.PlanPerformance, error) {
	out := domain.PlanPerformance{
		ModelUsage:  make([]domain.ModelUsage, 0),
		TokenTrend:  make([]domain.DashboardTrendPoint, 0),
		RecentUsage: make([]domain.MemberUsageTrend, 0),
	}
	err := s.pool.QueryRow(ctx, `
		WITH authorized AS (
			SELECT p.id
			FROM shared_plans p
			JOIN plan_members viewer ON viewer.plan_id=p.id AND viewer.user_id=$2 AND viewer.status='active'
			WHERE p.id=$1
		)
		SELECT count(g.id),count(g.id) FILTER (WHERE g.status_code BETWEEN 200 AND 299),
			COALESCE(avg(g.ttft_ms) FILTER (WHERE g.ttft_ms > 0),0)::float8,
			COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY g.ttft_ms) FILTER (WHERE g.ttft_ms > 0),0)::float8,
			COALESCE(avg(g.duration_ms),0)::float8,
			COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY g.duration_ms),0)::float8
		FROM authorized a
		LEFT JOIN gateway_request_metrics g ON g.plan_id=a.id AND g.created_at>=$3 AND g.created_at<=$4
		GROUP BY a.id`, planID, userID, windowStart, windowEnd,
	).Scan(&out.RequestCount, &out.SuccessCount, &out.AverageTTFTMs, &out.P95TTFTMs, &out.AverageDurationMs, &out.P95DurationMs)
	if err != nil {
		return domain.PlanPerformance{}, mapError(err)
	}
	out.ModelUsage, err = s.planModelUsage(ctx, planID, windowStart, windowEnd)
	if err != nil {
		return domain.PlanPerformance{}, err
	}
	out.TokenTrend, err = s.planTokenTrend(ctx, planID, windowStart, windowEnd, bucketSize)
	if err != nil {
		return domain.PlanPerformance{}, err
	}
	out.RecentUsage, err = s.planRecentUsage(ctx, planID, windowStart, windowEnd, bucketSize)
	if err != nil {
		return domain.PlanPerformance{}, err
	}
	return out, nil
}

func (s *Store) PlanRequestErrors(ctx context.Context, planID, userID string, windowStart, windowEnd time.Time, page, pageSize int) (domain.PlanRequestErrorList, error) {
	out := domain.PlanRequestErrorList{
		Items: make([]domain.PlanRequestError, 0),
		Page:  page, PageSize: pageSize,
	}
	err := s.pool.QueryRow(ctx, `
		WITH authorized AS (
			SELECT p.id
			FROM shared_plans p
			JOIN plan_members viewer ON viewer.plan_id=p.id AND viewer.user_id=$2 AND viewer.status='active'
			WHERE p.id=$1
		)
		SELECT count(g.id)
		FROM authorized a
		LEFT JOIN gateway_request_metrics g ON g.plan_id=a.id
			AND g.created_at>=$3 AND g.created_at<=$4
			AND (g.status_code<200 OR g.status_code>=300)
		GROUP BY a.id`, planID, userID, windowStart, windowEnd).Scan(&out.Total)
	if err != nil {
		return domain.PlanRequestErrorList{}, mapError(err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT g.id,g.request_id,g.endpoint,g.is_stream,g.status_code,g.error_source,g.error_code,g.error_message,
			g.requested_model,g.upstream_model,g.service_tier,g.duration_ms,
			m.id,u.username,a.id,a.name,k.name,k.key_prefix,g.created_at
		FROM gateway_request_metrics g
		JOIN plan_members viewer ON viewer.plan_id=g.plan_id AND viewer.user_id=$2 AND viewer.status='active'
		JOIN plan_members m ON m.id=g.member_id
		JOIN users u ON u.id=m.user_id
		JOIN openai_accounts a ON a.id=g.account_id
		JOIN api_keys k ON k.id=g.api_key_id
		WHERE g.plan_id=$1 AND g.created_at>=$3 AND g.created_at<=$4
			AND (g.status_code<200 OR g.status_code>=300)
		ORDER BY g.created_at DESC,g.id DESC
		LIMIT $5 OFFSET $6`, planID, userID, windowStart, windowEnd, pageSize, (page-1)*pageSize)
	if err != nil {
		return domain.PlanRequestErrorList{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.PlanRequestError
		if err := rows.Scan(
			&item.ID, &item.RequestID, &item.Endpoint, &item.IsStream, &item.StatusCode,
			&item.ErrorSource, &item.ErrorCode, &item.ErrorMessage,
			&item.RequestedModel, &item.UpstreamModel, &item.ServiceTier, &item.DurationMs,
			&item.MemberID, &item.MemberUsername, &item.AccountID, &item.AccountName,
			&item.APIKeyName, &item.APIKeyPrefix, &item.CreatedAt,
		); err != nil {
			return domain.PlanRequestErrorList{}, err
		}
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}

func (s *Store) memberUsageRanking(ctx context.Context, planID string, windowStart, windowEnd time.Time) ([]domain.MemberUsageRank, error) {
	rows, err := s.pool.Query(ctx, `
		WITH usage AS (
			SELECT g.member_id,1::bigint AS request_count,g.input_tokens,g.output_tokens,g.cached_tokens,g.cache_creation_tokens,
				g.image_input_tokens,g.image_output_tokens,g.image_count,g.web_search_calls,g.estimated_cost_micros
			FROM gateway_request_metrics g
			WHERE g.plan_id=$1 AND g.created_at>=$2 AND g.created_at<$3
			UNION ALL
			SELECT r.member_id,r.request_count,r.input_tokens,r.output_tokens,r.cached_tokens,r.cache_creation_tokens,
				r.image_input_tokens,r.image_output_tokens,r.image_count,r.web_search_calls,r.estimated_cost_micros
			FROM gateway_metric_daily_rollups r
			WHERE r.plan_id=$1
				AND r.usage_day>=($2 AT TIME ZONE 'UTC')::date
				AND r.usage_day<=($3 AT TIME ZONE 'UTC')::date
		)
		SELECT m.id,u.username,COALESCE(sum(g.request_count),0),COALESCE(sum(g.input_tokens),0),COALESCE(sum(g.output_tokens),0),COALESCE(sum(g.cached_tokens),0),
			COALESCE(sum(g.cache_creation_tokens),0),COALESCE(sum(g.image_input_tokens),0),COALESCE(sum(g.image_output_tokens),0),
			COALESCE(sum(g.image_count),0),COALESCE(sum(g.web_search_calls),0),COALESCE(sum(g.estimated_cost_micros),0)
		FROM usage g
		JOIN plan_members m ON m.id=g.member_id
		JOIN users u ON u.id=m.user_id
		GROUP BY m.id,u.username
		ORDER BY COALESCE(sum(g.input_tokens+g.output_tokens),0) DESC,COALESCE(sum(g.request_count),0) DESC,u.username`, planID, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.MemberUsageRank, 0)
	for rows.Next() {
		var rank domain.MemberUsageRank
		if err := rows.Scan(&rank.MemberID, &rank.Username, &rank.RequestCount,
			&rank.TokenUsage.InputTokens, &rank.TokenUsage.OutputTokens, &rank.TokenUsage.CachedTokens,
			&rank.TokenUsage.CacheCreationTokens, &rank.TokenUsage.ImageInputTokens, &rank.TokenUsage.ImageOutputTokens,
			&rank.TokenUsage.ImageCount, &rank.WebSearchCalls, &rank.EstimatedCostMicros); err != nil {
			return nil, err
		}
		rank.TokenUsage.TotalTokens = rank.TokenUsage.InputTokens + rank.TokenUsage.OutputTokens
		out = append(out, rank)
	}
	return out, rows.Err()
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
