package worker

import (
	"context"
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"log/slog"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/tracing"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
	Logger         *slog.Logger
}

type Handler struct {
	processor      Processor
	publisher      ConfirmingPublisher
	maxRetries     int
	publishTimeout time.Duration
	logger         *slog.Logger
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
		logger = logging.Module(logging.Discard(profile.Service), profile.Module)
	}
	return &Handler{
		processor: processor, publisher: publisher, maxRetries: options.MaxRetries,
		publishTimeout: options.PublishTimeout, logger: logger, profile: profile,
	}, nil
}

// Handle processes exactly one delivery. Secondary retry/dead publications
// must be confirmed before the original delivery is acknowledged.
func (handler *Handler) Handle(ctx context.Context, delivery amqp.Delivery) error {
	if ctx == nil {
		ctx = context.Background()
	}
	started := time.Now()
	metrics := componentmetrics.Active()
	metrics.Add("messages_in_flight", 1)
	outcome := "failure"
	identity := handler.metricIdentity(delivery)
	defer func() {
		metrics.Add("messages_in_flight", -1)
		metrics.Observe("messages_total", time.Since(started), identity, outcome)
		freshnessResult := "failure"
		if outcome == "success" {
			freshnessResult = "success"
		}
		metrics.Observe("freshness_events_total", time.Since(started), "consume", freshnessResult)
		if outcome == "success" {
			metrics.Set("last_success_timestamp_seconds", float64(time.Now().Unix()))
		}
	}()
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
	invalidTraceContext := envelope.HasInvalidTraceContext()
	if invalidTraceContext {
		tracing.RecordInvalidContext()
	}
	wire := deliveryTraceContext(delivery, envelope)
	traceContext, invalidWireContext := tracing.ContextFromWireWithStatus(ctx, wire)
	invalidTraceContext = invalidTraceContext || invalidWireContext
	if invalidTraceContext {
		metrics.Add("trace_context_invalid_total", 1)
	}
	traceContext, span := tracing.Start(traceContext, "worker.consume",
		attribute.String("gopulse.event_id", envelope.EventID),
		attribute.String("gopulse.event_type", string(envelope.EventType)),
		attribute.Int64("gopulse.post_id", int64(envelope.PostID)),
		attribute.Int64("gopulse.content_revision", int64(envelope.ContentRevision)),
		attribute.Int64("gopulse.outbox_id", int64(deliveryOutboxID(delivery.Headers))),
		attribute.Int("gopulse.attempt", attempt),
	)
	traceContext = tracing.WithOutboxID(traceContext, deliveryOutboxID(delivery.Headers))
	attemptID := deliveryAttemptID(delivery.Headers)
	if attemptID == "" {
		attemptID = tracing.NewAttemptID()
	}
	traceContext = tracing.WithAttemptID(traceContext, attemptID)
	span.SetAttributes(attribute.String("gopulse.attempt_id", attemptID))
	defer span.End()
	if !handler.profile.allows(delivery.RoutingKey) {
		span.SetStatus(codes.Error, "routing_key_not_allowed")
		return handler.deadLetter(traceContext, delivery, attempt, "routing_key_not_allowed")
	}
	if handler.profile.IgnoreSelfEvents && envelope.ActorID == envelope.RecipientID {
		if err := handler.ack(delivery); err != nil {
			span.SetStatus(codes.Error, "ack_failed")
			handler.logFailure(traceContext, "message acknowledgement failed", delivery, attempt, "ack_failed")
			return errors.New("ack self event")
		}
		outcome = "success"
		handler.logEvent(traceContext, "event ignored", delivery, envelope, attempt, "self_event")
		return nil
	}

	processErr := handler.processor.Process(traceContext, envelope)
	if processErr == nil {
		if err := handler.ack(delivery); err != nil {
			span.SetStatus(codes.Error, "ack_failed")
			handler.logFailure(traceContext, "message acknowledgement failed", delivery, attempt, "ack_failed")
			return errors.New("ack processed event")
		}
		outcome = "success"
		handler.logEvent(traceContext, "event processed", delivery, envelope, attempt, "processed")
		return nil
	}
	if errors.Is(processErr, context.Canceled) || errors.Is(processErr, context.DeadlineExceeded) {
		span.SetStatus(codes.Error, "processing_canceled")
		if nackErr := delivery.Nack(false, true); nackErr != nil {
			handler.logFailure(traceContext, "message requeue failed", delivery, attempt, "nack_failed")
			return errors.New("requeue canceled event")
		}
		return processErr
	}

	if IsPermanent(processErr) {
		span.SetStatus(codes.Error, permanentReason(processErr))
		return handler.deadLetter(traceContext, delivery, attempt, permanentReason(processErr))
	}

	if attempt < handler.maxRetries {
		err := handler.retry(traceContext, delivery, attempt+1)
		if err == nil {
			outcome = "retry"
		}
		return err
	}
	span.SetStatus(codes.Error, "retries_exhausted")
	return handler.deadLetter(traceContext, delivery, attempt, "retries_exhausted")
}

