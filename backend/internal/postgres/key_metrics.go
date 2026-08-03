package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Store) CreateAPIKey(ctx context.Context, key domain.APIKey, routes []domain.APIKeyRoute) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO api_keys(id,user_id,name,key_prefix,key_hash,key_ciphertext,strategy,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)`, key.ID, key.UserID, key.Name, key.KeyPrefix, key.KeyHash, key.KeyCiphertext, key.Strategy, key.Status, key.CreatedAt)
	if err != nil {
		return mapError(err)
	}
	if err := insertAPIKeyRoutes(ctx, tx, key.ID, key.UserID, routes); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) UpdateAPIKey(ctx context.Context, userID string, key domain.APIKey, routes []domain.APIKeyRoute) (domain.APIKey, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `UPDATE api_keys SET name=$3,strategy=$4,updated_at=now() WHERE id=$1 AND user_id=$2 RETURNING id,user_id,name,key_prefix,key_ciphertext,strategy,status,last_used_at,created_at`, key.ID, userID, key.Name, key.Strategy).Scan(&key.ID, &key.UserID, &key.Name, &key.KeyPrefix, &key.KeyCiphertext, &key.Strategy, &key.Status, &key.LastUsedAt, &key.CreatedAt)
	if err != nil {
		return domain.APIKey{}, mapError(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM api_key_plans WHERE api_key_id=$1`, key.ID); err != nil {
		return domain.APIKey{}, err
	}
	if err := insertAPIKeyRoutes(ctx, tx, key.ID, userID, routes); err != nil {
		return domain.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.APIKey{}, err
	}
	key.Routes = routes
	return key, nil
}

func insertAPIKeyRoutes(ctx context.Context, tx pgx.Tx, keyID, userID string, routes []domain.APIKeyRoute) error {
	for _, route := range routes {
		result, err := tx.Exec(ctx, `INSERT INTO api_key_plans(api_key_id,plan_id,priority,enabled) SELECT $1,p.id,$3,$4 FROM shared_plans p JOIN plan_members m ON m.plan_id=p.id WHERE p.id=$2 AND p.status='active' AND m.user_id=$5 AND m.status='active'`, keyID, route.PlanID, route.Priority, route.Enabled, userID)
		if err != nil {
			return mapError(err)
		}
		if result.RowsAffected() != 1 {
			return domain.ErrForbidden
		}
	}
	return nil
}

func (s *Store) ListAPIKeys(ctx context.Context, userID string) ([]domain.APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,user_id,name,key_prefix,key_ciphertext,strategy,status,last_used_at,created_at FROM api_keys WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.APIKey, 0)
	for rows.Next() {
		var k domain.APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.KeyCiphertext, &k.Strategy, &k.Status, &k.LastUsedAt, &k.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		k.Routes = make([]domain.APIKeyRoute, 0)
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for index := range out {
		routes, err := s.listAPIKeyRoutes(ctx, out[index].ID)
		if err != nil {
			return nil, err
		}
		out[index].Routes = routes
	}
	return out, nil
}

