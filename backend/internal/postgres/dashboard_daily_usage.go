package postgres

import (
	"context"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Store) dashboardDailyUsage(ctx context.Context, userID string, dailyStart, now time.Time, timezone string) ([]domain.DashboardDailyUsage, error) {
	rows, err := s.pool.Query(ctx, `
		WITH days AS (
			SELECT generate_series(
				($2::timestamptz AT TIME ZONE $4)::date,
				($3::timestamptz AT TIME ZONE $4)::date,
				INTERVAL '1 day'
			)::date AS usage_day
		), combined_usage AS (
			SELECT r.usage_day,r.request_count,r.input_tokens,r.output_tokens,r.cached_tokens,
				r.cache_creation_tokens,r.image_input_tokens,r.image_output_tokens,r.image_count
			FROM gateway_metric_daily_rollups r
			WHERE r.user_id=$1
				AND r.usage_day >= ($2::timestamptz AT TIME ZONE $4)::date
				AND r.usage_day <= ($3::timestamptz AT TIME ZONE $4)::date
			UNION ALL
			SELECT (g.created_at AT TIME ZONE $4)::date,COUNT(*),SUM(g.input_tokens),SUM(g.output_tokens),SUM(g.cached_tokens),
				SUM(g.cache_creation_tokens),SUM(g.image_input_tokens),SUM(g.image_output_tokens),SUM(g.image_count)
			FROM gateway_request_metrics g
			JOIN plan_members m ON m.id=g.member_id
			WHERE m.user_id=$1 AND g.created_at >= $2::timestamptz AND g.created_at <= $3::timestamptz
			GROUP BY 1
		), usage AS (
			SELECT usage_day,SUM(request_count) AS request_count,SUM(input_tokens) AS input_tokens,
				SUM(output_tokens) AS output_tokens,SUM(cached_tokens) AS cached_tokens,
				SUM(cache_creation_tokens) AS cache_creation_tokens,SUM(image_input_tokens) AS image_input_tokens,
				SUM(image_output_tokens) AS image_output_tokens,SUM(image_count) AS image_count
			FROM combined_usage
			GROUP BY usage_day
		)
		SELECT to_char(d.usage_day, 'YYYY-MM-DD'),COALESCE(u.request_count,0),COALESCE(u.input_tokens,0),
			COALESCE(u.output_tokens,0),COALESCE(u.cached_tokens,0),COALESCE(u.cache_creation_tokens,0),
			COALESCE(u.image_input_tokens,0),COALESCE(u.image_output_tokens,0),COALESCE(u.image_count,0)
		FROM days d
		LEFT JOIN usage u ON u.usage_day=d.usage_day
		ORDER BY d.usage_day`, userID, dailyStart, now, timezone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.DashboardDailyUsage, 0, 365)
	for rows.Next() {
		var usage domain.DashboardDailyUsage
		if err := rows.Scan(&usage.UsageDate, &usage.RequestCount, &usage.TokenUsage.InputTokens,
			&usage.TokenUsage.OutputTokens, &usage.TokenUsage.CachedTokens, &usage.TokenUsage.CacheCreationTokens,
			&usage.TokenUsage.ImageInputTokens, &usage.TokenUsage.ImageOutputTokens, &usage.TokenUsage.ImageCount); err != nil {
			return nil, err
		}
		usage.TokenUsage.TotalTokens = usage.TokenUsage.InputTokens + usage.TokenUsage.OutputTokens
		out = append(out, usage)
	}
	return out, rows.Err()
}