func (handler *Handler) retry(ctx context.Context, delivery amqp.Delivery, nextAttempt int) error {
	componentmetrics.Active().Add("retrying", 1)
	defer componentmetrics.Active().Add("retrying", -1)
	message := publishingFromDelivery(delivery)
	message.Headers[AttemptHeader] = int32(nextAttempt)
	// A retry is a new delivery attempt. Preserve the W3C parent context for
	// the business chain, but never reuse the attempt identity from the
	// failed delivery.
	message.Headers[tracing.AttemptIDHeader] = tracing.NewAttemptID()
	publishContext, cancel := context.WithTimeout(ctx, handler.publishTimeout)
	defer cancel()
	if err := handler.publish(publishContext, handler.profile.Topology.RetryExchange, delivery, message); err != nil {
		handler.logFailure(ctx, "retry publish failed", delivery, nextAttempt, "publish_unavailable")
		if nackErr := delivery.Nack(false, true); nackErr != nil {
			handler.logFailure(ctx, "message requeue failed", delivery, nextAttempt, "nack_failed")
			return errors.New("requeue after retry publish failure")
		}
		return errors.New("publish retry message")
	}
	if err := handler.ack(delivery); err != nil {
		handler.logFailure(ctx, "message acknowledgement failed", delivery, nextAttempt, "ack_failed")
		return errors.New("ack retried event")
	}
	handler.logEvent(ctx, "event retry scheduled", delivery, bus.Envelope{}, nextAttempt, "retry_scheduled")
	return nil
}

func (handler *Handler) deadLetter(ctx context.Context, delivery amqp.Delivery, attempt int, reason string) error {
	message := publishingFromDelivery(delivery)
	message.Headers[AttemptHeader] = int32(attempt)
	publishContext, cancel := context.WithTimeout(ctx, handler.publishTimeout)
	defer cancel()
	if err := handler.publish(publishContext, handler.profile.Topology.DeadExchange, delivery, message); err != nil {
		handler.logFailure(ctx, "dead letter publish failed", delivery, attempt, "publish_unavailable")
		if nackErr := delivery.Nack(false, true); nackErr != nil {
			handler.logFailure(ctx, "message requeue failed", delivery, attempt, "nack_failed")
			return errors.New("requeue after dead publish failure")
		}
		return errors.New("publish dead message")
	}
	if err := handler.ack(delivery); err != nil {
		handler.logFailure(ctx, "message acknowledgement failed", delivery, attempt, "ack_failed")
		return errors.New("ack dead-lettered event")
	}
	handler.logEvent(ctx, "event dead lettered", delivery, bus.Envelope{}, attempt, safeReason(reason))
	return nil
}

func (handler *Handler) logEvent(ctx context.Context, message string, delivery amqp.Delivery, envelope bus.Envelope, attempt int, reason string) {
	eventID, eventType := deliveryIdentity(delivery)
	attributes := []any{
		slog.String("event_id", eventID),
		slog.String("event_type", eventType),
		slog.Int("attempt", attempt),
		slog.String("attempt_id", tracing.AttemptID(ctx)),
		slog.String("stage", "consume"),
		slog.String("result", "success"),
		slog.String("reason", safeReason(reason)),
	}
	if handler.profile.IncludePostID && envelope.PostID > 0 {
		attributes = append(attributes, slog.Uint64("post_id", envelope.PostID))
		if envelope.ContentRevision > 0 {
			attributes = append(attributes, slog.Uint64("content_revision", envelope.ContentRevision))
		}
	}
	if outboxID := tracing.OutboxID(ctx); outboxID > 0 {
		attributes = append(attributes, slog.Uint64("outbox_id", outboxID))
	}
	logging.WithTrace(handler.logger, ctx).Info(message, attributes...)
}

