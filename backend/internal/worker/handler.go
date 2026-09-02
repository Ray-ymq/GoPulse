package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	AttemptHeader        = "x-gopulse-attempt"
	maximumAttemptHeader = 1000
)

type Processor interface {
	Process(context.Context, bus.Envelope) error
}

type ConfirmingPublisher interface {
	Publish(context.Context, string, string, amqp.Publishing) error
}

type HandlerOptions struct {
	MaxRetries     int
	PublishTimeout time.Duration
	Logger         func(string, ...any)
}

type Handler struct {
	processor      Processor
	publisher      ConfirmingPublisher
	maxRetries     int
	publishTimeout time.Duration
	logger         func(string, ...any)
}

func NewHandler(processor Processor, publisher ConfirmingPublisher, options HandlerOptions) (*Handler, error) {
	if processor == nil || publisher == nil {
		return nil, errors.New("worker handler requires processor and publisher")
	}
	if options.MaxRetries < 0 || options.MaxRetries > 20 {
		return nil, errors.New("worker max retries must be between 0 and 20")
	}
	if options.PublishTimeout <= 0 {
		return nil, errors.New("worker publish timeout must be positive")
	}
	logger := options.Logger
	if logger == nil {
		logger = log.Printf
	}
	return &Handler{
		processor: processor, publisher: publisher, maxRetries: options.MaxRetries,
		publishTimeout: options.PublishTimeout, logger: logger,
	}, nil
}

// Handle processes exactly one delivery. Secondary retry/dead publications
// must be confirmed before the original delivery is acknowledged.
func (handler *Handler) Handle(ctx context.Context, delivery amqp.Delivery) error {
	attempt, attemptErr := deliveryAttempt(delivery.Headers)
	envelope, decodeErr := DecodeDelivery(delivery)
	if attemptErr != nil {
		return handler.deadLetter(ctx, delivery, attempt, "invalid_attempt")
	}
	if attempt > handler.maxRetries {
		return handler.deadLetter(ctx, delivery, attempt, "attempt_exceeds_max")
	}
	if decodeErr != nil {
		return handler.deadLetter(ctx, delivery, attempt, decodeErr.Error())
	}
	if envelope.ActorID == envelope.RecipientID {
		if err := delivery.Ack(false); err != nil {
			return errors.New("ack self event")
		}
		return nil
	}

	if err := handler.processor.Process(ctx, envelope); err == nil {
		if err := delivery.Ack(false); err != nil {
			return errors.New("ack processed event")
		}
		return nil
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if nackErr := delivery.Nack(false, true); nackErr != nil {
			return errors.New("requeue canceled event")
		}
		return err
	}

	if attempt < handler.maxRetries {
		return handler.retry(ctx, delivery, attempt+1)
	}
	return handler.deadLetter(ctx, delivery, attempt, "retries_exhausted")
}

func (handler *Handler) retry(ctx context.Context, delivery amqp.Delivery, nextAttempt int) error {
	message := publishingFromDelivery(delivery)
	message.Headers[AttemptHeader] = int32(nextAttempt)
	publishContext, cancel := context.WithTimeout(ctx, handler.publishTimeout)
	defer cancel()
	if err := handler.publisher.Publish(publishContext, platform.BusinessRetryExchange, safeRoutingKey(delivery.RoutingKey), message); err != nil {
		handler.log(delivery, nextAttempt, "retry_publish_failed")
		if nackErr := delivery.Nack(false, true); nackErr != nil {
			return errors.New("requeue after retry publish failure")
		}
		return errors.New("publish retry message")
	}
	if err := delivery.Ack(false); err != nil {
		return errors.New("ack retried event")
	}
	handler.log(delivery, nextAttempt, "retry_scheduled")
	return nil
}

func (handler *Handler) deadLetter(ctx context.Context, delivery amqp.Delivery, attempt int, reason string) error {
	message := publishingFromDelivery(delivery)
	message.Headers[AttemptHeader] = int32(attempt)
	publishContext, cancel := context.WithTimeout(ctx, handler.publishTimeout)
	defer cancel()
	if err := handler.publisher.Publish(publishContext, platform.BusinessDeadExchange, safeRoutingKey(delivery.RoutingKey), message); err != nil {
		handler.log(delivery, attempt, "dead_publish_failed")
		if nackErr := delivery.Nack(false, true); nackErr != nil {
			return errors.New("requeue after dead publish failure")
		}
		return errors.New("publish dead message")
	}
	if err := delivery.Ack(false); err != nil {
		return errors.New("ack dead-lettered event")
	}
	handler.log(delivery, attempt, reason)
	return nil
}

func (handler *Handler) log(delivery amqp.Delivery, attempt int, reason string) {
	eventID, eventType := deliveryIdentity(delivery)
	handler.logger("business worker event_id=%s event_type=%s attempt=%d reason=%s", eventID, eventType, attempt, reason)
}

func publishingFromDelivery(delivery amqp.Delivery) amqp.Publishing {
	headers := make(amqp.Table, len(delivery.Headers)+1)
	for key, value := range delivery.Headers {
		headers[key] = value
	}
	return amqp.Publishing{
		Headers: headers, ContentType: delivery.ContentType, ContentEncoding: delivery.ContentEncoding,
		DeliveryMode: amqp.Persistent, Priority: delivery.Priority, CorrelationId: delivery.CorrelationId,
		ReplyTo: delivery.ReplyTo, Expiration: delivery.Expiration, MessageId: delivery.MessageId,
		Timestamp: timestampOrNow(delivery.Timestamp), Type: delivery.Type, UserId: delivery.UserId,
		AppId: delivery.AppId, Body: append([]byte(nil), delivery.Body...),
	}
}

func safeRoutingKey(routingKey string) string {
	switch routingKey {
	case bus.CommentCreatedRoutingKey, bus.PostLikedRoutingKey:
		return routingKey
	default:
		return platform.BusinessInvalidRoutingKey
	}
}

func (handler *Handler) String() string {
	return fmt.Sprintf("business worker handler(max_retries=%d)", handler.maxRetries)
}
