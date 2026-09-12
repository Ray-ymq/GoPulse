package logquery

import (
	"context"
	"github.com/Ray-ymq/GoPulse/backend/internal/alert/count"
	"time"
)

func (r *ElasticsearchRepository) AlertCount(ctx context.Context, labels map[string]string, from, to time.Time) (int64, error) {
	if !ValidateAlertSelector(labels) {
		return 0, ErrUnavailable
	}
	fields := map[string]string{}
	for k, v := range labels {
		fields[k] = v
	}
	n, e := count.Query(ctx, r.client, ReadAlias, fields, from, to)
	if e != nil {
		return 0, ErrUnavailable
	}
	return n, nil
}
