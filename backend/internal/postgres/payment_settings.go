package postgres

import (
	"context"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

func (s *Store) PaymentSettings(ctx context.Context) (domain.PaymentSettings, error) {
	var settings domain.PaymentSettings
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(p.base_url,''),COALESCE(p.pid,''),COALESCE(p.key_ciphertext,''::bytea),COALESCE(p.enabled,false),COALESCE(p.revision,0),r.started_at FROM (SELECT true AS singleton) seed LEFT JOIN payment_settings p USING(singleton) LEFT JOIN membership_rollout r USING(singleton)`).Scan(&settings.BaseURL, &settings.PID, &settings.KeyCiphertext, &settings.Enabled, &settings.Revision, &settings.StartedAt)
	return settings, err
}

func (s *Store) SavePaymentSettings(ctx context.Context, settings domain.PaymentSettings, event domain.AuditEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `INSERT INTO payment_settings(singleton,base_url,pid,key_ciphertext,enabled,revision) SELECT true,$1,$2,$3,$4,1 WHERE $5::bigint=0 ON CONFLICT(singleton) DO NOTHING`, settings.BaseURL, settings.PID, settings.KeyCiphertext, settings.Enabled, settings.Revision)
	if err != nil {
		return err
	}
	if settings.Revision > 0 {
		result, err = tx.Exec(ctx, `UPDATE payment_settings SET base_url=$1,pid=$2,key_ciphertext=$3,enabled=$4,revision=revision+1 WHERE singleton AND revision=$5`, settings.BaseURL, settings.PID, settings.KeyCiphertext, settings.Enabled, settings.Revision)
		if err != nil {
			return err
		}
	}
	if result.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	if settings.Enabled {
		if err = activateMemberships(ctx, tx, event.CreatedAt); err != nil {
			return err
		}
	}
	if err = insertAuditEvent(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
