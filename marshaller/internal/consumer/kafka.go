package consumer

import (
	"context"
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type Kafka struct {
	Client        *kgo.Client
	Ownership     *Ownership
	Topic         string
	CommitTimeout time.Duration
	MinPartitions int
	MaxInFlight   int
	halted        atomic.Bool
}

func NewKafka(brokers []string, topic, group string, commitTimeout time.Duration, ownership *Ownership) (*Kafka, error) {
	return NewKafkaWithOptions(brokers, topic, group, commitTimeout, 1, 1, ownership)
}

func NewKafkaWithOptions(brokers []string, topic, group string, commitTimeout time.Duration, minPartitions, maxInFlight int, ownership *Ownership) (*Kafka, error) {
	if ownership == nil {
		ownership = NewOwnership()
	}
	if minPartitions < 1 {
		minPartitions = 1
	}
	if maxInFlight < 1 {
		maxInFlight = 1
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
	return &Kafka{Client: client, Ownership: ownership, Topic: topic, CommitTimeout: commitTimeout, MinPartitions: minPartitions, MaxInFlight: maxInFlight}, nil
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
func (k *Kafka) Ready(ctx context.Context) (result error) {
	defer func() { componentmetrics.Dependency("kafka", result) }()
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
	minimum := k.MinPartitions
	if minimum < 1 {
		minimum = 1
	}
	if !ok || len(metadata.Topics) != 1 || metadata.Topics[0].ErrorCode != 0 || len(metadata.Topics[0].Partitions) < minimum {
		return errors.New("Kafka topic unavailable")
	}
	return nil
}
func (k *Kafka) Run(ctx context.Context, processor *Processor, logf func(string, ...any)) error {
	if processor == nil {
		return errors.New("marshaller processor is required")
	}
	maxInFlight := k.MaxInFlight
	if maxInFlight < 1 {
		maxInFlight = 1
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	semaphore := make(chan struct{}, maxInFlight)
	var workers sync.WaitGroup
	var locksMu sync.Mutex
	partitionLocks := make(map[Partition]*sync.Mutex)
	var lagMu sync.Mutex
	partitionPending := make(map[Partition]int)
	var firstErr error
	var firstErrMu sync.Mutex
	partitionLock := func(partition Partition) *sync.Mutex {
		locksMu.Lock()
		defer locksMu.Unlock()
		lock := partitionLocks[partition]
		if lock == nil {
			lock = &sync.Mutex{}
			partitionLocks[partition] = lock
		}
		return lock
	}
	setLag := func(partition Partition, delta int) {
		lagMu.Lock()
		partitionPending[partition] += delta
		pending := partitionPending[partition]
		if pending <= 0 {
			delete(partitionPending, partition)
			pending = 0
		}
		lagMu.Unlock()
		if metrics := componentmetrics.Active(); metrics != nil && partition.Partition >= 0 && partition.Partition < 16 {
			metrics.Set("partition_lag", float64(pending), fmt.Sprintf("%d", partition.Partition))
		}
	}
	fail := func(err error) {
		if err == nil {
			return
		}
		firstErrMu.Lock()
		if firstErr == nil {
			firstErr = err
			k.halted.Store(true)
			cancel()
		}
		firstErrMu.Unlock()
	}
	for runCtx.Err() == nil {
		fetches := k.Client.PollRecords(runCtx, maxInFlight)
		if errs := fetches.Errors(); len(errs) > 0 {
			componentmetrics.Dependency("kafka", errs[0].Err)
			if ctx.Err() != nil {
				break
			}
			if logf != nil {
				logf("Kafka poll failed", "module", "consumer", "error_count", len(errs))
			}
			continue
		}
		if fetches.NumRecords() > 0 {
			componentmetrics.Dependency("kafka", nil)
		}
		fetches.EachRecord(func(record *kgo.Record) {
			if runCtx.Err() != nil {
				return
			}
			partition := Partition{Topic: record.Topic, Partition: record.Partition}
			partitionNumber := record.Partition
			lease, ok := k.Ownership.Lease(partition)
			if !ok {
				return
			}
			setLag(partition, 1)
			select {
			case semaphore <- struct{}{}:
			case <-runCtx.Done():
				setLag(partition, -1)
				return
			}
			item := Record{Topic: record.Topic, Partition: record.Partition, Offset: record.Offset, Key: append([]byte(nil), record.Key...), Value: append([]byte(nil), record.Value...)}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-semaphore }()
				defer setLag(partition, -1)
				lock := partitionLock(partition)
				lock.Lock()
				defer lock.Unlock()
				if err := processor.Handle(runCtx, item, lease); err != nil && !errors.Is(err, ErrOwnershipLost) {
					fail(fmt.Errorf("partition %d halted: %w", partitionNumber, err))
				}
			}()
		})
	}
	workers.Wait()
	firstErrMu.Lock()
	err := firstErr
	firstErrMu.Unlock()
	if err != nil {
		return err
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
