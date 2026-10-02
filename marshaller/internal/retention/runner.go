package retention

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/componentmetrics"
)

type ItemReport struct {
	Stream   Stream        `json:"stream"`
	Index    string        `json:"index"`
	Result   string        `json:"result"`
	Reason   string        `json:"reason,omitempty"`
	Attempts int           `json:"attempts"`
	Duration time.Duration `json:"duration_ns"`
}

type Report struct {
	ClusterUUID string       `json:"cluster_uuid"`
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  time.Time    `json:"finished_at"`
	Items       []ItemReport `json:"items"`
	Err         string       `json:"error,omitempty"`
}

type Runner struct {
	store  Store
	config Config
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
	report func(Report, error)

	mu      sync.Mutex
	cluster string
}

func NewRunner(store Store, config Config) (*Runner, error) {
	if store == nil {
		return nil, errors.New("retention store is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Runner{store: store, config: config, now: time.Now, sleep: sleep}, nil
}

func (r *Runner) Config() Config { return r.config }

// SetClock is used by deterministic lifecycle acceptance to advance the
// evaluation instant without changing a host or dependency clock.
func (r *Runner) SetClock(clock func() time.Time) {
	if clock != nil {
		r.mu.Lock()
		r.now = clock
		r.mu.Unlock()
	}
}

// SetReportObserver installs a bounded diagnostic hook. The runner never
// depends on the hook for correctness and calls it only after a round ends.
func (r *Runner) SetReportObserver(observer func(Report, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report = observer
}

func (r *Runner) publish(report Report, err error) {
	r.mu.Lock()
	observer := r.report
	r.mu.Unlock()
	if observer != nil {
		observer(report, err)
	}
}

func (r *Runner) Run(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	report, err := r.RunOnce(ctx)
	r.publish(report, err)
	ticker := time.NewTicker(r.config.Cycle)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report, err := r.RunOnce(ctx)
			r.publish(report, err)
		}
	}
}

func (r *Runner) RunOnce(parent context.Context) (Report, error) {
	if parent == nil {
		parent = context.Background()
	}
	started := r.now().UTC()
	ctx, cancel := context.WithTimeout(parent, r.config.RoundTimeout)
	defer cancel()
	report := Report{StartedAt: started}
	cluster, err := r.store.ClusterIdentity(ctx)
	if err != nil {
		report.FinishedAt = r.now().UTC()
		report.Err = err.Error()
		return report, err
	}
	r.mu.Lock()
	if r.cluster == "" {
		r.cluster = cluster
	}
	bound := r.cluster
	r.mu.Unlock()
	report.ClusterUUID = cluster
	if cluster != bound {
		report.FinishedAt = r.now().UTC()
		report.Err = "Elasticsearch cluster identity changed"
		return report, errors.New(report.Err)
	}

	indices, err := r.store.List(ctx)
	if err != nil {
		report.FinishedAt = r.now().UTC()
		report.Err = err.Error()
		return report, err
	}
	remaining := r.config.BatchIndices
	for _, policy := range []Policy{r.config.Logs, r.config.Events} {
		for _, index := range indices {
			if ctx.Err() != nil {
				break
			}
			if !strings.HasPrefix(index.Name, policy.Prefix) {
				if policy.Stream == Logs {
					report.Items = append(report.Items, ItemReport{Stream: policy.Stream, Index: index.Name, Result: "not_owned", Reason: "prefix"})
					observe(policy.Stream, "not_owned", 0)
				}
				continue
			}
			if remaining <= 0 {
				report.Items = append(report.Items, ItemReport{Stream: policy.Stream, Index: index.Name, Result: "budget_exhausted", Reason: "index_request_budget"})
				observe(policy.Stream, "budget_exhausted", 0)
				continue
			}
			decision := policy.Classify(index.Name, r.now())
			if decision.Reason != "expired" {
				report.Items = append(report.Items, ItemReport{Stream: policy.Stream, Index: index.Name, Result: decision.Reason})
				observe(policy.Stream, decision.Reason, 0)
				continue
			}
			remaining--
			item := r.remove(ctx, policy, index.Name, bound)
			report.Items = append(report.Items, item)
			observe(policy.Stream, item.Result, item.Duration)
		}
	}
	report.FinishedAt = r.now().UTC()
	return report, nil
}

func (r *Runner) remove(parent context.Context, policy Policy, index, cluster string) ItemReport {
	started := r.now()
	item := ItemReport{Stream: policy.Stream, Index: index, Result: "unknown"}
	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		item.Attempts = attempt + 1
		if parent.Err() != nil {
			item.Result, item.Reason = "budget_exhausted", parent.Err().Error()
			break
		}
		callCtx, cancel := context.WithTimeout(parent, r.config.RequestTimeout)
		ownership, err := r.store.Inspect(callCtx, index, policy, cluster)
		cancel()
		if err != nil {
			if IsPermissionDenied(err) {
				item.Result, item.Reason = "permission_denied", err.Error()
				break
			}
			if !IsTransient(err) || attempt == r.config.MaxRetries {
				item.Result, item.Reason = "transient", err.Error()
				break
			}
			if !r.backoff(parent, attempt) {
				item.Result, item.Reason = "budget_exhausted", parent.Err().Error()
				break
			}
			continue
		}
		if !ownership.Valid {
			item.Result, item.Reason = "not_owned", ownership.Reason
			break
		}
		callCtx, cancel = context.WithTimeout(parent, r.config.RequestTimeout)
		err = r.store.BlockWrites(callCtx, index)
		cancel()
		if err != nil {
			if IsPermissionDenied(err) {
				item.Result, item.Reason = "permission_denied", err.Error()
				break
			}
			if !IsTransient(err) || attempt == r.config.MaxRetries {
				item.Result, item.Reason = "transient", err.Error()
				break
			}
			if !r.backoff(parent, attempt) {
				item.Result, item.Reason = "budget_exhausted", parent.Err().Error()
				break
			}
			continue
		}
		// The second inspection is intentionally after the write block.  It
		// prevents a mapping/alias change between the first check and DELETE
		// from turning a stale ownership decision into an unsafe deletion.
		callCtx, cancel = context.WithTimeout(parent, r.config.RequestTimeout)
		ownership, err = r.store.Inspect(callCtx, index, policy, cluster)
		cancel()
		if err != nil {
			item.Result, item.Reason = "transient", err.Error()
			break
		}
		if !ownership.Valid {
			item.Result, item.Reason = "not_owned", ownership.Reason
			break
		}
		callCtx, cancel = context.WithTimeout(parent, r.config.RequestTimeout)
		err = r.store.DeleteIndex(callCtx, index)
		cancel()
		if err == nil {
			item.Result = "deleted"
			break
		}
		if IsNotFound(err) {
			item.Result, item.Reason = "not_found", "idempotent_absence"
			break
		}
		if IsPermissionDenied(err) {
			item.Result, item.Reason = "permission_denied", err.Error()
			break
		}
		if !IsTransient(err) || attempt == r.config.MaxRetries {
			item.Result, item.Reason = "transient", err.Error()
			break
		}
		if !r.backoff(parent, attempt) {
			item.Result, item.Reason = "budget_exhausted", parent.Err().Error()
			break
		}
	}
	item.Duration = r.now().Sub(started)
	return item
}

