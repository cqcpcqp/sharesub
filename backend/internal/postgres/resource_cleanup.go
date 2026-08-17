package postgres

import (
	"context"
	"time"
)

const cleanupBatchSize = 10_000

func gatewayMetricCutoff(now time.Time, retention time.Duration) time.Time {
	cutoff := now.Add(-retention).UTC()
	return time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, time.UTC)
}

type RetentionPolicy struct {
	GatewayMetrics    time.Duration
	AuditEvents       time.Duration
	ReadNotifications time.Duration
	TerminalRecords   time.Duration
}

type CleanupResult struct {
	GatewayMetrics     int64
	AuditEvents        int64
	ReadNotifications  int64
	Sessions           int64
	OAuthFlows         int64
	EmailVerifications int64
	Invites            int64
	Applications       int64
	APIKeys            int64
}

func (s *Store) CleanupResources(ctx context.Context, now time.Time, policy RetentionPolicy) (CleanupResult, error) {
	var result CleanupResult
	// Keep the boundary UTC day as raw metrics. Daily rollups cannot represent a
	// partial-day ranking window without including data outside that window.
	metricCutoff := gatewayMetricCutoff(now, policy.GatewayMetrics)
	for {
		tag, err := s.pool.Exec(ctx, `
			WITH candidates AS (
				SELECT g.id,(g.created_at AT TIME ZONE 'UTC')::date AS usage_day,
					m.user_id,g.plan_id,g.account_id,g.member_id,g.account_binding_generation,g.status_code,
					g.input_tokens,g.output_tokens,g.cached_tokens,g.cache_creation_tokens,
					g.image_input_tokens,g.image_output_tokens,g.image_count,g.web_search_calls,g.estimated_cost_micros
				FROM gateway_request_metrics g
				JOIN plan_members m ON m.id=g.member_id
				WHERE g.created_at<$1
				ORDER BY g.id
				LIMIT $2
				FOR UPDATE OF g SKIP LOCKED
			), rollup AS (
				INSERT INTO gateway_metric_daily_rollups(
					usage_day,user_id,plan_id,account_id,member_id,account_binding_generation,request_count,success_count,
					input_tokens,output_tokens,cached_tokens,cache_creation_tokens,image_input_tokens,image_output_tokens,
					image_count,web_search_calls,estimated_cost_micros
				)
				SELECT usage_day,user_id,plan_id,account_id,member_id,account_binding_generation,count(*),
					count(*) FILTER (WHERE status_code BETWEEN 200 AND 299),
					sum(input_tokens),sum(output_tokens),sum(cached_tokens),sum(cache_creation_tokens),
					sum(image_input_tokens),sum(image_output_tokens),sum(image_count),sum(web_search_calls),sum(estimated_cost_micros)
				FROM candidates
				GROUP BY usage_day,user_id,plan_id,account_id,member_id,account_binding_generation
				ON CONFLICT(usage_day,user_id,plan_id,account_id,member_id,account_binding_generation) DO UPDATE SET
					request_count=gateway_metric_daily_rollups.request_count+EXCLUDED.request_count,
					success_count=gateway_metric_daily_rollups.success_count+EXCLUDED.success_count,
					input_tokens=gateway_metric_daily_rollups.input_tokens+EXCLUDED.input_tokens,
					output_tokens=gateway_metric_daily_rollups.output_tokens+EXCLUDED.output_tokens,
					cached_tokens=gateway_metric_daily_rollups.cached_tokens+EXCLUDED.cached_tokens,
					cache_creation_tokens=gateway_metric_daily_rollups.cache_creation_tokens+EXCLUDED.cache_creation_tokens,
					image_input_tokens=gateway_metric_daily_rollups.image_input_tokens+EXCLUDED.image_input_tokens,
					image_output_tokens=gateway_metric_daily_rollups.image_output_tokens+EXCLUDED.image_output_tokens,
					image_count=gateway_metric_daily_rollups.image_count+EXCLUDED.image_count,
					web_search_calls=gateway_metric_daily_rollups.web_search_calls+EXCLUDED.web_search_calls,
					estimated_cost_micros=gateway_metric_daily_rollups.estimated_cost_micros+EXCLUDED.estimated_cost_micros
			)
			DELETE FROM gateway_request_metrics g USING candidates c WHERE g.id=c.id`, metricCutoff, cleanupBatchSize)
		if err != nil {
			return result, err
		}
		deleted := tag.RowsAffected()
		result.GatewayMetrics += deleted
		if deleted < cleanupBatchSize {
			break
		}
	}

	deletions := []struct {
		target *int64
		query  string
		args   []any
	}{
		{&result.AuditEvents, `WITH candidates AS (SELECT ctid FROM audit_events WHERE created_at<$1 LIMIT $2 FOR UPDATE SKIP LOCKED) DELETE FROM audit_events t USING candidates c WHERE t.ctid=c.ctid`, []any{now.Add(-policy.AuditEvents)}},
		{&result.ReadNotifications, `WITH candidates AS (SELECT ctid FROM notifications WHERE read_at IS NOT NULL AND created_at<$1 LIMIT $2 FOR UPDATE SKIP LOCKED) DELETE FROM notifications t USING candidates c WHERE t.ctid=c.ctid`, []any{now.Add(-policy.ReadNotifications)}},
		{&result.Sessions, `WITH candidates AS (SELECT ctid FROM user_sessions WHERE expires_at<$1 LIMIT $2 FOR UPDATE SKIP LOCKED) DELETE FROM user_sessions t USING candidates c WHERE t.ctid=c.ctid`, []any{now}},
		{&result.OAuthFlows, `WITH candidates AS (SELECT ctid FROM oauth_flows WHERE expires_at<$1 LIMIT $2 FOR UPDATE SKIP LOCKED) DELETE FROM oauth_flows t USING candidates c WHERE t.ctid=c.ctid`, []any{now}},
		{&result.EmailVerifications, `WITH candidates AS (SELECT ctid FROM email_verification_tokens WHERE expires_at<$1 AND created_at<$1-interval '1 hour' LIMIT $2 FOR UPDATE SKIP LOCKED) DELETE FROM email_verification_tokens t USING candidates c WHERE t.ctid=c.ctid`, []any{now}},
		{&result.Invites, `WITH candidates AS (SELECT ctid FROM plan_invites WHERE COALESCE(accepted_at,revoked_at,expires_at)<$1 LIMIT $2 FOR UPDATE SKIP LOCKED) DELETE FROM plan_invites t USING candidates c WHERE t.ctid=c.ctid`, []any{now.Add(-policy.TerminalRecords)}},
		{&result.Applications, `WITH candidates AS (SELECT ctid FROM plan_join_applications WHERE status<>'pending' AND COALESCE(reviewed_at,created_at)<$1 LIMIT $2 FOR UPDATE SKIP LOCKED) DELETE FROM plan_join_applications t USING candidates c WHERE t.ctid=c.ctid`, []any{now.Add(-policy.TerminalRecords)}},
		{&result.APIKeys, `WITH candidates AS (SELECT k.ctid FROM api_keys k WHERE k.status='revoked' AND k.updated_at<$1 AND NOT EXISTS(SELECT 1 FROM gateway_request_metrics g WHERE g.api_key_id=k.id) LIMIT $2 FOR UPDATE OF k SKIP LOCKED) DELETE FROM api_keys k USING candidates c WHERE k.ctid=c.ctid`, []any{now.Add(-policy.TerminalRecords)}},
	}
	for _, deletion := range deletions {
		for {
			args := append(deletion.args, cleanupBatchSize)
			tag, err := s.pool.Exec(ctx, deletion.query, args...)
			if err != nil {
				return result, err
			}
			deleted := tag.RowsAffected()
			*deletion.target += deleted
			if deleted < cleanupBatchSize {
				break
			}
		}
	}
	return result, nil
}
