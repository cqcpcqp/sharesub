package postgres

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Store) CurrentPricingID(ctx context.Context) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT version_id FROM current_pricing WHERE singleton`).Scan(&id)
	return id, err
}

func (s *Store) PricingVersion(ctx context.Context, id int64) (domain.PricingVersion, error) {
	var version domain.PricingVersion
	err := s.pool.QueryRow(ctx, `SELECT id,published_at,published_by,reason,config FROM pricing_versions WHERE id=$1`, id).Scan(&version.ID, &version.PublishedAt, &version.PublishedBy, &version.Reason, &version.Config)
	return version, mapError(err)
}

func (s *Store) PricingHistory(ctx context.Context, before int64) ([]domain.PricingVersionSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,published_at,published_by,reason FROM pricing_versions WHERE $1::bigint=0 OR id<$1 ORDER BY id DESC LIMIT 50`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []domain.PricingVersionSummary{}
	for rows.Next() {
		var version domain.PricingVersionSummary
		if err := rows.Scan(&version.ID, &version.PublishedAt, &version.PublishedBy, &version.Reason); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

func (s *Store) PublishPricing(ctx context.Context, input domain.PublishPricingInput, event domain.AuditEvent) (domain.PricingVersion, error) {
	var version domain.PricingVersion
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return version, err
	}
	defer tx.Rollback(ctx)
	var current int64
	if err := tx.QueryRow(ctx, `SELECT version_id FROM current_pricing WHERE singleton FOR UPDATE`).Scan(&current); err != nil {
		return version, err
	}
	if current != input.BaseVersionID {
		return version, domain.ErrConflict
	}
	version.Config = input.Config
	version.PublishedBy = event.ActorUserID
	version.Reason = input.Reason
	err = tx.QueryRow(ctx, `INSERT INTO pricing_versions(published_by,reason,config) VALUES($1,$2,$3) RETURNING id,published_at`, version.PublishedBy, version.Reason, version.Config).Scan(&version.ID, &version.PublishedAt)
	if err != nil {
		return version, err
	}
	if _, err := tx.Exec(ctx, `UPDATE current_pricing SET version_id=$1 WHERE singleton`, version.ID); err != nil {
		return version, err
	}
	event.ResourceID = strconv.FormatInt(version.ID, 10)
	event.Metadata, err = json.Marshal(map[string]any{"base_version_id": current, "version_id": version.ID, "reason": input.Reason})
	if err != nil {
		return version, err
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return version, err
	}
	return version, tx.Commit(ctx)
}