func (s *Store) listAPIKeyRoutes(ctx context.Context, keyID string) ([]domain.APIKeyRoute, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.plan_id,p.name,r.priority,r.enabled FROM api_key_plans r JOIN shared_plans p ON p.id=r.plan_id WHERE r.api_key_id=$1 ORDER BY r.priority,p.name`, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.APIKeyRoute, 0)
	for rows.Next() {
		var route domain.APIKeyRoute
		if err := rows.Scan(&route.PlanID, &route.PlanName, &route.Priority, &route.Enabled); err != nil {
			return nil, err
		}
		out = append(out, route)
	}
	return out, rows.Err()
}
func (s *Store) RevokeAPIKey(ctx context.Context, userID, keyID string) error {
	r, err := s.pool.Exec(ctx, `UPDATE api_keys SET status='revoked',updated_at=now() WHERE id=$1 AND user_id=$2`, keyID, userID)
	if err != nil {
		return err
	}
	if r.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) ResolveGatewayRoutes(ctx context.Context, hash []byte, now time.Time) (domain.GatewayRouteSet, error) {
	var out domain.GatewayRouteSet
	err := s.pool.QueryRow(ctx, `SELECT id,user_id,name,key_prefix,strategy,status,last_used_at,created_at FROM api_keys WHERE key_hash=$1 AND status='active'`, hash).Scan(&out.APIKey.ID, &out.APIKey.UserID, &out.APIKey.Name, &out.APIKey.KeyPrefix, &out.APIKey.Strategy, &out.APIKey.Status, &out.APIKey.LastUsedAt, &out.APIKey.CreatedAt)
	if err != nil {
		return out, mapError(err)
	}
	out.APIKey.Routes = make([]domain.APIKeyRoute, 0)
	out.Candidates = make([]domain.GatewayCredential, 0)
	rows, err := s.pool.Query(ctx, `
		SELECT k.id,k.strategy,r.priority,
			m.id,m.plan_id,m.user_id,u.username,u.email,m.role,m.status,m.share_basis_points,m.created_at,
			p.id,p.owner_user_id,p.account_id,p.name,p.status,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode,p.created_at,
			a.id,a.owner_user_id,a.name,a.notes,a.email,a.chatgpt_account_id,a.plan_type,a.access_token_ciphertext,a.refresh_token_ciphertext,a.proxy_url_ciphertext,a.max_concurrency,a.rpm_limit,a.fast_policy,a.token_expires_at,a.status,a.last_error,a.created_at,
			COALESCE((SELECT max(q.used_micros) FROM member_quota_windows q WHERE q.member_id=m.id AND q.account_id=a.id AND q.reset_at>$2),0),
			COALESCE((SELECT max(q.used_micros) FROM account_quota_snapshots q WHERE q.account_id=a.id AND q.reset_at>$2),0)
		FROM api_keys k
		JOIN api_key_plans r ON r.api_key_id=k.id AND r.enabled=true
		JOIN shared_plans p ON p.id=r.plan_id AND p.status='active'
		JOIN plan_members m ON m.plan_id=p.id AND m.user_id=k.user_id AND m.status='active'
		JOIN users u ON u.id=m.user_id
		JOIN openai_accounts a ON a.id=p.account_id AND a.status='active'
		WHERE k.id=$1
		ORDER BY r.priority,p.created_at`, out.APIKey.ID, now)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var credential domain.GatewayCredential
		err := rows.Scan(&credential.APIKeyID, &credential.APIKeyStrategy, &credential.RoutePriority,
			&credential.Member.ID, &credential.Member.PlanID, &credential.Member.UserID, &credential.Member.Username, &credential.Member.Email, &credential.Member.Role, &credential.Member.Status, &credential.Member.ShareBasisPoints, &credential.Member.CreatedAt,
			&credential.Plan.ID, &credential.Plan.OwnerUserID, &credential.Plan.AccountID, &credential.Plan.Name, &credential.Plan.Status, &credential.Plan.Visibility, &credential.Plan.PublicSlots, &credential.Plan.PublicShareBasisPoints, &credential.Plan.AllocationMode, &credential.Plan.CreatedAt,
			&credential.Account.ID, &credential.Account.OwnerUserID, &credential.Account.Name, &credential.Account.Notes, &credential.Account.Email, &credential.Account.ChatGPTAccountID, &credential.Account.PlanType, &credential.AccessTokenCiphertext, &credential.RefreshTokenCiphertext, &credential.ProxyURLCiphertext, &credential.Account.MaxConcurrency, &credential.Account.RPMLimit, &credential.Account.FastPolicy, &credential.TokenExpiresAt, &credential.Account.Status, &credential.Account.LastError, &credential.Account.CreatedAt,
			&credential.UsageMicros, &credential.AccountUsageMicros)
		if err != nil {
			return out, err
		}
		out.Candidates = append(out.Candidates, credential)
	}
	return out, rows.Err()
}

func (s *Store) TouchAPIKey(ctx context.Context, keyID string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=$2,updated_at=$2 WHERE id=$1`, keyID, now)
	return err
}

