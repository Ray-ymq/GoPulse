package businessstate

import (
	"context"
	"database/sql"
	"strings"
)

// CleanupExpired removes evidence only after its outbox and retention charge
// have gone. Open work and live command receipts always protect the event.
func (repository *Repository) CleanupExpired(ctx context.Context, batch int) (int64, error) {
	if batch < 1 || batch > 500 {
		return 0, ErrInvalidArgument
	}
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	now := repository.now()
	rows, err := tx.QueryContext(ctx, `SELECT r.event_id FROM business_event_requirements r
 WHERE r.retention_released=TRUE AND r.closed_at IS NOT NULL AND r.retain_until<=?
 AND NOT EXISTS(SELECT 1 FROM business_outbox o WHERE o.event_id=r.event_id)
 AND NOT EXISTS(SELECT 1 FROM business_commands c WHERE c.event_id=r.event_id AND (c.retained_until IS NULL OR c.retained_until>?))
 AND NOT EXISTS(SELECT 1 FROM business_event_requirements pending WHERE pending.event_id=r.event_id AND (pending.closed_at IS NULL OR pending.retain_until IS NULL OR pending.retain_until>? OR pending.retention_released=FALSE))
 ORDER BY r.retain_until,r.event_id LIMIT ? FOR UPDATE SKIP LOCKED`, now, now, now, batch)
	if err != nil {
		return 0, err
	}
	ids := []any{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return 0, err
	}
	if len(ids) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
		for _, table := range []string{"business_application_results", "business_event_failures", "business_event_requirements", "business_commands"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE event_id IN ("+placeholders+")", ids...); err != nil {
				return 0, err
			}
		}
	}
	// Failed/unknown commands without an event can also expire; NULL expiry
	// is deliberately protected, as are receipts whose event still has evidence.
	result, err := tx.ExecContext(ctx, `DELETE FROM business_commands WHERE retained_until<=? AND event_id IS NULL ORDER BY retained_until LIMIT ?`, now, batch)
	if err != nil {
		return 0, err
	}
	orphaned, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(ids)) + orphaned, nil
}
