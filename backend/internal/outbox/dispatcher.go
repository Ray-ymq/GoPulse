package outbox

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
)

const (
	DefaultDispatcherPollInterval = time.Second
	MinimumDispatcherPollInterval = 10 * time.Millisecond
	MaximumDispatcherPollInterval = time.Minute
	DefaultDispatcherClaimBatch   = 10
	DefaultDispatcherLease        = 30 * time.Second
	DefaultDispatcherPublish      = 5 * time.Second
	MinimumDispatcherPublish      = 10 * time.Millisecond
	MaximumDispatcherPublish      = 30 * time.Second
)

// Store is the lease-aware persistence boundary used by Dispatcher. Keeping
// this interface small makes the delivery loop independently testable while
// ensuring all state transitions remain in the Outbox Repository.
type Store interface {
	Claim(context.Context, string, int, time.Duration) ([]Record, error)
	MarkPublished(context.Context, uint64, string) error
	ReleaseFailed(context.Context, uint64, string, FailureCode) error
}

// Publisher publishes one validated business event and returns only bounded
// failure categories through PublishError. It must not mark Outbox state.
type Publisher interface {
	Publish(context.Context, bus.Envelope) error
}

type DispatcherOptions struct {
	Owner          string
	PollInterval   time.Duration
	ClaimBatch     int
	LeaseDuration  time.Duration
	PublishTimeout time.Duration
}

type Dispatcher struct {
	store          Store
	publisher      Publisher
	owner          string
	pollInterval   time.Duration
	claimBatch     int
	leaseDuration  time.Duration
	publishTimeout time.Duration
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
	if publishTimeout >= leaseDuration {
		return nil, fmt.Errorf("create outbox dispatcher: publish timeout must be shorter than lease duration")
	}
	return &Dispatcher{
		store:          store,
		publisher:      publisher,
		owner:          owner,
		pollInterval:   pollInterval,
		claimBatch:     claimBatch,
		leaseDuration:  leaseDuration,
		publishTimeout: publishTimeout,
	}, nil
}

// Run continuously claims and publishes bounded batches. Claim failures and
// individual delivery failures are retried on the next poll; a cancellation
// never claims another row and lets an active lease expire if its outcome is
// uncertain.
func (dispatcher *Dispatcher) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("run outbox dispatcher: context is required")
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			if ctx.Err() != nil {
				return nil
			}
			if err := dispatcher.DispatchOnce(ctx); err != nil && ctx.Err() == nil {
				log.Printf("outbox dispatcher cycle failed: %s", safeDispatcherError(err))
			}
			timer.Reset(dispatcher.pollInterval)
		}
	}
}

// DispatchOnce performs one bounded claim/publish cycle. It is exported for
// deterministic integration and lifecycle tests; production uses Run.
func (dispatcher *Dispatcher) DispatchOnce(ctx context.Context) error {
	if ctx == nil {
		return errors.New("dispatch outbox events: context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	records, err := dispatcher.store.Claim(ctx, dispatcher.owner, dispatcher.claimBatch, dispatcher.leaseDuration)
	if err != nil {
		return fmt.Errorf("claim outbox events: %w", err)
	}

	var firstErr error
	for _, record := range records {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := dispatcher.dispatchRecord(ctx, record); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (dispatcher *Dispatcher) dispatchRecord(ctx context.Context, record Record) error {
	envelope, err := record.Envelope()
	if err != nil {
		releaseErr := dispatcher.releaseFailed(ctx, record, FailureInternal)
		if releaseErr != nil {
			return releaseErr
		}
		return fmt.Errorf("decode outbox event: %w", err)
	}

	publishContext, cancel := context.WithTimeout(ctx, dispatcher.publishTimeout)
	publishErr := dispatcher.publisher.Publish(publishContext, envelope)
	cancel()
	if publishErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A bounded publish failure is handled as part of the state machine:
		// release it for a later attempt and keep processing the claimed batch.
		return dispatcher.releaseFailed(ctx, record, publishFailureCode(publishErr))
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := dispatcher.store.MarkPublished(ctx, record.ID, dispatcher.owner); err != nil {
		if errors.Is(err, ErrLeaseLost) || ctx.Err() != nil {
			return err
		}
		// A successful publish followed by a failed mark is an accepted
		// at-least-once duplicate boundary. Releasing the lease, when still
		// current, prevents a transient database error from stranding the row.
		if releaseErr := dispatcher.releaseFailed(ctx, record, FailureInternal); releaseErr != nil {
			return fmt.Errorf("mark published: %w; release failed: %v", err, releaseErr)
		}
		return fmt.Errorf("mark published: %w", err)
	}
	return nil
}

func (dispatcher *Dispatcher) releaseFailed(ctx context.Context, record Record, failure FailureCode) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := dispatcher.store.ReleaseFailed(ctx, record.ID, dispatcher.owner, failure); err != nil {
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

func safeDispatcherError(err error) string {
	if err == nil {
		return "unknown failure"
	}
	var publishErr *PublishError
	if errors.As(err, &publishErr) {
		return publishErr.Error()
	}
	return "internal delivery failure"
}

func defaultDispatcherOwner() string {
	return "backend-" + strconv.Itoa(os.Getpid())
}