func (s *Store) MemberQuotaExhausted(ctx context.Context, memberID, accountID string, shareBPS int, now time.Time) (bool, error) {
	var exhausted bool
	limit := int64(shareBPS) * 10_000
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member_quota_windows WHERE member_id=$1 AND account_id=$2 AND reset_at>$3 AND used_micros >= $4)`, memberID, accountID, now, limit).Scan(&exhausted)
	return exhausted, err
}

func (s *Store) AccountQuotaExhausted(ctx context.Context, accountID string, now time.Time) (bool, error) {
	var exhausted bool
	limit := int64(domain.MaxShareBPS) * 10_000
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_quota_snapshots WHERE account_id=$1 AND reset_at>$2 AND used_micros >= $3)`, accountID, now, limit).Scan(&exhausted)
	return exhausted, err
}

func (s *Store) RecordAccountQuotaSignals(ctx context.Context, accountID string, signals []domain.QuotaSignal, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, signal := range signals {
		_, err = tx.Exec(ctx, `INSERT INTO account_quota_snapshots(account_id,window_type,window_start,reset_at,used_micros,updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(account_id,window_type) DO UPDATE SET window_start=EXCLUDED.window_start,reset_at=EXCLUDED.reset_at,used_micros=EXCLUDED.used_micros,updated_at=EXCLUDED.updated_at`, accountID, signal.WindowType, signal.WindowStart, signal.ResetAt, signal.AccountUsedMicros, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) RecordQuotaSignals(ctx context.Context, accountID, memberID string, signals []domain.QuotaSignal, requestID string, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, signal := range signals {
		var oldUsed int64
		var oldStart time.Time
		err = tx.QueryRow(ctx, `SELECT used_micros,window_start FROM account_quota_snapshots WHERE account_id=$1 AND window_type=$2 FOR UPDATE`, accountID, signal.WindowType).Scan(&oldUsed, &oldStart)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		delta := int64(0)
		if err == nil && oldStart.Equal(signal.WindowStart) && signal.AccountUsedMicros > oldUsed {
			delta = signal.AccountUsedMicros - oldUsed
		}
		_, err = tx.Exec(ctx, `INSERT INTO account_quota_snapshots(account_id,window_type,window_start,reset_at,used_micros,updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(account_id,window_type) DO UPDATE SET window_start=EXCLUDED.window_start,reset_at=EXCLUDED.reset_at,used_micros=EXCLUDED.used_micros,updated_at=EXCLUDED.updated_at`, accountID, signal.WindowType, signal.WindowStart, signal.ResetAt, signal.AccountUsedMicros, now)
		if err != nil {
			return err
		}
		if requestID == "" {
			continue
		}
		eventID := requestID + ":" + signal.WindowType
		tag, err := tx.Exec(ctx, `INSERT INTO quota_usage_events(id,member_id,account_id,window_type,window_start,request_id,delta_micros,account_used_micros,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(member_id,account_id,window_type,window_start,request_id) DO NOTHING`, eventID, memberID, accountID, signal.WindowType, signal.WindowStart, requestID, delta, signal.AccountUsedMicros, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		_, err = tx.Exec(ctx, `INSERT INTO member_quota_windows(member_id,account_id,window_type,window_start,reset_at,used_micros,account_used_micros,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(member_id,account_id,window_type,window_start) DO UPDATE SET reset_at=EXCLUDED.reset_at,used_micros=member_quota_windows.used_micros+EXCLUDED.used_micros,account_used_micros=EXCLUDED.account_used_micros,updated_at=EXCLUDED.updated_at`, memberID, accountID, signal.WindowType, signal.WindowStart, signal.ResetAt, delta, signal.AccountUsedMicros, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) RecordGatewayMetric(ctx context.Context, metric domain.GatewayMetric) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO gateway_request_metrics(request_id,api_key_id,plan_id,account_id,member_id,model,service_tier,status_code,ttft_ms,duration_ms,input_tokens,output_tokens,cached_tokens,estimated_cost_micros,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, metric.RequestID, metric.APIKeyID, metric.PlanID, metric.AccountID, metric.MemberID, metric.Model, metric.ServiceTier, metric.StatusCode, metric.TTFT.Milliseconds(), metric.Duration.Milliseconds(), metric.TokenUsage.InputTokens, metric.TokenUsage.OutputTokens, metric.TokenUsage.CachedTokens, metric.AccountCostMicros, metric.CreatedAt)
	return err
}

func (s *Store) Dashboard(ctx context.Context, userID string, todayStart, trendStart, now time.Time) (domain.Dashboard, error) {
	var out domain.Dashboard
	var successToday int64
	err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(g.input_tokens) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(SUM(g.output_tokens) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(SUM(g.cached_tokens) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(SUM(g.input_tokens), 0),
			COALESCE(SUM(g.output_tokens), 0),
			COALESCE(SUM(g.cached_tokens), 0),
			COUNT(*) FILTER (WHERE g.created_at >= $2),
			COUNT(*) FILTER (WHERE g.created_at >= $2 AND g.status_code BETWEEN 200 AND 299),
			COALESCE(AVG(g.ttft_ms) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(AVG(g.duration_ms) FILTER (WHERE g.created_at >= $2), 0),
			COUNT(DISTINCT g.plan_id) FILTER (WHERE g.created_at >= $2),
			COUNT(*) FILTER (WHERE g.created_at >= $3::timestamptz - INTERVAL '1 minute'),
			COALESCE(SUM(g.input_tokens + g.output_tokens) FILTER (WHERE g.created_at >= $3::timestamptz - INTERVAL '1 minute'), 0)
		FROM gateway_request_metrics g
		JOIN plan_members m ON m.id = g.member_id
		WHERE m.user_id = $1 AND g.created_at <= $3::timestamptz`, userID, todayStart, now).Scan(
		&out.TodayTokens.InputTokens,
		&out.TodayTokens.OutputTokens,
		&out.TodayTokens.CachedTokens,
		&out.TotalTokens.InputTokens,
		&out.TotalTokens.OutputTokens,
		&out.TotalTokens.CachedTokens,
		&out.Performance.RequestsToday,
		&successToday,
		&out.Performance.AverageTTFTMs,
		&out.Performance.AverageDurationMs,
		&out.Performance.ActivePlans,
		&out.Performance.RequestsPerMinute,
		&out.Performance.TokensPerMinute,
	)
	if err != nil {
		return out, err
	}
	out.TodayTokens.TotalTokens = out.TodayTokens.InputTokens + out.TodayTokens.OutputTokens
	out.TotalTokens.TotalTokens = out.TotalTokens.InputTokens + out.TotalTokens.OutputTokens
	if out.Performance.RequestsToday > 0 {
		out.Performance.SuccessRate = float64(successToday) / float64(out.Performance.RequestsToday) * 100
	}

	rows, err := s.pool.Query(ctx, `
		WITH buckets AS (
			SELECT generate_series($2::timestamptz, $2::timestamptz + INTERVAL '23 hours', INTERVAL '1 hour') AS bucket_start
		), usage AS (
			SELECT
				date_trunc('hour', g.created_at - $2::timestamptz) + $2::timestamptz AS bucket_start,
				SUM(g.input_tokens) AS input_tokens,
				SUM(g.output_tokens) AS output_tokens,
				SUM(g.cached_tokens) AS cached_tokens
			FROM gateway_request_metrics g
			JOIN plan_members m ON m.id = g.member_id
			WHERE m.user_id = $1 AND g.created_at >= $2::timestamptz AND g.created_at <= $3::timestamptz
			GROUP BY 1
		)
		SELECT b.bucket_start, COALESCE(u.input_tokens, 0), COALESCE(u.output_tokens, 0), COALESCE(u.cached_tokens, 0)
		FROM buckets b
		LEFT JOIN usage u ON u.bucket_start = b.bucket_start
		ORDER BY b.bucket_start`, userID, trendStart, now)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Trend = make([]domain.DashboardTrendPoint, 0, 24)
	for rows.Next() {
		var point domain.DashboardTrendPoint
		if err := rows.Scan(&point.BucketStart, &point.InputTokens, &point.OutputTokens, &point.CachedTokens); err != nil {
			return out, err
		}
		out.Trend = append(out.Trend, point)
	}
	return out, rows.Err()
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return domain.ErrConflict
		case "23503", "23514":
			return domain.ErrInvalidInput
		case "40001":
			return domain.ErrConflict
		}
	}
	return err
}
