package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/instrumentation"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	DefaultDispatcherPollInterval    = time.Second
	MinimumDispatcherPollInterval    = 10 * time.Millisecond
	MaximumDispatcherPollInterval    = time.Minute
	DefaultDispatcherClaimBatch      = 50
	DefaultDispatcherLease           = 5 * time.Minute
	DefaultDispatcherPublish         = 5 * time.Second
	MinimumDispatcherPublish         = 10 * time.Millisecond
	MaximumDispatcherPublish         = 30 * time.Second
	DispatcherLeaseSafetyMargin      = time.Second
	DefaultDispatcherCleanupInterval = time.Hour
	MinimumDispatcherCleanupInterval = time.Minute
	MaximumDispatcherCleanupInterval = 24 * time.Hour
	DefaultDispatcherRetention       = 7 * 24 * time.Hour
	MinimumDispatcherRetention       = time.Hour
	MaximumDispatcherRetention       = 365 * 24 * time.Hour
	DefaultDispatcherCleanupBatch    = 500
	dispatcherCleanupBatchDelay      = time.Second
)

// Store is the lease-aware persistence boundary used by Dispatcher. Keeping
// this interface small makes the delivery loop independently testable while
// ensuring all state transitions remain in the Outbox Repository.
type Store interface {
	Claim(context.Context, string, int, time.Duration) ([]Record, error)
	MarkPublished(context.Context, uint64, string) error
	ReleaseFailed(context.Context, uint64, string, FailureCode) error
	CleanupPublished(context.Context, time.Time, int) (int64, error)
}

// BatchStore commits successful confirms together and preserves per-item lease results.
type BatchStore interface {
	MarkPublishedBatch(context.Context, []uint64, string) ([]error, error)
}

// Publisher publishes one validated business event and returns only bounded
// failure categories through PublishError. It must not mark Outbox state.
type Publisher interface {
	Publish(context.Context, bus.Envelope) error
}

type DispatcherOptions struct {
	Publishers      []Publisher
	DatabaseTimeout time.Duration
	Owner           string
	PollInterval    time.Duration
	ClaimBatch      int
	LeaseDuration   time.Duration
	PublishTimeout  time.Duration
	CleanupInterval time.Duration
	Retention       time.Duration
	CleanupBatch    int
	Clock           Clock
	Logger          *slog.Logger
}

type Dispatcher struct {
	cleanupStartupDelay time.Duration
	publishers          []Publisher
	databaseTimeout     time.Duration
	store               Store
	publisher           Publisher
	owner               string
	pollInterval        time.Duration
	claimBatch          int
	leaseDuration       time.Duration
	publishTimeout      time.Duration
	cleanupInterval     time.Duration
	retention           time.Duration
	cleanupBatch        int
	clock               Clock
	logger              *slog.Logger
}

