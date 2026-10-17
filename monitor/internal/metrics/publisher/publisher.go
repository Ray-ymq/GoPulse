package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	MetricsTopic = "gopulse-observability-metrics"
	LogsTopic    = "gopulse-observability-logs"
	EventsTopic  = "gopulse-observability-events"
)

var messageIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Publisher interface {
	Publish(context.Context, envelope.Envelope) error
}

// Message is the validated wire record handed to a delivery queue or Kafka.
// Keeping the body opaque here ensures retries resend the exact original bytes.
type Message struct {
	ID   string
	Type string
	Body []byte
}

type Sender interface {
	Send(context.Context, Message) error
}

type Transport interface {
	Publisher
	PublishRaw(context.Context, string, any) error
}

type Discard struct{}

func (Discard) Publish(context.Context, envelope.Envelope) error { return nil }
func (Discard) PublishRaw(context.Context, string, any) error    { return nil }
func (Discard) Send(context.Context, Message) error              { return nil }

type KafkaConfig struct {
	Brokers            []string
	ProduceTimeout     time.Duration
	MaxBufferedRecords int
	MaxBufferedBytes   int
}

type PermanentError struct{ Code string }

func (e *PermanentError) Error() string   { return "record permanently rejected: " + e.Code }
func (e *PermanentError) Permanent() bool { return true }

type Kafka struct {
	client  *kgo.Client
	closeMu sync.Mutex
	closed  bool
}

func NewKafka(cfg KafkaConfig) (*Kafka, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("Kafka brokers are required")
	}
	if cfg.ProduceTimeout <= 0 {
		cfg.ProduceTimeout = 3 * time.Second
	}
	if cfg.MaxBufferedRecords <= 0 {
		cfg.MaxBufferedRecords = 128
	}
	if cfg.MaxBufferedBytes <= 0 {
		cfg.MaxBufferedBytes = 8 << 20
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID("gopulse-monitor"),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.AllowIdempotentProduceCancellation(),
		kgo.RecordDeliveryTimeout(cfg.ProduceTimeout),
		kgo.RetryTimeout(cfg.ProduceTimeout),
		kgo.MaxBufferedRecords(cfg.MaxBufferedRecords),
		kgo.MaxBufferedBytes(cfg.MaxBufferedBytes),
	)
	if err != nil {
		return nil, err
	}
	return &Kafka{client: client}, nil
}

func (k *Kafka) Publish(ctx context.Context, message envelope.Envelope) error {
	if message.Type != "metrics" {
		return &PermanentError{Code: "unsupported_envelope"}
	}
	body, err := json.Marshal(message)
	if err != nil {
		return &PermanentError{Code: "serialization_failed"}
	}
	return k.Send(ctx, Message{ID: message.MessageID, Type: message.Type, Body: body})
}

func (k *Kafka) PublishRaw(ctx context.Context, messageID string, value any) error {
	message, err := MessageFromRaw(messageID, value)
	if err != nil {
		return err
	}
	return k.Send(ctx, message)
}

// MessageFromRaw validates the common envelope before it enters a durable
// queue. The body is returned unchanged so retries cannot alter timestamps,
// labels, values, or document IDs.
func MessageFromRaw(messageID string, value any) (Message, error) {
	body, err := rawJSON(value)
	if err != nil {
		return Message{}, &PermanentError{Code: "invalid_envelope"}
	}
	var header struct {
		SchemaVersion int             `json:"schema_version"`
		MessageID     string          `json:"message_id"`
		Type          string          `json:"type"`
		Source        string          `json:"source"`
		Timestamp     string          `json:"timestamp"`
		Payload       json.RawMessage `json:"payload"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&header); err != nil || header.SchemaVersion != envelope.SchemaVersion || header.MessageID != messageID || !messageIDPattern.MatchString(messageID) || header.Source == "" || header.Timestamp == "" || len(header.Payload) == 0 {
		return Message{}, &PermanentError{Code: "invalid_envelope"}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Message{}, &PermanentError{Code: "invalid_envelope"}
	}
	if header.Type != "logs" && header.Type != "events" {
		return Message{}, &PermanentError{Code: "unsupported_envelope"}
	}
	return Message{ID: messageID, Type: header.Type, Body: body}, nil
}

func (k *Kafka) Send(ctx context.Context, message Message) error {
	if !messageIDPattern.MatchString(message.ID) || message.Type == "" || len(message.Body) == 0 {
		return &PermanentError{Code: "invalid_envelope"}
	}
	return k.publish(ctx, message.ID, message.Type, message.Body)
}

func (k *Kafka) publish(ctx context.Context, messageID, messageType string, body []byte) error {
	if !messageIDPattern.MatchString(messageID) {
		return &PermanentError{Code: "invalid_message_id"}
	}
	topic, ok := topicFor(messageType)
	if !ok {
		return &PermanentError{Code: "unsupported_envelope"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	k.closeMu.Lock()
	if k.closed {
		k.closeMu.Unlock()
		return errors.New("Kafka publisher is closed")
	}
	done := make(chan error, 1)
	k.client.Produce(ctx, &kgo.Record{Topic: topic, Key: []byte(messageID), Value: body}, func(_ *kgo.Record, err error) {
		done <- err
	})
	k.closeMu.Unlock()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func topicFor(messageType string) (string, bool) {
	switch messageType {
	case "metrics":
		return MetricsTopic, true
	case "logs":
		return LogsTopic, true
	case "events":
		return EventsTopic, true
	default:
		return "", false
	}
}

func rawJSON(value any) ([]byte, error) {
	switch raw := value.(type) {
	case json.RawMessage:
		return append([]byte(nil), raw...), nil
	case []byte:
		return append([]byte(nil), raw...), nil
	default:
		return json.Marshal(value)
	}
}

func (k *Kafka) Close(ctx context.Context) error {
	k.closeMu.Lock()
	if k.closed {
		k.closeMu.Unlock()
		return nil
	}
	k.closed = true
	k.closeMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	flushErr := k.client.Flush(ctx)
	done := make(chan struct{})
	go func() {
		k.client.Close()
		close(done)
	}()
	select {
	case <-done:
		return flushErr
	case <-ctx.Done():
		if flushErr != nil {
			return fmt.Errorf("flush: %w; close: %w", flushErr, ctx.Err())
		}
		return ctx.Err()
	}
}
