package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	AttemptHeader        = "x-gopulse-attempt"
	maximumAttemptHeader = 1000
)

// Processor implementations must stop promptly when the supplied context is
// canceled so Runtime can reclaim the in-flight handler before shutdown returns.
type Processor interface {
	Process(context.Context, bus.Envelope) error
}

type ConfirmingPublisher interface {
	Publish(context.Context, string, string, amqp.Publishing) error
}

type HandlerOptions struct {
	Profile        Profile
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
	profile        Profile
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
	profile := normalizeProfile(options.Profile)
	logger := options.Logger
	if logger == nil {
		logger = log.Printf
	}
	return &Handler{
		processor: processor, publisher: publisher, maxRetries: options.MaxRetries,
		publishTimeout: options.PublishTimeout, logger: logger, profile: profile,
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
	if !handler.profile.allows(delivery.RoutingKey) {
		return handler.deadLetter(ctx, delivery, attempt, "routing_key_not_allowed")
	}
	if handler.profile.IgnoreSelfEvents && envelope.ActorID == envelope.RecipientID {
		if err := delivery.Ack(false); err != nil {
			return errors.New("ack self event")
		}
		return nil
	}

	processErr := handler.processor.Process(ctx, envelope)
	if processErr == nil {
		if err := delivery.Ack(false); err != nil {
			return errors.New("ack processed event")
		}
		return nil
	}
	if errors.Is(processErr, context.Canceled) || errors.Is(processErr, context.DeadlineExceeded) {
		if nackErr := delivery.Nack(false, true); nackErr != nil {
			return errors.New("requeue canceled event")
		}
		return processErr
	}

	if IsPermanent(processErr) {
		return handler.deadLetter(ctx, delivery, attempt, processErr.Error())
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
	if err := handler.publisher.Publish(publishContext, handler.profile.Topology.RetryExchange, handler.safeRoutingKey(delivery.RoutingKey), message); err != nil {
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
	if err := handler.publisher.Publish(publishContext, handler.profile.Topology.DeadExchange, handler.safeRoutingKey(delivery.RoutingKey), message); err != nil {
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
	handler.logger("%s event_id=%s event_type=%s attempt=%d reason=%s", handler.profile.Name, eventID, eventType, attempt, reason)
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

func (handler *Handler) safeRoutingKey(routingKey string) string {
	if handler.profile.allows(routingKey) {
		return routingKey
	}
	return handler.profile.Topology.InvalidRoutingKey
}

func (handler *Handler) String() string {
	return fmt.Sprintf("%s handler(max_retries=%d)", handler.profile.Name, handler.maxRetries)
}