func NewDispatcher(store Store, publisher Publisher, options DispatcherOptions) (*Dispatcher, error) {
	if store == nil {
		return nil, fmt.Errorf("create outbox dispatcher: store is required")
	}
	if publisher == nil {
		return nil, fmt.Errorf("create outbox dispatcher: publisher is required")
	}
	owner := options.Owner
	if owner == "" {
		owner = defaultDispatcherOwner()
	}
	if err := validateOwner(owner); err != nil {
		return nil, fmt.Errorf("create outbox dispatcher: %w", err)
	}
	pollInterval := options.PollInterval
	if pollInterval == 0 {
		pollInterval = DefaultDispatcherPollInterval
	}
	if pollInterval < MinimumDispatcherPollInterval || pollInterval > MaximumDispatcherPollInterval {
		return nil, fmt.Errorf("create outbox dispatcher: poll interval must be between %s and %s", MinimumDispatcherPollInterval, MaximumDispatcherPollInterval)
	}
	claimBatch := options.ClaimBatch
	if claimBatch == 0 {
		claimBatch = DefaultDispatcherClaimBatch
	}
	if claimBatch < 1 || claimBatch > DefaultMaxClaimBatch {
		return nil, fmt.Errorf("create outbox dispatcher: claim batch must be between 1 and %d", DefaultMaxClaimBatch)
	}
	leaseDuration := options.LeaseDuration
	if leaseDuration == 0 {
		leaseDuration = DefaultDispatcherLease
	}
	if leaseDuration < minimumLeaseDuration || leaseDuration > maximumLeaseDuration {
		return nil, fmt.Errorf("create outbox dispatcher: lease duration must be between %s and %s", minimumLeaseDuration, maximumLeaseDuration)
	}
	publishTimeout := options.PublishTimeout
	if publishTimeout == 0 {
		publishTimeout = DefaultDispatcherPublish
	}
	if publishTimeout < MinimumDispatcherPublish || publishTimeout > MaximumDispatcherPublish {
		return nil, fmt.Errorf("create outbox dispatcher: publish timeout must be between %s and %s", MinimumDispatcherPublish, MaximumDispatcherPublish)
	}
	publishers := options.Publishers
	if len(publishers) == 0 {
		publishers = []Publisher{publisher}
	}
	if len(publishers) > 32 {
		return nil, errors.New("publish concurrency must be at most 32")
	}
	for _, p := range publishers {
		if p == nil {
			return nil, errors.New("nil publisher")
		}
	}
	dbTimeout := options.DatabaseTimeout
	if dbTimeout == 0 {
		dbTimeout = 2 * time.Second
	}
	if dbTimeout < 10*time.Millisecond || dbTimeout > 30*time.Second {
		return nil, errors.New("invalid database timeout")
	}
	requiredLease := time.Duration((claimBatch+len(publishers)-1)/len(publishers))*publishTimeout + time.Duration(claimBatch+2)*dbTimeout + DispatcherLeaseSafetyMargin
	if leaseDuration < requiredLease {
		return nil, fmt.Errorf("create outbox dispatcher: lease duration must cover the full claim batch publish budget plus %s safety margin", DispatcherLeaseSafetyMargin)
	}
	cleanupInterval := options.CleanupInterval
	if cleanupInterval == 0 {
		cleanupInterval = DefaultDispatcherCleanupInterval
	}
	if cleanupInterval < MinimumDispatcherCleanupInterval || cleanupInterval > MaximumDispatcherCleanupInterval {
		return nil, fmt.Errorf("create outbox dispatcher: cleanup interval must be between %s and %s", MinimumDispatcherCleanupInterval, MaximumDispatcherCleanupInterval)
	}
	retention := options.Retention
	if retention == 0 {
		retention = DefaultDispatcherRetention
	}
	if retention < MinimumDispatcherRetention || retention > MaximumDispatcherRetention {
		return nil, fmt.Errorf("create outbox dispatcher: retention must be between %s and %s", MinimumDispatcherRetention, MaximumDispatcherRetention)
	}
	cleanupBatch := options.CleanupBatch
	if cleanupBatch == 0 {
		cleanupBatch = DefaultDispatcherCleanupBatch
	}
	if cleanupBatch < 1 || cleanupBatch > maximumCleanupBatch {
		return nil, fmt.Errorf("create outbox dispatcher: cleanup batch must be between 1 and %d", maximumCleanupBatch)
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	logger := options.Logger
	if logger == nil {
		logger = logging.Module(logging.Discard("backend"), "outbox")
	}
	return &Dispatcher{
		cleanupStartupDelay: time.Minute,
		publishers:          publishers, databaseTimeout: dbTimeout,
		store:           store,
		publisher:       publisher,
		owner:           owner,
		pollInterval:    pollInterval,
		claimBatch:      claimBatch,
		leaseDuration:   leaseDuration,
		publishTimeout:  publishTimeout,
		cleanupInterval: cleanupInterval,
		retention:       retention,
		cleanupBatch:    cleanupBatch,
		clock:           clock,
		logger:          logger,
	}, nil
}

// Run continuously claims and publishes bounded batches while a separately
// owned cleanup loop removes expired published rows. Cancellation stops both
// loops before Run returns.
func (dispatcher *Dispatcher) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("run outbox dispatcher: context is required")
	}
	cleanupContext, cancelCleanup := context.WithCancel(ctx)
	cleanupDone := make(chan struct{})
	go func() {
		dispatcher.runCleanup(cleanupContext)
		close(cleanupDone)
	}()

	err := dispatcher.runDelivery(ctx)
	cancelCleanup()
	<-cleanupDone
	return err
}

