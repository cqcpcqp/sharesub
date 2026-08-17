package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sharesub/sharesub/backend/internal/domain"
)

const quotaWindowResetTolerance = 2 * time.Minute

func initializePlanAccountBinding(ctx context.Context, tx pgx.Tx, planID, accountID string, generation int64, signals []domain.QuotaSignal, observedAt time.Time) error {
	orderedSignals, ok := orderedBindingQuotaSignals(signals)
	if !ok {
		return domain.ErrInvalidInput
	}
	for _, signal := range orderedSignals {
		if _, err := tx.Exec(ctx, `
			INSERT INTO account_quota_snapshots(account_id,window_type,window_start,reset_at,used_micros,updated_at,authoritative,authoritative_at)
			VALUES($1,$2,$3,$4,$5,$6,true,$6)
			ON CONFLICT(account_id,window_type) DO UPDATE SET
				window_start=EXCLUDED.window_start,reset_at=EXCLUDED.reset_at,
				used_micros=EXCLUDED.used_micros,updated_at=EXCLUDED.updated_at,
				authoritative=true,authoritative_at=EXCLUDED.authoritative_at`,
			accountID, signal.WindowType, signal.WindowStart, signal.ResetAt, signal.AccountUsedMicros, observedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO plan_account_quota_baselines(
				plan_id,account_id,account_binding_generation,window_type,window_start,reset_at,
				baseline_used_micros,accounting_started_at,updated_at
			) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8)`,
			planID, accountID, generation, signal.WindowType, signal.WindowStart, signal.ResetAt, signal.AccountUsedMicros, observedAt); err != nil {
			return err
		}
	}
	return nil
}

func hasRequiredBindingQuotaSignals(signals []domain.QuotaSignal) bool {
	_, ok := orderedBindingQuotaSignals(signals)
	return ok
}

// orderedBindingQuotaSignals requires the weekly window, accepts the legacy 5h
// window when OpenAI returns it, and gives every transaction a stable lock order.
func orderedBindingQuotaSignals(signals []domain.QuotaSignal) ([]domain.QuotaSignal, bool) {
	if len(signals) == 0 || len(signals) > 2 {
		return nil, false
	}
	var fiveHour, sevenDay domain.QuotaSignal
	has5H, has7D := false, false
	for _, signal := range signals {
		switch signal.WindowType {
		case domain.Window5H:
			if has5H {
				return nil, false
			}
			has5H = true
			fiveHour = signal
		case domain.Window7D:
			if has7D {
				return nil, false
			}
			has7D = true
			sevenDay = signal
		default:
			return nil, false
		}
	}
	if !has7D {
		return nil, false
	}
	if has5H {
		return []domain.QuotaSignal{fiveHour, sevenDay}, true
	}
	return []domain.QuotaSignal{sevenDay}, true
}

func (s *Store) CreateAPIKey(ctx context.Context, key domain.APIKey, routes []domain.APIKeyRoute) error {
	if key.FastPolicy == nil {
		key.FastPolicy = make([]domain.FastPolicyRule, 0)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO api_keys(id,user_id,name,key_prefix,key_hash,key_ciphertext,strategy,fast_policy,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`, key.ID, key.UserID, key.Name, key.KeyPrefix, key.KeyHash, key.KeyCiphertext, key.Strategy, key.FastPolicy, key.Status, key.CreatedAt)
	if err != nil {
		return mapError(err)
	}
	if err := insertAPIKeyRoutes(ctx, tx, key.ID, key.UserID, routes, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) UpdateAPIKey(ctx context.Context, userID string, key domain.APIKey, routes []domain.APIKeyRoute) (domain.APIKey, error) {
	if key.FastPolicy == nil {
		key.FastPolicy = make([]domain.FastPolicyRule, 0)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `UPDATE api_keys SET name=$3,strategy=$4,fast_policy=$5,updated_at=now() WHERE id=$1 AND user_id=$2 RETURNING id,user_id,name,key_prefix,key_ciphertext,strategy,fast_policy,status,last_used_at,created_at`, key.ID, userID, key.Name, key.Strategy, key.FastPolicy).Scan(&key.ID, &key.UserID, &key.Name, &key.KeyPrefix, &key.KeyCiphertext, &key.Strategy, &key.FastPolicy, &key.Status, &key.LastUsedAt, &key.CreatedAt)
	if err != nil {
		return domain.APIKey{}, mapError(err)
	}
	existingRoutePlanIDs := make(map[string]struct{})
	rows, err := tx.Query(ctx, `SELECT plan_id FROM api_key_plans WHERE api_key_id=$1 FOR UPDATE`, key.ID)
	if err != nil {
		return domain.APIKey{}, err
	}
	for rows.Next() {
		var planID string
		if err := rows.Scan(&planID); err != nil {
			rows.Close()
			return domain.APIKey{}, err
		}
		existingRoutePlanIDs[planID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.APIKey{}, err
	}
	rows.Close()
	if _, err := tx.Exec(ctx, `DELETE FROM api_key_plans WHERE api_key_id=$1`, key.ID); err != nil {
		return domain.APIKey{}, err
	}
	if err := insertAPIKeyRoutes(ctx, tx, key.ID, userID, routes, existingRoutePlanIDs); err != nil {
		return domain.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.APIKey{}, err
	}
	key.Routes = routes
	return key, nil
}

func insertAPIKeyRoutes(ctx context.Context, tx pgx.Tx, keyID, userID string, routes []domain.APIKeyRoute, existingRoutePlanIDs map[string]struct{}) error {
	for _, route := range routes {
		_, existed := existingRoutePlanIDs[route.PlanID]
		result, err := tx.Exec(ctx, `INSERT INTO api_key_plans(api_key_id,plan_id,priority,enabled) SELECT $1,p.id,$3,$4 FROM shared_plans p JOIN plan_members m ON m.plan_id=p.id WHERE p.id=$2 AND (p.status='active' OR $6) AND p.account_id IS NOT NULL AND m.user_id=$5 AND m.status='active'`, keyID, route.PlanID, route.Priority, route.Enabled, userID, existed)
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
	rows, err := s.pool.Query(ctx, `SELECT id,user_id,name,key_prefix,key_ciphertext,strategy,fast_policy,status,last_used_at,created_at FROM api_keys WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.APIKey, 0)
	for rows.Next() {
		var k domain.APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.KeyCiphertext, &k.Strategy, &k.FastPolicy, &k.Status, &k.LastUsedAt, &k.CreatedAt); err != nil {
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
	err := s.pool.QueryRow(ctx, `SELECT id,user_id,name,key_prefix,strategy,fast_policy,status,last_used_at,created_at FROM api_keys WHERE key_hash=$1 AND status='active'`, hash).Scan(&out.APIKey.ID, &out.APIKey.UserID, &out.APIKey.Name, &out.APIKey.KeyPrefix, &out.APIKey.Strategy, &out.APIKey.FastPolicy, &out.APIKey.Status, &out.APIKey.LastUsedAt, &out.APIKey.CreatedAt)
	if err != nil {
		return out, mapError(err)
	}
	out.APIKey.Routes = make([]domain.APIKeyRoute, 0)
	out.Candidates = make([]domain.GatewayCredential, 0)
	rows, err := s.pool.Query(ctx, `
		SELECT k.id,k.strategy,k.fast_policy,r.priority,
			m.id,m.plan_id,m.user_id,u.username,u.email,m.role,m.status,m.share_basis_points,m.created_at,
			p.id,p.owner_user_id,p.account_id,p.name,p.status,p.visibility,p.public_slots,p.public_share_basis_points,p.allocation_mode,p.created_at,p.account_binding_generation,
			a.id,a.owner_user_id,a.name,a.notes,a.email,a.chatgpt_account_id,a.plan_type,a.subscription_expires_at,a.access_token_ciphertext,a.refresh_token_ciphertext,a.proxy_url_ciphertext,a.max_concurrency,a.rpm_limit,a.fast_policy,a.codex_fingerprint_mode,a.token_expires_at,a.status,a.last_error,a.created_at,
			COALESCE((
				SELECT max(CASE WHEN costs.total_cost_micros=0 THEN 0::bigint
					ELSE floor(costs.member_cost_micros::numeric*GREATEST(q.used_micros-b.baseline_used_micros,0)/costs.total_cost_micros)::bigint END)
				FROM account_quota_snapshots q
				JOIN plan_account_quota_baselines b ON b.plan_id=p.id AND b.account_id=a.id
					AND b.account_binding_generation=p.account_binding_generation AND b.window_type=q.window_type
					AND b.reset_at BETWEEN q.reset_at - INTERVAL '2 minutes' AND q.reset_at + INTERVAL '2 minutes'
				CROSS JOIN LATERAL (
					SELECT
						COALESCE(sum(g.estimated_cost_micros) FILTER (WHERE g.member_id=m.id),0)::bigint AS member_cost_micros,
						COALESCE(sum(g.estimated_cost_micros),0)::bigint AS total_cost_micros
					FROM gateway_request_metrics g
					WHERE g.plan_id=p.id AND g.account_id=a.id
						AND g.account_binding_generation=p.account_binding_generation
						AND g.created_at>=GREATEST(q.window_start,b.accounting_started_at)
						AND g.created_at<LEAST($2,q.reset_at)
				) costs
				WHERE q.account_id=a.id AND q.reset_at>$2
			),0),
			COALESCE((SELECT max(q.used_micros) FROM account_quota_snapshots q WHERE q.account_id=a.id AND q.reset_at>$2),0)
		FROM api_keys k
		JOIN api_key_plans r ON r.api_key_id=k.id AND r.enabled=true
		JOIN shared_plans p ON p.id=r.plan_id AND p.status='active'
		JOIN plan_members m ON m.plan_id=p.id AND m.user_id=k.user_id AND m.status='active'
		JOIN users u ON u.id=m.user_id AND u.status='active'
		JOIN openai_accounts a ON a.id=p.account_id AND a.status='active'
		WHERE k.id=$1
		ORDER BY r.priority,p.created_at`, out.APIKey.ID, now)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var credential domain.GatewayCredential
		err := rows.Scan(&credential.APIKeyID, &credential.APIKeyStrategy, &credential.APIKeyFastPolicy, &credential.RoutePriority,
			&credential.Member.ID, &credential.Member.PlanID, &credential.Member.UserID, &credential.Member.Username, &credential.Member.Email, &credential.Member.Role, &credential.Member.Status, &credential.Member.ShareBasisPoints, &credential.Member.CreatedAt,
			&credential.Plan.ID, &credential.Plan.OwnerUserID, &credential.Plan.AccountID, &credential.Plan.Name, &credential.Plan.Status, &credential.Plan.Visibility, &credential.Plan.PublicSlots, &credential.Plan.PublicShareBasisPoints, &credential.Plan.AllocationMode, &credential.Plan.CreatedAt, &credential.AccountBindingGeneration,
			&credential.Account.ID, &credential.Account.OwnerUserID, &credential.Account.Name, &credential.Account.Notes, &credential.Account.Email, &credential.Account.ChatGPTAccountID, &credential.Account.PlanType, &credential.Account.SubscriptionExpiresAt, &credential.AccessTokenCiphertext, &credential.RefreshTokenCiphertext, &credential.ProxyURLCiphertext, &credential.Account.MaxConcurrency, &credential.Account.RPMLimit, &credential.Account.FastPolicy, &credential.Account.CodexFingerprintMode, &credential.TokenExpiresAt, &credential.Account.Status, &credential.Account.LastError, &credential.Account.CreatedAt,
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

func (s *Store) MemberQuotaExhausted(ctx context.Context, memberID, planID, accountID string, generation int64, shareBPS int, now time.Time) (bool, error) {
	if shareBPS == 0 {
		return true, nil
	}
	var exhausted bool
	limit := int64(shareBPS) * 10_000
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM plan_members m
			JOIN account_quota_snapshots q ON q.account_id=$3 AND q.reset_at>$5
			JOIN plan_account_quota_baselines b ON b.plan_id=$2 AND b.account_id=q.account_id
				AND b.account_binding_generation=$4 AND b.window_type=q.window_type
				AND b.reset_at BETWEEN q.reset_at - INTERVAL '2 minutes' AND q.reset_at + INTERVAL '2 minutes'
			CROSS JOIN LATERAL (
				SELECT
					COALESCE(sum(g.estimated_cost_micros) FILTER (WHERE g.member_id=m.id),0)::bigint AS member_cost_micros,
					COALESCE(sum(g.estimated_cost_micros),0)::bigint AS total_cost_micros
				FROM gateway_request_metrics g
				WHERE g.plan_id=$2 AND g.account_id=q.account_id
					AND g.account_binding_generation=$4
					AND g.created_at>=GREATEST(q.window_start,b.accounting_started_at)
					AND g.created_at<LEAST($5,q.reset_at)
			) costs
			WHERE m.id=$1 AND m.plan_id=$2 AND costs.total_cost_micros>0
				AND floor(costs.member_cost_micros::numeric*GREATEST(q.used_micros-b.baseline_used_micros,0)/costs.total_cost_micros)>=$6
		)`, memberID, planID, accountID, generation, now, limit).Scan(&exhausted)
	return exhausted, err
}

func (s *Store) AccountQuotaExhausted(ctx context.Context, accountID string, now time.Time) (bool, error) {
	var exhausted bool
	limit := int64(domain.MaxShareBPS) * 10_000
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_quota_snapshots WHERE account_id=$1 AND reset_at>$2 AND used_micros >= $3)`, accountID, now, limit).Scan(&exhausted)
	return exhausted, err
}

func (s *Store) RecordAccountQuotaSignals(ctx context.Context, planID, accountID string, generation int64, signals []domain.QuotaSignal, now time.Time) error {
	return s.recordAccountQuotaSignals(ctx, planID, accountID, generation, signals, now, false)
}

// RecordProbedAccountQuotaSignals persists an actively queried upstream
// snapshot as authoritative. Unlike passive gateway observations, a probe can
// intentionally switch quota dimensions, so an earlier reset_at must be able
// to replace a later snapshot from a different upstream quota bucket.
func (s *Store) RecordProbedAccountQuotaSignals(ctx context.Context, planID, accountID string, generation int64, signals []domain.QuotaSignal, now time.Time) error {
	return s.recordAccountQuotaSignals(ctx, planID, accountID, generation, signals, now, true)
}

func (s *Store) recordAccountQuotaSignals(ctx context.Context, planID, accountID string, generation int64, signals []domain.QuotaSignal, now time.Time, authoritative bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var currentAccountID string
	var currentGeneration int64
	if err := tx.QueryRow(ctx, `SELECT account_id,account_binding_generation FROM shared_plans WHERE id=$1 FOR UPDATE`, planID).Scan(&currentAccountID, &currentGeneration); err != nil {
		return mapError(err)
	}
	if currentAccountID != accountID || currentGeneration != generation {
		return domain.ErrConflict
	}
	if authoritative {
		if err := removeMissingOptionalQuotaWindows(ctx, tx, accountID, signals); err != nil {
			return err
		}
	}
	for _, signal := range signals {
		var snapshotAuthoritative bool
		var authoritativeAt time.Time
		var accepted bool
		signal, snapshotAuthoritative, authoritativeAt, accepted, err = lockAndReconcileAccountQuotaSignal(ctx, tx, accountID, signal, authoritative, now)
		if err != nil {
			return err
		}
		if !accepted {
			// A newer authoritative probe already committed. Roll this whole
			// transaction back so an optional-window deletion or an earlier
			// signal from the stale batch cannot partially survive.
			if authoritative {
				return nil
			}
			continue
		}
		var authoritativeAtValue any
		if snapshotAuthoritative {
			authoritativeAtValue = authoritativeAt
		}
		_, err = tx.Exec(ctx, `INSERT INTO account_quota_snapshots(account_id,window_type,window_start,reset_at,used_micros,updated_at,authoritative,authoritative_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(account_id,window_type) DO UPDATE SET window_start=EXCLUDED.window_start,reset_at=EXCLUDED.reset_at,used_micros=EXCLUDED.used_micros,updated_at=EXCLUDED.updated_at,authoritative=EXCLUDED.authoritative,authoritative_at=EXCLUDED.authoritative_at`, accountID, signal.WindowType, signal.WindowStart, signal.ResetAt, signal.AccountUsedMicros, now, snapshotAuthoritative, authoritativeAtValue)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `
			INSERT INTO plan_account_quota_baselines(
				plan_id,account_id,account_binding_generation,window_type,window_start,reset_at,
				baseline_used_micros,accounting_started_at,updated_at
			)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8)
			ON CONFLICT(plan_id,account_binding_generation,window_type) DO UPDATE SET
				window_start=EXCLUDED.window_start,reset_at=EXCLUDED.reset_at,
				baseline_used_micros=0,accounting_started_at=EXCLUDED.window_start,
				updated_at=EXCLUDED.updated_at
			WHERE ABS(EXTRACT(EPOCH FROM (plan_account_quota_baselines.reset_at-EXCLUDED.reset_at))) > 120`,
			planID, accountID, generation, signal.WindowType, signal.WindowStart, signal.ResetAt,
			signal.AccountUsedMicros, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) RecordQuotaResetSignals(ctx context.Context, planID, accountID string, generation int64, signals []domain.QuotaSignal, now time.Time) error {
	orderedSignals, ok := orderedBindingQuotaSignals(signals)
	if !ok {
		return domain.ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var currentAccountID string
	var currentGeneration int64
	if err := tx.QueryRow(ctx, `SELECT account_id,account_binding_generation FROM shared_plans WHERE id=$1 FOR UPDATE`, planID).Scan(&currentAccountID, &currentGeneration); err != nil {
		return mapError(err)
	}
	if currentAccountID != accountID || currentGeneration != generation {
		return domain.ErrConflict
	}
	if err := removeMissingOptionalQuotaWindows(ctx, tx, accountID, orderedSignals); err != nil {
		return err
	}
	for _, signal := range orderedSignals {
		_, err = tx.Exec(ctx, `INSERT INTO account_quota_snapshots(account_id,window_type,window_start,reset_at,used_micros,updated_at,authoritative,authoritative_at) VALUES($1,$2,$3,$4,$5,$6,true,$6) ON CONFLICT(account_id,window_type) DO UPDATE SET window_start=EXCLUDED.window_start,reset_at=EXCLUDED.reset_at,used_micros=EXCLUDED.used_micros,updated_at=EXCLUDED.updated_at,authoritative=true,authoritative_at=EXCLUDED.authoritative_at`, accountID, signal.WindowType, signal.WindowStart, signal.ResetAt, signal.AccountUsedMicros, now)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `
			INSERT INTO plan_account_quota_baselines(
				plan_id,account_id,account_binding_generation,window_type,window_start,reset_at,
				baseline_used_micros,accounting_started_at,updated_at
			) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8)
			ON CONFLICT(plan_id,account_binding_generation,window_type) DO UPDATE SET
				account_id=EXCLUDED.account_id,window_start=EXCLUDED.window_start,reset_at=EXCLUDED.reset_at,
				baseline_used_micros=EXCLUDED.baseline_used_micros,
				accounting_started_at=EXCLUDED.accounting_started_at,updated_at=EXCLUDED.updated_at`,
			planID, accountID, generation, signal.WindowType, signal.WindowStart, signal.ResetAt, signal.AccountUsedMicros, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// A valid weekly-only response is the complete current quota state. Remove a
// previously recorded optional 5h window so stale exhaustion and attribution
// state cannot survive a refresh or an external quota reset.
func removeMissingOptionalQuotaWindows(ctx context.Context, tx pgx.Tx, accountID string, signals []domain.QuotaSignal) error {
	has5H, has7D := false, false
	for _, signal := range signals {
		switch signal.WindowType {
		case domain.Window5H:
			has5H = true
		case domain.Window7D:
			has7D = true
		}
	}
	if !has7D || has5H {
		return nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM plan_account_quota_baselines WHERE account_id=$1 AND window_type='5h'`, accountID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM account_quota_snapshots WHERE account_id=$1 AND window_type='5h'`, accountID)
	return err
}

func lockAndReconcileAccountQuotaSignal(ctx context.Context, tx pgx.Tx, accountID string, signal domain.QuotaSignal, authoritative bool, observedAt time.Time) (domain.QuotaSignal, bool, time.Time, bool, error) {
	var oldUsed int64
	var oldStart, oldReset time.Time
	var oldAuthoritative bool
	var oldAuthoritativeAt time.Time
	err := tx.QueryRow(ctx, `SELECT used_micros,window_start,reset_at,authoritative,COALESCE(authoritative_at,to_timestamp(0)) FROM account_quota_snapshots WHERE account_id=$1 AND window_type=$2 FOR UPDATE`, accountID, signal.WindowType).Scan(&oldUsed, &oldStart, &oldReset, &oldAuthoritative, &oldAuthoritativeAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return signal, authoritative, observedAt, true, nil
	}
	if err != nil {
		return domain.QuotaSignal{}, false, time.Time{}, false, err
	}
	return reconcileAccountQuotaSignal(oldStart, oldReset, oldUsed, oldAuthoritative, oldAuthoritativeAt, signal, authoritative, observedAt)
}

func reconcileAccountQuotaSignal(oldStart, oldReset time.Time, oldUsed int64, oldAuthoritative bool, oldAuthoritativeAt time.Time, signal domain.QuotaSignal, authoritative bool, observedAt time.Time) (domain.QuotaSignal, bool, time.Time, bool, error) {
	if authoritative {
		if oldAuthoritative && !observedAt.After(oldAuthoritativeAt) {
			return domain.QuotaSignal{}, true, oldAuthoritativeAt, false, nil
		}
		return signal, true, observedAt, true, nil
	}
	if oldAuthoritative && !sameQuotaWindow(oldReset, signal.ResetAt) {
		return domain.QuotaSignal{}, true, oldAuthoritativeAt, false, nil
	}
	merged, err := mergeAccountQuotaSignal(oldStart, oldReset, oldUsed, signal)
	return merged, oldAuthoritative, oldAuthoritativeAt, true, err
}

func mergeAccountQuotaSignal(oldStart, oldReset time.Time, oldUsed int64, signal domain.QuotaSignal) (domain.QuotaSignal, error) {
	if !sameQuotaWindow(oldReset, signal.ResetAt) {
		if signal.ResetAt.After(oldReset) {
			return signal, nil
		}
		return domain.QuotaSignal{
			WindowType:        signal.WindowType,
			WindowStart:       oldStart,
			ResetAt:           oldReset,
			AccountUsedMicros: oldUsed,
		}, nil
	}
	signal.WindowStart = oldStart
	signal.ResetAt = oldReset
	if signal.AccountUsedMicros <= oldUsed {
		signal.AccountUsedMicros = oldUsed
		return signal, nil
	}
	return signal, nil
}

func sameQuotaWindow(left, right time.Time) bool {
	difference := left.Sub(right)
	if difference < 0 {
		difference = -difference
	}
	return difference <= quotaWindowResetTolerance
}

func (s *Store) RecordGatewayMetric(ctx context.Context, metric domain.GatewayMetric) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO gateway_request_metrics(
		request_id,api_key_id,plan_id,account_id,member_id,model,requested_model,upstream_model,billing_model,service_tier,
		endpoint,is_stream,status_code,error_source,error_code,error_message,ttft_ms,duration_ms,input_tokens,output_tokens,cached_tokens,cache_creation_tokens,image_input_tokens,image_output_tokens,
		image_count,web_search_calls,input_cost_micros,output_cost_micros,cache_creation_cost_micros,cache_read_cost_micros,
		image_input_cost_micros,image_output_cost_micros,web_search_cost_micros,estimated_cost_micros,created_at,account_binding_generation
	) VALUES(
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36
	) ON CONFLICT(request_id,api_key_id,plan_id,account_id,account_binding_generation) DO NOTHING`,
		metric.RequestID, metric.APIKeyID, metric.PlanID, metric.AccountID, metric.MemberID, metric.Model, metric.RequestedModel, metric.UpstreamModel, metric.BillingModel, metric.ServiceTier,
		metric.Endpoint, metric.IsStream, metric.StatusCode, metric.ErrorSource, metric.ErrorCode, metric.ErrorMessage,
		metric.TTFT.Milliseconds(), metric.Duration.Milliseconds(), metric.TokenUsage.InputTokens, metric.TokenUsage.OutputTokens, metric.TokenUsage.CachedTokens, metric.TokenUsage.CacheCreationTokens, metric.TokenUsage.ImageInputTokens, metric.TokenUsage.ImageOutputTokens,
		metric.ImageCount, metric.WebSearchCalls, metric.CostBreakdown.InputMicros, metric.CostBreakdown.OutputMicros, metric.CostBreakdown.CacheCreationMicros, metric.CostBreakdown.CacheReadMicros,
		metric.CostBreakdown.ImageInputMicros, metric.CostBreakdown.ImageOutputMicros, metric.CostBreakdown.WebSearchMicros, metric.AccountCostMicros, metric.CreatedAt, metric.AccountBindingGeneration)
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
			COALESCE(SUM(g.cache_creation_tokens) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(SUM(g.image_input_tokens) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(SUM(g.image_output_tokens) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(SUM(g.image_count) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(SUM(g.web_search_calls) FILTER (WHERE g.created_at >= $2), 0),
			COALESCE(SUM(g.input_tokens), 0) + COALESCE((SELECT SUM(r.input_tokens) FROM gateway_metric_daily_rollups r WHERE r.user_id=$1), 0),
			COALESCE(SUM(g.output_tokens), 0) + COALESCE((SELECT SUM(r.output_tokens) FROM gateway_metric_daily_rollups r WHERE r.user_id=$1), 0),
			COALESCE(SUM(g.cached_tokens), 0) + COALESCE((SELECT SUM(r.cached_tokens) FROM gateway_metric_daily_rollups r WHERE r.user_id=$1), 0),
			COALESCE(SUM(g.cache_creation_tokens), 0) + COALESCE((SELECT SUM(r.cache_creation_tokens) FROM gateway_metric_daily_rollups r WHERE r.user_id=$1), 0),
			COALESCE(SUM(g.image_input_tokens), 0) + COALESCE((SELECT SUM(r.image_input_tokens) FROM gateway_metric_daily_rollups r WHERE r.user_id=$1), 0),
			COALESCE(SUM(g.image_output_tokens), 0) + COALESCE((SELECT SUM(r.image_output_tokens) FROM gateway_metric_daily_rollups r WHERE r.user_id=$1), 0),
			COALESCE(SUM(g.image_count), 0) + COALESCE((SELECT SUM(r.image_count) FROM gateway_metric_daily_rollups r WHERE r.user_id=$1), 0),
			COALESCE(SUM(g.web_search_calls), 0) + COALESCE((SELECT SUM(r.web_search_calls) FROM gateway_metric_daily_rollups r WHERE r.user_id=$1), 0),
			COUNT(*) FILTER (WHERE g.created_at >= $2),
			COUNT(*) FILTER (WHERE g.created_at >= $2 AND g.status_code BETWEEN 200 AND 299),
			COALESCE(AVG(g.ttft_ms) FILTER (WHERE g.created_at >= $2 AND g.ttft_ms > 0), 0),
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
		&out.TodayTokens.CacheCreationTokens,
		&out.TodayTokens.ImageInputTokens,
		&out.TodayTokens.ImageOutputTokens,
		&out.TodayTokens.ImageCount,
		&out.TodayWebSearchCalls,
		&out.TotalTokens.InputTokens,
		&out.TotalTokens.OutputTokens,
		&out.TotalTokens.CachedTokens,
		&out.TotalTokens.CacheCreationTokens,
		&out.TotalTokens.ImageInputTokens,
		&out.TotalTokens.ImageOutputTokens,
		&out.TotalTokens.ImageCount,
		&out.TotalWebSearchCalls,
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
				SUM(g.cached_tokens) AS cached_tokens,
				SUM(g.cache_creation_tokens) AS cache_creation_tokens,
				SUM(g.image_input_tokens) AS image_input_tokens,
				SUM(g.image_output_tokens) AS image_output_tokens,
				SUM(g.image_count) AS image_count,
				SUM(g.web_search_calls) AS web_search_calls
			FROM gateway_request_metrics g
			JOIN plan_members m ON m.id = g.member_id
			WHERE m.user_id = $1 AND g.created_at >= $2::timestamptz AND g.created_at <= $3::timestamptz
			GROUP BY 1
		)
		SELECT b.bucket_start, COALESCE(u.input_tokens, 0), COALESCE(u.output_tokens, 0), COALESCE(u.cached_tokens, 0),
			COALESCE(u.cache_creation_tokens,0),COALESCE(u.image_input_tokens,0),COALESCE(u.image_output_tokens,0),
			COALESCE(u.image_count,0),COALESCE(u.web_search_calls,0)
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
		if err := rows.Scan(&point.BucketStart, &point.InputTokens, &point.OutputTokens, &point.CachedTokens,
			&point.CacheCreationTokens, &point.ImageInputTokens, &point.ImageOutputTokens, &point.ImageCount, &point.WebSearchCalls); err != nil {
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
