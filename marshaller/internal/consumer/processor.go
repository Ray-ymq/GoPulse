package consumer

import (
	"context"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
)

var (
	ErrOwnershipLost = errors.New("partition ownership lost")
	ErrCommitFailed  = errors.New("offset commit failed")
)

type Record struct {
	Topic      string
	Partition  int32
	Offset     int64
	Key, Value []byte
}
type Decoder interface {
	Decode([]byte, []byte) (envelope.Envelope, error)
}
type Transformer interface {
	Transform(envelope.Envelope) ([]byte, error)
}
type Writer interface {
	Write(context.Context, []byte) error
}

type Target struct {
	Transformer Transformer
	Writer      Writer
}
type Committer interface {
	Commit(context.Context, Record) error
}
type Logger interface {
	Permanent(Record, string)
	Transient(Record)
	Accepted(Record)
}
type nopLogger struct{}

func (nopLogger) Permanent(Record, string) {}
func (nopLogger) Transient(Record)         {}
func (nopLogger) Accepted(Record)          {}

type Processor struct {
	Decoder            Decoder
	ExpectedType       string
	Transformer        Transformer
	Writer             Writer
	Targets            map[string]Target
	Committer          Committer
	RetryMin, RetryMax time.Duration
	MaxRetrying        int
	Logger             Logger
	Sleep              func(context.Context, time.Duration) error
	prepareOnce        sync.Once
	retryOnce          sync.Once
	retrySlots         chan struct{}
}

func (p *Processor) Handle(ctx context.Context, record Record, lease Lease) error {
	p.prepareOnce.Do(func() {
		if p.Logger == nil {
			p.Logger = nopLogger{}
		}
		if p.Sleep == nil {
			p.Sleep = sleep
		}
		if p.RetryMin <= 0 {
			p.RetryMin = 250 * time.Millisecond
		}
		if p.RetryMax < p.RetryMin {
			p.RetryMax = p.RetryMin
		}
	})
	metrics := componentmetrics.Active()
	if metrics != nil {
		metrics.Add("records_in_flight", 1)
		defer metrics.Add("records_in_flight", -1)
	}
	started := time.Now()
	message, err := p.Decoder.Decode(record.Key, record.Value)
	kind, source := componentmetrics.MessageIdentity(message.Type, message.Source)
	if metrics != nil {
		metrics.Observe("records_total", time.Since(started), kind, source, "consume", "consumed")
	}
	validationResult := "validated"
	if err != nil {
		validationResult = "rejected"
	}
	if metrics != nil {
		metrics.Observe("records_total", time.Since(started), kind, source, "validate", validationResult)
	}
	if err != nil {
		code := envelope.Code(err)
		if code == "" {
			code = "invalid_envelope"
		}
		return p.reject(ctx, record, lease, kind, source, code)
	}
	if p.ExpectedType != "" && message.Type != p.ExpectedType {
		return p.reject(ctx, record, lease, kind, source, "unsupported_envelope")
	}
	target := Target{Transformer: p.Transformer, Writer: p.Writer}
	if p.Targets != nil {
		var ok bool
		target, ok = p.Targets[message.Type+"/"+message.Source]
		if !ok || target.Transformer == nil || target.Writer == nil {
			return p.reject(ctx, record, lease, kind, source, "unsupported_envelope")
		}
	}
	body, err := target.Transformer.Transform(message)
	if err != nil {
		code := envelope.Code(err)
		if code == "" {
			code = "transform_failed"
		}
		return p.reject(ctx, record, lease, kind, source, code)
	}
	delay := p.RetryMin
	for {
		if !lease.Valid() {
			return ErrOwnershipLost
		}
		writeCtx, cancel := mergeContext(ctx, lease.Context())
		started = time.Now()
		err = target.Writer.Write(writeCtx, body)
		storage := "elasticsearch"
		if message.Type == "metrics" {
			storage = "victoriametrics"
		}
		componentmetrics.Dependency(storage, err)
		result := "stored"
		if err != nil {
			result = "retried"
			if metrics != nil {
				metrics.Set("target_blocked", 1, storage)
			}
		} else {
			if metrics != nil {
				metrics.Set("target_blocked", 0, storage)
				metrics.Set("last_storage_success_timestamp_seconds", float64(time.Now().Unix()), storage)
			}
		}
		if metrics != nil {
			metrics.Observe("records_total", time.Since(started), kind, source, "store", result)
		}
		cancel()
		if err != nil {
			if code := envelope.Code(err); code != "" {
				return p.reject(ctx, record, lease, kind, source, code)
			}
		}
		if err == nil {
			if !lease.Valid() {
				return ErrOwnershipLost
			}
			if err = p.commit(ctx, record, lease, kind, source); err != nil {
				return err
			}
			p.Logger.Accepted(record)
			return nil
		}
		if ctx.Err() != nil || lease.Context().Err() != nil {
			return ErrOwnershipLost
		}
		p.Logger.Transient(record)
		retryCtx, retryCancel := mergeContext(ctx, lease.Context())
		releaseRetry, acquireErr := p.acquireRetry(retryCtx, lease)
		if acquireErr != nil {
			retryCancel()
			return ErrOwnershipLost
		}
		if metrics != nil {
			metrics.Add("retrying", 1)
		}
		err = p.Sleep(retryCtx, delay)
		releaseRetry()
		retryCancel()
		if metrics != nil {
			metrics.Add("retrying", -1)
		}
		if err != nil {
			return ErrOwnershipLost
		}
		delay *= 2
		if delay > p.RetryMax {
			delay = p.RetryMax
		}
	}
}