func (dispatcher *Dispatcher) runDelivery(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	attempt := uint(0)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			if ctx.Err() != nil {
				return nil
			}
			more, err := dispatcher.dispatchBatch(ctx)
			if err == nil && more {
				attempt = 0
				// A full claim batch is direct evidence that backlog remains.
				// Continue draining it without imposing the idle poll interval.
				timer.Reset(0)
				continue
			}
			delay := dispatcher.pollInterval
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

func (dispatcher *Dispatcher) runCleanup(ctx context.Context) {
	attempt := uint(0)
	timer := time.NewTimer(dispatcher.cleanupStartupDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			delay := dispatcher.cleanupInterval
			if err := dispatcher.CleanupOnce(ctx); err != nil && ctx.Err() == nil {
				dispatcher.logger.Error("outbox cleanup failed", slog.String("reason", "storage_unavailable"))
				delay = max(time.Second, storageBackoff(attempt))
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

// CleanupOnce removes all currently expired published rows in bounded batches.
// A fixed cutoff keeps each cycle deterministic, and full batches yield before
// continuing so cleanup cannot monopolize the database.
func (dispatcher *Dispatcher) CleanupOnce(ctx context.Context) error {
	if ctx == nil {
		return errors.New("cleanup outbox events: context is required")
	}
	cutoff := dispatcher.clock().UTC().Add(-dispatcher.retention)
	for {
		dbCtx, cancel := context.WithTimeout(ctx, dispatcher.databaseTimeout)
		deleted, err := dispatcher.store.CleanupPublished(dbCtx, cutoff, dispatcher.cleanupBatch)
		cancel()
		if err != nil {
			return fmt.Errorf("cleanup published outbox events: %w", err)
		}
		more := deleted == int64(dispatcher.cleanupBatch)
		if scanner, ok := dispatcher.store.(interface{ CleanupHasMore() bool }); ok {
			more = scanner.CleanupHasMore()
		}
		if !more {
			return nil
		}
		if err := waitForCleanup(ctx, dispatcherCleanupBatchDelay); err != nil {
			return err
		}
	}
}

func waitForCleanup(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// DispatchOnce performs one bounded claim/publish cycle. It is exported for
// deterministic integration and lifecycle tests; production uses Run.
func (dispatcher *Dispatcher) DispatchOnce(ctx context.Context) error {
	_, err := dispatcher.dispatchBatch(ctx)
	return err
}

func (dispatcher *Dispatcher) dispatchBatch(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, errors.New("dispatch outbox events: context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	dbCtx, cancel := context.WithTimeout(ctx, dispatcher.databaseTimeout)
	records, err := dispatcher.store.Claim(dbCtx, dispatcher.owner, dispatcher.claimBatch, dispatcher.leaseDuration)
	cancel()
	if err != nil {
		dispatcher.logger.Error("outbox claim failed", slog.String("reason", "storage_unavailable"))
		return false, fmt.Errorf("claim outbox events: %w", err)
	}

	if _, ok := dispatcher.store.(BatchStore); !ok && len(dispatcher.publishers) == 1 {
		var firstErr error
		for _, record := range records {
			if err := dispatcher.dispatchRecord(ctx, record); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return len(records) == dispatcher.claimBatch, firstErr
	}
	var firstErr error
	var mu sync.Mutex
	successful := []uint64{}
	jobs := make(chan Record)
	var workers sync.WaitGroup
	for _, publisher := range dispatcher.publishers {
		workers.Add(1)
		go func(p Publisher) {
			defer workers.Done()
			for record := range jobs {
				err := dispatcher.dispatchRecordWith(ctx, record, p, func(_ context.Context, id uint64, _ string) error {
					mu.Lock()
					successful = append(successful, id)
					mu.Unlock()
					return nil
				})
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
				}
			}
		}(publisher)
	}
	for _, record := range records {
		jobs <- record
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if len(successful) > 0 && ctx.Err() == nil {
		dbCtx, cancel := context.WithTimeout(ctx, dispatcher.databaseTimeout)
		if batchStore, ok := dispatcher.store.(BatchStore); ok {
			results, err := batchStore.MarkPublishedBatch(dbCtx, successful, dispatcher.owner)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
			} else {
				for _, err := range results {
					if err != nil && firstErr == nil {
						firstErr = err
					}
				}
			}
		} else {
			for _, id := range successful {
				if err := dispatcher.store.MarkPublished(dbCtx, id, dispatcher.owner); err != nil && firstErr == nil {
					firstErr = err
				}
			}
		}
		cancel()
	}
	return len(records) == dispatcher.claimBatch, firstErr
}

func storageBackoff(attempt uint) time.Duration {
	delay := time.Second * time.Duration(1<<min(attempt, 5))
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	// Equal jitter remains bounded by the configured exponential cap.
	return delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1))
}

func (dispatcher *Dispatcher) dispatchRecord(ctx context.Context, record Record) error {
	return dispatcher.dispatchRecordWith(ctx, record, dispatcher.publisher, dispatcher.store.MarkPublished)
}

func (dispatcher *Dispatcher) dispatchRecordWith(ctx context.Context, record Record, publisher Publisher, mark func(context.Context, uint64, string) error) error {
	envelope, err := record.Envelope()
	if err != nil {
		dispatcher.logger.Error("outbox event invalid",
			slog.Uint64("outbox_id", record.ID),
			slog.String("event_id", record.EventID),
			slog.String("event_type", string(record.EventType)),
			slog.String("reason", "invalid_envelope"),
		)
		releaseErr := dispatcher.releaseFailed(ctx, record, FailureInternal)
		if releaseErr != nil {
			return releaseErr
		}
		return fmt.Errorf("decode outbox event: %w", err)
	}
	if envelope.HasInvalidTraceContext() {
		tracing.RecordInvalidContext()
		if backendMetrics := instrumentation.BackendActive(); backendMetrics != nil {
			backendMetrics.ObserveTraceContextInvalid()
		}
	}

	traceContext, span := tracing.StartFromWire(ctx, "outbox.publish", tracing.WireContext{
		TraceParent: envelope.TraceParent,
		TraceState:  envelope.TraceState,
	},
		attribute.String("gopulse.event_id", envelope.EventID),
		attribute.String("gopulse.event_type", string(envelope.EventType)),
		attribute.String("gopulse.data_epoch", envelope.DataEpoch),
		attribute.String("gopulse.command_id", envelope.CommandID),
		attribute.Int64("gopulse.post_id", int64(envelope.PostID)),
		attribute.Int64("gopulse.content_revision", int64(envelope.ContentRevision)),
		attribute.Int64("gopulse.outbox_id", int64(record.ID)),
	)
	defer span.End()
	attemptID := tracing.NewAttemptID()
	traceContext = tracing.WithAttemptID(traceContext, attemptID)
	span.SetAttributes(attribute.String("gopulse.attempt_id", attemptID))
	traceContext = tracing.WithOutboxID(traceContext, record.ID)
	publishContext, cancel := context.WithTimeout(traceContext, dispatcher.publishTimeout)
	publishStarted := dispatcher.clock()
	publishErr := publisher.Publish(publishContext, envelope)
	cancel()
	if backendMetrics := instrumentation.BackendActive(); backendMetrics != nil {
		result := "success"
		if publishErr != nil {
			result = "failure"
		}
		backendMetrics.ObserveFreshness("publish", result, dispatcher.clock().Sub(publishStarted))
	}
	if publishErr != nil {
		span.SetStatus(codes.Error, "publish_failed")
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A bounded publish failure is handled as part of the state machine:
		// release it for a later attempt and keep processing the claimed batch.
		reason := publishFailureCode(publishErr)
		if err := dispatcher.releaseFailed(traceContext, record, reason); err != nil {
			return err
		}
		attributes := []any{slog.Uint64("outbox_id", record.ID), slog.String("event_id", envelope.EventID), slog.String("event_type", string(envelope.EventType))}
		attributes = append(attributes, outboxEventAttributes(envelope)...)
		attributes = append(attributes, slog.String("attempt_id", attemptID), slog.String("stage", "publish"), slog.String("reason", string(reason)))
		logging.WithTrace(dispatcher.logger, traceContext).Warn("outbox publish failed", attributes...)
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	dbCtx, cancelDB := context.WithTimeout(ctx, dispatcher.databaseTimeout)
	defer cancelDB()
	if err := mark(dbCtx, record.ID, dispatcher.owner); err != nil {
		attributes := []any{slog.Uint64("outbox_id", record.ID), slog.String("event_id", envelope.EventID), slog.String("event_type", string(envelope.EventType))}
		attributes = append(attributes, outboxEventAttributes(envelope)...)
		attributes = append(attributes, slog.String("attempt_id", attemptID), slog.String("stage", "publish_ack"), slog.String("reason", safeDispatcherReason(err)))
		logging.WithTrace(dispatcher.logger, traceContext).Error("outbox mark published failed", attributes...)
		if errors.Is(err, ErrLeaseLost) || ctx.Err() != nil {
			return err
		}
		// A successful publish followed by a failed mark is an accepted
		// at-least-once duplicate boundary. Releasing the lease, when still
		// current, prevents a transient database error from stranding the row.
		if releaseErr := dispatcher.releaseFailed(traceContext, record, FailureInternal); releaseErr != nil {
			return fmt.Errorf("mark published: %w; release failed: %v", err, releaseErr)
		}
		return fmt.Errorf("mark published: %w", err)
	}
	attributes := []any{slog.Uint64("outbox_id", record.ID), slog.String("event_id", envelope.EventID), slog.String("event_type", string(envelope.EventType))}
	attributes = append(attributes, outboxEventAttributes(envelope)...)
	attributes = append(attributes, slog.String("attempt_id", attemptID), slog.String("stage", "publish"), slog.String("result", "success"))
	logging.WithTrace(dispatcher.logger, traceContext).Info("outbox publish confirmed", attributes...)
	return nil
}

func outboxEventAttributes(envelope bus.Envelope) []any {
	attributes := []any{}
	if envelope.PostID > 0 {
		attributes = append(attributes, slog.Uint64("post_id", envelope.PostID))
	}
	if envelope.ContentRevision > 0 {
		attributes = append(attributes, slog.Uint64("content_revision", envelope.ContentRevision))
	}
	if envelope.DataEpoch != "" {
		attributes = append(attributes, slog.String("data_epoch", envelope.DataEpoch))
	}
	if envelope.CommandID != "" {
		attributes = append(attributes, slog.String("command_id", envelope.CommandID))
	}
	return attributes
}

func (dispatcher *Dispatcher) releaseFailed(ctx context.Context, record Record, failure FailureCode) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	dbCtx, cancel := context.WithTimeout(ctx, dispatcher.databaseTimeout)
	defer cancel()
	if err := dispatcher.store.ReleaseFailed(dbCtx, record.ID, dispatcher.owner, failure); err != nil {
		logging.WithTrace(dispatcher.logger, ctx).Error("outbox release failed",
			slog.Uint64("outbox_id", record.ID),
			slog.String("event_id", record.EventID),
			slog.String("event_type", string(record.EventType)),
			slog.String("reason", safeDispatcherReason(err)),
		)
		if errors.Is(err, ErrLeaseLost) {
			return nil
		}
		return fmt.Errorf("release outbox event after %s: %w", failure, err)
	}
	return nil
}

func publishFailureCode(err error) FailureCode {
	var publishErr *PublishError
	if errors.As(err, &publishErr) && validFailureCode(publishErr.Code) {
		return publishErr.Code
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return FailurePublishTimeout
	case errors.Is(err, context.Canceled):
		return FailurePublisherClosed
	default:
		return FailurePublishUnavailable
	}
}

func safeDispatcherReason(err error) string {
	if err == nil {
		return "internal_failure"
	}
	var publishErr *PublishError
	if errors.As(err, &publishErr) && validFailureCode(publishErr.Code) {
		return string(publishErr.Code)
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return string(FailurePublishTimeout)
	case errors.Is(err, context.Canceled):
		return string(FailurePublisherClosed)
	case errors.Is(err, ErrLeaseLost):
		return "lease_lost"
	default:
		return "internal_failure"
	}
}

func defaultDispatcherOwner() string {
	return "backend-" + strconv.Itoa(os.Getpid())
}
