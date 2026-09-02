package worker

import (
	"errors"
	"fmt"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	amqp "github.com/rabbitmq/amqp091-go"
)

type PermanentError struct {
	Reason string
}

func (err *PermanentError) Error() string { return err.Reason }

func permanent(reason string) error { return &PermanentError{Reason: reason} }

func IsPermanent(err error) bool {
	var target *PermanentError
	return errors.As(err, &target)
}

// DecodeDelivery validates both the strict JSON envelope and the AMQP
// properties that duplicate its identity and routing metadata.
func DecodeDelivery(delivery amqp.Delivery) (bus.Envelope, error) {
	envelope, err := bus.Decode(delivery.Body)
	if err != nil {
		return bus.Envelope{}, permanent("invalid_body")
	}
	routingKey, err := envelope.RoutingKey()
	if err != nil || delivery.RoutingKey != routingKey {
		return bus.Envelope{}, permanent("routing_key_mismatch")
	}
	metadata, err := envelope.Metadata()
	if err != nil {
		return bus.Envelope{}, permanent("invalid_envelope")
	}
	if delivery.MessageId != metadata.MessageID {
		return bus.Envelope{}, permanent("message_id_mismatch")
	}
	if delivery.ContentType != metadata.ContentType {
		return bus.Envelope{}, permanent("content_type_mismatch")
	}
	if delivery.Type != metadata.Type {
		return bus.Envelope{}, permanent("message_type_mismatch")
	}
	if delivery.DeliveryMode != amqp.Persistent {
		return bus.Envelope{}, permanent("delivery_mode_mismatch")
	}
	if delivery.Timestamp.IsZero() || delivery.Timestamp.UTC().Unix() != metadata.Timestamp.UTC().Unix() {
		return bus.Envelope{}, permanent("timestamp_mismatch")
	}
	return envelope, nil
}

func deliveryAttempt(headers amqp.Table) (int, error) {
	if headers == nil {
		return 0, nil
	}
	value, exists := headers[AttemptHeader]
	if !exists {
		return 0, nil
	}
	var attempt int64
	switch typed := value.(type) {
	case int8:
		attempt = int64(typed)
	case int16:
		attempt = int64(typed)
	case int32:
		attempt = int64(typed)
	case int64:
		attempt = typed
	case uint8:
		attempt = int64(typed)
	case uint16:
		attempt = int64(typed)
	case uint32:
		attempt = int64(typed)
	case string:
		return 0, fmt.Errorf("attempt header must be numeric")
	default:
		return 0, fmt.Errorf("attempt header has unsupported type")
	}
	if attempt < 0 || attempt > maximumAttemptHeader {
		return 0, fmt.Errorf("attempt header is out of range")
	}
	return int(attempt), nil
}

func deliveryIdentity(delivery amqp.Delivery) (string, string) {
	eventID := delivery.MessageId
	if eventID == "" {
		eventID = "unknown"
	}
	eventType := delivery.Type
	if eventType == "" {
		eventType = "unknown"
	}
	return eventID, eventType
}

func timestampOrNow(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}
