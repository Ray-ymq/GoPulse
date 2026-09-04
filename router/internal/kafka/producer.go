package kafka

import (
	"context"
	"errors"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type client interface {
	TryProduce(context.Context, *kgo.Record, func(*kgo.Record, error))
	Ping(context.Context) error
	Request(context.Context, kmsg.Request) (kmsg.Response, error)
	Flush(context.Context) error
	UnsafeAbortBufferedRecords()
	Close()
}

type Producer struct {
	client         client
	produceTimeout time.Duration
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
	return &Producer{client: client, produceTimeout: cfg.ProduceTimeout}, nil
}

func (p *Producer) Produce(ctx context.Context, topic, key string, value []byte) error {
	// The record has its own bounded delivery context. Request cancellation stops
	// the caller's wait but does not cancel other records sharing a Kafka batch.
	produceCtx, cancelProduce := context.WithTimeout(context.Background(), p.produceTimeout)
	result := make(chan error, 1)
	p.client.TryProduce(produceCtx, &kgo.Record{Topic: topic, Key: []byte(key), Value: value}, func(_ *kgo.Record, err error) {
		cancelProduce()
		result <- err
	})
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Producer) Ready(ctx context.Context, topic string) error {
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
	err := p.client.Flush(ctx)
	if err != nil {
		p.client.UnsafeAbortBufferedRecords()
	}
	p.client.Close()
	return err
}
