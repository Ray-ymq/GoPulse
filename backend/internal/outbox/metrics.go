package outbox

import (
	"context"
	"database/sql"
	"time"

	"github.com/Ray-ymq/GoPulse/componentmetrics"
)

// SampleMetrics only aggregates indexed status and creation timestamps. It
// never reads an event payload and never runs in a request/metrics handler.
func (repository *Repository) SampleMetrics(ctx context.Context, metrics *componentmetrics.Backend) {
	sample := func() {
		queryCtx, cancel := context.WithTimeout(ctx, time.Second)
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
	}
	sample()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sample()
		}
	}
}
