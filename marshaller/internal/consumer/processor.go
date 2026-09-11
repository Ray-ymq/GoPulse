package consumer

import (
	"context"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
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
	Transformer        Transformer
	Writer             Writer
	Targets            map[string]Target
	Committer          Committer
	RetryMin, RetryMax time.Duration
	Logger             Logger
	Sleep              func(context.Context, time.Duration) error
}

func (p *Processor) Handle(ctx context.Context, record Record, lease Lease) error {
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
	metrics := componentmetrics.Active()
	metrics.Add("records_in_flight", 1)
	defer metrics.Add("records_in_flight", -1)
	started := time.Now()
	message, err := p.Decoder.Decode(record.Key, record.Value)
	kind, source := componentmetrics.MessageIdentity(message.Type, message.Source)
	metrics.Observe("records_total", time.Since(started), kind, source, "consume", "consumed")
	validationResult := "validated"
	if err != nil {
		validationResult = "rejected"
	}
	metrics.Observe("records_total", time.Since(started), kind, source, "validate", validationResult)
	if err != nil {
		code := envelope.Code(err)
		if code == "" {
			return err
		}
		p.Logger.Permanent(record, code)
		return p.commit(ctx, record, lease, kind, source)
	}
	target := Target{Transformer: p.Transformer, Writer: p.Writer}
	if p.Targets != nil {
		var ok bool
		target, ok = p.Targets[message.Type+"/"+message.Source]
		if !ok || target.Transformer == nil || target.Writer == nil {
			p.Logger.Permanent(record, "unsupported_envelope")
			return p.commit(ctx, record, lease, kind, source)
		}
	}
	body, err := target.Transformer.Transform(message)
	if err != nil {
		code := envelope.Code(err)
		if code == "" {
			code = "transform_failed"
		}
		p.Logger.Permanent(record, code)
		return p.commit(ctx, record, lease, kind, source)
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
		} else {
			metrics.Set("last_storage_success_timestamp_seconds", float64(time.Now().Unix()), storage)
		}
		metrics.Observe("records_total", time.Since(started), kind, source, "store", result)
		cancel()
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
		metrics.Add("retrying", 1)
		err = p.Sleep(lease.Context(), delay)
		metrics.Add("retrying", -1)
		if err != nil {
			return ErrOwnershipLost
		}
		delay *= 2
		if delay > p.RetryMax {
			delay = p.RetryMax
		}
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
			componentmetrics.Active().Set("last_commit_success_timestamp_seconds", float64(time.Now().Unix()))
		}
		componentmetrics.Active().Observe("records_total", time.Since(started), kind, source, "commit", outcome)
	}()
	if !lease.Valid() {
		return ErrOwnershipLost
	}
	commitCtx, cancel := mergeContext(ctx, lease.Context())
	defer cancel()
	err := p.Committer.Commit(commitCtx, record)
	componentmetrics.Dependency("kafka", err)
	if err != nil {
		if !lease.Valid() || lease.Context().Err() != nil {
			return ErrOwnershipLost
		}
		return ErrCommitFailed
	}
	if !lease.Valid() {
		return ErrOwnershipLost
	}
	return nil
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