func (handler *Handler) logFailure(ctx context.Context, message string, delivery amqp.Delivery, attempt int, reason string) {
	eventID, eventType := deliveryIdentity(delivery)
	attributes := []any{
		slog.String("event_id", eventID),
		slog.String("event_type", eventType),
		slog.Int("attempt", attempt),
		slog.String("attempt_id", tracing.AttemptID(ctx)),
		slog.String("stage", "consume"),
		slog.String("result", "failure"),
		slog.String("reason", safeReason(reason)),
	}
	if outboxID := tracing.OutboxID(ctx); outboxID > 0 {
		attributes = append(attributes, slog.Uint64("outbox_id", outboxID))
	}
	logging.WithTrace(handler.logger, ctx).Error(message, attributes...)
}

func deliveryTraceContext(delivery amqp.Delivery, envelope bus.Envelope) tracing.WireContext {
	parent, parentPresent := traceHeader(delivery.Headers, tracing.TraceParentHeader)
	state, statePresent := traceHeader(delivery.Headers, tracing.TraceStateHeader)
	if parentPresent || statePresent {
		return tracing.WireContext{TraceParent: parent, TraceState: state}
	}
	parent, state, valid := envelope.TraceContext()
	if !valid {
		return tracing.WireContext{}
	}
	return tracing.WireContext{TraceParent: parent, TraceState: state}
}

func traceHeader(headers amqp.Table, key string) (string, bool) {
	if headers == nil {
		return "", false
	}
	value, exists := headers[key]
	if !exists {
		return "", false
	}
	stringValue, ok := value.(string)
	if !ok {
		return "invalid", true
	}
	return stringValue, true
}

func deliveryOutboxID(headers amqp.Table) uint64 {
	value, ok := headers[tracing.OutboxIDHeader]
	if !ok {
		return 0
	}
	switch number := value.(type) {
	case int64:
		if number > 0 {
			return uint64(number)
		}
	case int32:
		if number > 0 {
			return uint64(number)
		}
	case int:
		if number > 0 {
			return uint64(number)
		}
	case uint64:
		return number
	case uint32:
		return uint64(number)
	case uint:
		return uint64(number)
	}
	return 0
}

func deliveryAttemptID(headers amqp.Table) string {
	value, _ := traceHeader(headers, tracing.AttemptIDHeader)
	if len(value) != 32 {
		return ""
	}
	return tracing.AttemptID(tracing.WithAttemptID(context.Background(), value))
}

func permanentReason(err error) string {
	var permanent *PermanentError
	if errors.As(err, &permanent) {
		return safeReason(permanent.Reason)
	}
	return "processing_failed"
}

func safeReason(reason string) string {
	switch reason {
	case "processed", "self_event", "retry_scheduled", "retries_exhausted",
		"invalid_attempt", "attempt_exceeds_max", "invalid_body", "routing_key_mismatch",
		"invalid_envelope", "message_id_mismatch", "content_type_mismatch", "message_type_mismatch",
		"delivery_mode_mismatch", "timestamp_mismatch", "routing_key_not_allowed",
		"unsupported_event_type", "post_not_found", "invalid_document", "index_mapping_rejected",
		"publish_unavailable", "ack_failed", "nack_failed", "processing_failed":
		return reason
	default:
		return "processing_failed"
	}
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

// metricIdentity never promotes message metadata into a label.
func (handler *Handler) metricIdentity(delivery amqp.Delivery) string {
	if handler.profile.Service == "search-indexer" {
		switch delivery.RoutingKey {
		case bus.PostCreatedRoutingKey:
			return "create"
		case bus.PostUpdatedRoutingKey:
			return "update"
		case bus.PostDeletedRoutingKey:
			return "delete"
		}
		return ""
	}
	switch delivery.RoutingKey {
	case bus.CommentCreatedRoutingKey:
		return "comment.created"
	case bus.PostLikedRoutingKey:
		return "post.liked"
	case bus.UserFollowedRoutingKey:
		return "user.followed"
	}
	return "unknown"
}
func (handler *Handler) ack(delivery amqp.Delivery) error {
	started := time.Now()
	err := delivery.Ack(false)
	componentmetrics.Dependency("rabbitmq", err)
	if err == nil && handler.profile.Service != "search-indexer" {
		componentmetrics.Active().Observe("messages_total", time.Since(started), handler.metricIdentity(delivery), "ack")
	}
	return err
}

// A canceled secondary publication must never authorize ack of the original.
func (handler *Handler) publish(ctx context.Context, exchange string, delivery amqp.Delivery, message amqp.Publishing) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := handler.publisher.Publish(ctx, exchange, handler.safeRoutingKey(delivery.RoutingKey), message); err != nil {
		return err
	}
	return ctx.Err()
}
