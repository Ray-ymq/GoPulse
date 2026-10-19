package outbox

import (
	"context"
	"database/sql"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/instrumentation"
)

// SampleMetrics only aggregates indexed status and creation timestamps. It
// never reads an event payload and never runs in a request/metrics handler.
func (repository *Repository) SampleMetrics(ctx context.Context, metrics *instrumentation.Backend) {
	sample := func() error {
		queryCtx, cancel := context.WithTimeout(ctx, repository.databaseTimeout)
		defer cancel()
		var pending int64
		var oldest sql.NullTime
		err := repository.database.QueryRowContext(queryCtx, `SELECT COUNT(*), MIN(created_at) FROM business_outbox WHERE status IN ('pending','leased')`).Scan(&pending, &oldest)
		var age time.Duration
		if oldest.Valid {
			age = time.Since(oldest.Time)
			if age < 0 {
				age = 0
			}
		}
		metrics.ObserveOutbox(pending, age, err)
		return err
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	attempt := uint(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			err := sample()
			delay := 5 * time.Second
			if err != nil {
				delay = storageBackoff(attempt)
				if attempt < 5 {
					attempt++
				}
			} else {
				attempt = 0
			}
			timer.Reset(delay)
		}
	}
}
