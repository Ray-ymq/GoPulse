package consumer

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/config"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type Kafka struct {
	Client        *kgo.Client
	Ownership     *Ownership
	Topic         string
	CommitTimeout time.Duration
	halted        atomic.Bool
}

func NewKafka(brokers []string, topic, group string, commitTimeout time.Duration, ownership *Ownership) (*Kafka, error) {
	if ownership == nil {
		ownership = NewOwnership()
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...), kgo.ClientID("gopulse-marshaller"), kgo.ConsumerGroup(group), kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()), kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
			ownership.Assign(convertPartitions(assigned))
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, revoked map[string][]int32) {
			ownership.Revoke(convertPartitions(revoked))
		}),
		kgo.OnPartitionsLost(func(_ context.Context, _ *kgo.Client, lost map[string][]int32) {
			ownership.Lose(convertPartitions(lost))
		}),
	)
	if err != nil {
		return nil, err
	}
	return &Kafka{Client: client, Ownership: ownership, Topic: topic, CommitTimeout: commitTimeout}, nil
}
func convertPartitions(input map[string][]int32) []Partition {
	var out []Partition
	for topic, parts := range input {
		for _, part := range parts {
			out = append(out, Partition{Topic: topic, Partition: part})
		}
	}
	return out
}
func (k *Kafka) Commit(ctx context.Context, record Record) error {
	timeout := k.CommitTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	commitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return k.Client.CommitRecords(commitCtx, &kgo.Record{Topic: record.Topic, Partition: record.Partition, Offset: record.Offset})
}
func (k *Kafka) Ready(ctx context.Context) error {
	if k.halted.Load() {
		return errors.New("Kafka partition processing halted")
	}
	if err := k.Client.Ping(ctx); err != nil {
		return errors.New("Kafka unavailable")
	}
	request := kmsg.NewPtrMetadataRequest()
	request.Topics = []kmsg.MetadataRequestTopic{{Topic: kmsg.StringPtr(k.Topic)}}
	response, err := k.Client.Request(ctx, request)
	if err != nil {
		return errors.New("Kafka metadata unavailable")
	}
	metadata, ok := response.(*kmsg.MetadataResponse)
	if !ok || len(metadata.Topics) != 1 || metadata.Topics[0].ErrorCode != 0 {
		return errors.New("Kafka topic unavailable")
	}
	return nil
}
func (k *Kafka) Run(ctx context.Context, processor *Processor, logf func(string, ...any)) error {
	for ctx.Err() == nil {
		fetches := k.Client.PollRecords(ctx, 1)
		if errs := fetches.Errors(); len(errs) > 0 {
			if ctx.Err() != nil {
				return nil
			}
			if logf != nil {
				logf("Kafka poll failed", "module", "consumer", "error_count", len(errs))
			}
			continue
		}
		var record *kgo.Record
		fetches.EachRecord(func(r *kgo.Record) { record = r })
		if record == nil {
			continue
		}
		if len(record.Value) > config.MaxRecordBytes { /* decoder classifies this normally */
		}
		partition := Partition{Topic: record.Topic, Partition: record.Partition}
		lease, ok := k.Ownership.Lease(partition)
		if !ok {
			continue
		}
		item := Record{Topic: record.Topic, Partition: record.Partition, Offset: record.Offset, Key: append([]byte(nil), record.Key...), Value: append([]byte(nil), record.Value...)}
		if err := processor.Handle(ctx, item, lease); err != nil {
			if errors.Is(err, ErrOwnershipLost) {
				continue
			}
			k.halted.Store(true)
			return fmt.Errorf("partition %d halted: %w", record.Partition, err)
		}
	}
	return nil
}
func (k *Kafka) Close(ctx context.Context) error {
	k.Ownership.CancelAll()
	done := make(chan struct{})
	go func() { k.Client.Close(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
