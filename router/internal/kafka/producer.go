package kafka

import (
	"context"
	"errors"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type Producer struct {
	client         *kgo.Client
	produceTimeout time.Duration
	slot           chan struct{}
}

type Config struct {
	Brokers            []string
	ProduceTimeout     time.Duration
	MaxBufferedRecords int
	MaxBufferedBytes   int
}

func New(cfg Config) (*Producer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.AllowIdempotentProduceCancellation(),
		kgo.MaxBufferedRecords(cfg.MaxBufferedRecords),
		kgo.MaxBufferedBytes(cfg.MaxBufferedBytes),
		kgo.RecordDeliveryTimeout(cfg.ProduceTimeout),
		kgo.RetryTimeout(cfg.ProduceTimeout),
	)
	if err != nil {
		return nil, err
	}
	return &Producer{client: client, produceTimeout: cfg.ProduceTimeout, slot: make(chan struct{}, 1)}, nil
}

func (p *Producer) Produce(ctx context.Context, topic, key string, value []byte) error {
	produceCtx, cancel := context.WithTimeout(ctx, p.produceTimeout)
	defer cancel()
	if err := p.acquire(produceCtx); err != nil {
		return err
	}
	defer p.release()

	result := make(chan error, 1)
	p.client.Produce(produceCtx, &kgo.Record{Topic: topic, Key: []byte(key), Value: value}, func(_ *kgo.Record, err error) {
		result <- err
	})
	select {
	case err := <-result:
		return err
	case <-produceCtx.Done():
		// Bounded cancellation is more important than retaining the producer sequence
		// window. Kafka may already have accepted the record, so callers must treat
		// this outcome as uncertain and downstream consumers must tolerate duplicates.
		p.client.UnsafeAbortBufferedRecords()
		p.client.PurgeTopicsFromClient(topic)
		return produceCtx.Err()
	}
}

func (p *Producer) Ready(ctx context.Context, topic string) error {
	if err := p.acquire(ctx); err != nil {
		return err
	}
	defer p.release()
	if err := p.client.Ping(ctx); err != nil {
		return err
	}
	request := kmsg.NewPtrMetadataRequest()
	request.Topics = []kmsg.MetadataRequestTopic{{Topic: &topic}}
	response, err := p.client.Request(ctx, request)
	if err != nil {
		return err
	}
	metadata, ok := response.(*kmsg.MetadataResponse)
	if !ok || len(metadata.Topics) != 1 {
		return errors.New("topic metadata is unavailable")
	}
	topicMetadata := metadata.Topics[0]
	if topicMetadata.ErrorCode != 0 || len(topicMetadata.Partitions) == 0 {
		return errors.New("topic is unavailable")
	}
	for _, partition := range topicMetadata.Partitions {
		if partition.ErrorCode != 0 || partition.Leader < 0 {
			return errors.New("topic partition is unavailable")
		}
	}
	return nil
}

func (p *Producer) Close(ctx context.Context) error {
	if err := p.acquire(ctx); err != nil {
		p.client.UnsafeAbortBufferedRecords()
		p.client.Close()
		return err
	}
	defer p.release()
	err := p.client.Flush(ctx)
	if err != nil {
		p.client.UnsafeAbortBufferedRecords()
	}
	p.client.Close()
	return err
}

func (p *Producer) acquire(ctx context.Context) error {
	select {
	case p.slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Producer) release() { <-p.slot }