func (p *Processor) reject(ctx context.Context, record Record, lease Lease, kind, source, code string) error {
	if metrics := componentmetrics.Active(); metrics != nil {
		metrics.Add("records_discarded_total", 1, discardReason(code))
	}
	p.Logger.Permanent(record, code)
	return p.commit(ctx, record, lease, kind, source)
}

func discardReason(code string) string {
	switch code {
	case "invalid_json", "invalid_utf8", "invalid_envelope", "invalid_payload", "invalid_payload_identity", "invalid_producer", "invalid_sample_set", "invalid_timestamp", "invalid_value", "message_id_mismatch", "timestamp_too_far_future", "invalid_component_status", "unsupported_envelope", "record_too_large", "invalid_metric_batch", "invalid_log_payload", "invalid_event_payload", "source_mismatch", "timestamp_mismatch", "transform_failed", "expired_log_retention", "expired_event_retention":
		return code
	default:
		return "unknown"
	}
}

func (p *Processor) commit(ctx context.Context, record Record, lease Lease, identity ...string) (result error) {
	started := time.Now()
	kind, source := "unknown", "unknown"
	if len(identity) == 2 {
		kind, source = identity[0], identity[1]
	}
	defer func() {
		outcome := "committed"
		if result != nil {
			outcome = "failure"
		} else {
			if metrics := componentmetrics.Active(); metrics != nil {
				metrics.Set("last_commit_success_timestamp_seconds", float64(time.Now().Unix()))
			}
		}
		if metrics := componentmetrics.Active(); metrics != nil {
			metrics.Observe("records_total", time.Since(started), kind, source, "commit", outcome)
		}
	}()
	// Never advance to another record until this offset is acknowledged. Retry
	// only while this exact lease is valid; exhaustion terminates the process.
	delay := p.RetryMin
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil || !lease.Valid() {
			return ErrOwnershipLost
		}
		commitCtx, cancel := mergeContext(ctx, lease.Context())
		err := p.Committer.Commit(commitCtx, record)
		cancel()
		componentmetrics.Dependency("kafka", err)
		if ctx.Err() != nil || !lease.Valid() {
			return ErrOwnershipLost
		}
		if err == nil {
			return nil
		}
		if attempt == 2 {
			return ErrCommitFailed
		}
		retryCtx, retryCancel := mergeContext(ctx, lease.Context())
		releaseRetry, acquireErr := p.acquireRetry(retryCtx, lease)
		if acquireErr != nil {
			retryCancel()
			return ErrOwnershipLost
		}
		if metrics := componentmetrics.Active(); metrics != nil {
			metrics.Add("retrying", 1)
		}
		err = p.Sleep(retryCtx, delay)
		releaseRetry()
		if metrics := componentmetrics.Active(); metrics != nil {
			metrics.Add("retrying", -1)
		}
		retryCancel()
		if err != nil {
			return ErrOwnershipLost
		}
		delay *= 2
		if delay > p.RetryMax {
			delay = p.RetryMax
		}
	}
	return ErrCommitFailed
}

func (p *Processor) acquireRetry(ctx context.Context, lease Lease) (func(), error) {
	limit := p.MaxRetrying
	if limit < 1 {
		limit = 1
	}
	p.retryOnce.Do(func() { p.retrySlots = make(chan struct{}, limit) })
	select {
	case p.retrySlots <- struct{}{}:
		return func() { <-p.retrySlots }, nil
	case <-ctx.Done():
		return func() {}, ctx.Err()
	case <-lease.Context().Done():
		return func() {}, lease.Context().Err()
	}
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
func mergeContext(a, b context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(a)
	go func() {
		select {
		case <-b.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