func (r *Runner) backoff(ctx context.Context, attempt int) bool {
	delay := r.config.RetryMin
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay >= r.config.RetryMax {
			delay = r.config.RetryMax
			break
		}
	}
	if delay > r.config.RetryMax {
		delay = r.config.RetryMax
	}
	return r.sleep(ctx, delay) == nil
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func observe(stream Stream, result string, duration time.Duration) {
	metrics := componentmetrics.Active()
	if metrics == nil {
		return
	}
	metrics.Observe("retention_cleanup", duration, string(stream), result)
	if result == "deleted" || result == "not_found" {
		metrics.Set("retention_last_success_timestamp_seconds", float64(time.Now().Unix()), string(stream))
		metrics.Set("retention_blocked", 0, string(stream))
	} else if result != "not_expired" && result != "not_owned" && result != "invalid_date" {
		metrics.Set("retention_blocked", 1, string(stream))
	}
	if result == "transient" || result == "permission_denied" || result == "unknown" {
		metrics.Add("retention_retries_total", 1, string(stream), result)
	}
}

func ObserveLate(stream Stream, result string) {
	metrics := componentmetrics.Active()
	if metrics != nil {
		metrics.Add("retention_late_records_total", 1, string(stream), result)
	}
}

func (r *Runner) String(report Report) string {
	return fmt.Sprintf("retention cleanup cluster=%s items=%d", report.ClusterUUID, len(report.Items))
}
