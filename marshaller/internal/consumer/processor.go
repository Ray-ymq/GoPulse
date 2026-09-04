package consumer

import (
	"context"
	"errors"
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
	message, err := p.Decoder.Decode(record.Key, record.Value)
	if err != nil {
		code := envelope.Code(err)
		if code == "" {
			return err
		}
		p.Logger.Permanent(record, code)
		return p.commit(ctx, record, lease)
	}
	body, err := p.Transformer.Transform(message)
	if err != nil {
		p.Logger.Permanent(record, "transform_failed")
		return p.commit(ctx, record, lease)
	}
	delay := p.RetryMin
	for {
		if !lease.Valid() {
			return ErrOwnershipLost
		}
		writeCtx, cancel := mergeContext(ctx, lease.Context())
		err = p.Writer.Write(writeCtx, body)
		cancel()
		if err == nil {
			if !lease.Valid() {
				return ErrOwnershipLost
			}
			if err = p.commit(ctx, record, lease); err != nil {
				return err
			}
			p.Logger.Accepted(record)
			return nil
		}
		if ctx.Err() != nil || lease.Context().Err() != nil {
			return ErrOwnershipLost
		}
		p.Logger.Transient(record)
		if err = p.Sleep(lease.Context(), delay); err != nil {
			return ErrOwnershipLost
		}
		delay *= 2
		if delay > p.RetryMax {
			delay = p.RetryMax
		}
	}
}
func (p *Processor) commit(ctx context.Context, record Record, lease Lease) error {
	if !lease.Valid() {
		return ErrOwnershipLost
	}
	commitCtx, cancel := mergeContext(ctx, lease.Context())
	defer cancel()
	if err := p.Committer.Commit(commitCtx, record); err != nil {
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
