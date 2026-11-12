package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/modules/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/shared/correlation"

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
	Logger         *slog.Logger
	Admission      bus.Admission
}

type Handler struct {
	processor      Processor
	publisher      ConfirmingPublisher
	maxRetries     int
	publishTimeout time.Duration
	logger         *slog.Logger
	profile        Profile
	admission      bus.Admission
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
		publishTimeout: options.PublishTimeout, logger: logger, profile: profile, admission: options.Admission,
	}, nil
}

// Handle processes exactly one delivery. Secondary retry/dead publications
// must be confirmed before the original delivery is acknowledged.
func (handler *Handler) Handle(ctx context.Context, delivery amqp.Delivery) error {
	if ctx == nil {
		ctx = context.Background()
	}
	attempt, attemptErr := deliveryAttempt(delivery.Headers)
	envelope, decodeErr := DecodeDelivery(delivery)
	if attemptErr != nil {
		if handler.admission != nil {
			err := handler.parkRaw(ctx, delivery, attempt, "invalid_attempt")

			return err
		}
		return handler.deadLetter(ctx, delivery, attempt, "invalid_attempt")
	}
	if decodeErr != nil {
		if handler.admission != nil {
			err := handler.parkRaw(ctx, delivery, attempt, decodeErr.Error())

			return err
		}
		return handler.deadLetter(ctx, delivery, attempt, decodeErr.Error())
	}
	messageContext := ctx

	messageContext = correlation.WithOutboxID(messageContext, deliveryOutboxID(delivery.Headers))
	attemptID := deliveryAttemptID(delivery.Headers)
	if attemptID == "" {
		attemptID = correlation.NewAttemptID()
	}
	messageContext = correlation.WithAttemptID(messageContext, attemptID)

	if !handler.profile.allows(delivery.RoutingKey) {

		if err := handler.recordFailure(messageContext, envelope, "dead", "routing_key_not_allowed"); err != nil {
			return handler.requeueFailure(messageContext, delivery, err)
		}
		return handler.deadLetter(messageContext, delivery, attempt, "routing_key_not_allowed")
	}
	if handler.admission != nil {
		decision, admissionErr := handler.admit(messageContext, envelope)
		if admissionErr != nil {

			return handler.requeueAdmission(messageContext, delivery, admissionErr)
		}
		if decision == bus.AdmissionPark {
			if err := handler.parkMessage(messageContext, parkingMessage(delivery, envelope, attempt, "epoch_or_legacy_rejected")); err != nil {

				return handler.requeueAdmission(messageContext, delivery, err)
			}
			if err := handler.ack(delivery); err != nil {

				handler.logFailure(messageContext, "message acknowledgement failed", delivery, attempt, "ack_failed")
				return errors.New("ack parked event")
			}

			handler.logEvent(messageContext, "event parked", delivery, envelope, attempt, "epoch_or_legacy_rejected")
			return nil
		}
	}
	if attempt > handler.maxRetries {
		if err := handler.recordFailure(messageContext, envelope, "dead", "attempt_exceeds_max"); err != nil {
			return handler.requeueFailure(messageContext, delivery, err)
		}
		return handler.deadLetter(messageContext, delivery, attempt, "attempt_exceeds_max")
	}
	if handler.profile.IgnoreSelfEvents && envelope.ActorID == envelope.RecipientID {
		if err := handler.ack(delivery); err != nil {

			handler.logFailure(messageContext, "message acknowledgement failed", delivery, attempt, "ack_failed")
			return errors.New("ack self event")
		}

		handler.logEvent(messageContext, "event ignored", delivery, envelope, attempt, "self_event")
		return nil
	}

	processErr := handler.processor.Process(messageContext, envelope)
	if processErr == nil || errors.Is(processErr, bus.ErrDuplicate) {
		if err := handler.ack(delivery); err != nil {

			handler.logFailure(messageContext, "message acknowledgement failed", delivery, attempt, "ack_failed")
			return errors.New("ack processed event")
		}

		handler.logEvent(messageContext, "event processed", delivery, envelope, attempt, "processed")
		return nil
	}
	if ctx.Err() != nil && (errors.Is(processErr, context.Canceled) || errors.Is(processErr, context.DeadlineExceeded)) {

		if nackErr := delivery.Nack(false, true); nackErr != nil {
			handler.logFailure(messageContext, "message requeue failed", delivery, attempt, "nack_failed")
			return errors.New("requeue canceled event")
		}
		return processErr
	}

	if bus.IsPermanent(processErr) {

		if err := handler.recordFailure(messageContext, envelope, "dead", permanentReason(processErr)); err != nil {
			return handler.requeueFailure(messageContext, delivery, err)
		}
		return handler.deadLetter(messageContext, delivery, attempt, permanentReason(processErr))
	}

	if attempt < handler.maxRetries {
		if err := handler.recordFailure(messageContext, envelope, "retry", processErr.Error()); err != nil {
			return handler.requeueFailure(messageContext, delivery, err)
		}
		err := handler.retry(messageContext, delivery, attempt+1)

		return err
	}
	if handler.deferExhausted(envelope) {

		if err := handler.recordFailure(messageContext, envelope, "retry", "recovery_deferred"); err != nil {
			return handler.requeueFailure(messageContext, delivery, err)
		}
		// The search projection is recoverable work. Reset the bounded attempt
		// cycle after the confirmed retry publication so a temporary dependency
		// outage cannot turn an open application obligation into a dead letter.
		err := handler.retry(messageContext, delivery, 0)

		return err
	}

	if err := handler.recordFailure(messageContext, envelope, "dead", "retries_exhausted"); err != nil {
		return handler.requeueFailure(messageContext, delivery, err)
	}
	return handler.deadLetter(messageContext, delivery, attempt, "retries_exhausted")
}

func (handler *Handler) deferExhausted(envelope bus.Envelope) bool {
	if !handler.profile.RetryOnExhaustion || handler.profile.Consumer != "search" || envelope.SchemaVersion != bus.SchemaVersionV2 {
		return false
	}
	switch envelope.EventType {
	case bus.PostCreated, bus.PostUpdated, bus.PostDeleted:
		return true
	default:
		return false
	}
}

func (handler *Handler) admit(ctx context.Context, envelope bus.Envelope) (bus.AdmissionDecision, error) {
	if consumerAdmission, ok := handler.admission.(interface {
		CheckEnvelopeForConsumer(context.Context, bus.Envelope, string) (bus.AdmissionDecision, error)
	}); ok && handler.profile.Consumer != "" {
		return consumerAdmission.CheckEnvelopeForConsumer(ctx, envelope, handler.profile.Consumer)
	}
	return handler.admission.CheckEnvelope(ctx, envelope)
}

func (handler *Handler) requeueAdmission(ctx context.Context, delivery amqp.Delivery, cause error) error {
	if nackErr := delivery.Nack(false, true); nackErr != nil {
		handler.logFailure(ctx, "message requeue failed", delivery, deliveryAttemptForLog(delivery), "nack_failed")
		return fmt.Errorf("requeue after admission failure: %w", nackErr)
	}
	return cause
}

func (handler *Handler) requeueFailure(ctx context.Context, delivery amqp.Delivery, cause error) error {
	if nackErr := delivery.Nack(false, true); nackErr != nil {
		handler.logFailure(ctx, "message requeue failed", delivery, deliveryAttemptForLog(delivery), "nack_failed")
		return fmt.Errorf("requeue after failure evidence error: %w", nackErr)
	}
	return cause
}

func (handler *Handler) recordFailure(ctx context.Context, envelope bus.Envelope, kind, reason string) error {
	if handler.admission == nil || envelope.SchemaVersion != bus.SchemaVersionV2 || envelope.DataEpoch == "" || envelope.EventID == "" {
		return nil
	}
	recorder, ok := handler.admission.(bus.FailureRecorder)
	if !ok {
		return nil
	}
	return recorder.RecordFailure(ctx, bus.FailureRecord{
		DataEpoch: envelope.DataEpoch, EventID: envelope.EventID, Consumer: handler.profile.Consumer,
		Kind: kind, Reason: reason, OccurredAt: time.Now().UTC(),
	})
}

func (handler *Handler) parkRaw(ctx context.Context, delivery amqp.Delivery, attempt int, reason string) error {
	if err := handler.parkMessage(ctx, parkingMessage(delivery, bus.Envelope{}, attempt, reason)); err != nil {
		return handler.requeueAdmission(ctx, delivery, err)
	}
	if err := handler.ack(delivery); err != nil {
		handler.logFailure(ctx, "message acknowledgement failed", delivery, attempt, "ack_failed")
		return errors.New("ack parked invalid event")
	}
	return nil
}

func parkingMessage(delivery amqp.Delivery, envelope bus.Envelope, attempt int, reason string) bus.ParkingMessage {
	return bus.ParkingMessage{
		SourceEpoch: envelope.DataEpoch, EventID: envelope.EventID,
		EventType: string(envelope.EventType), SchemaVersion: envelope.SchemaVersion,
		RoutingKey: delivery.RoutingKey, SourceKind: "broker", Payload: append([]byte(nil), delivery.Body...),
		Reason: reason, AttemptCount: attempt,
	}
}

func deliveryAttemptForLog(delivery amqp.Delivery) int {
	attempt, err := deliveryAttempt(delivery.Headers)
	if err != nil {
		return 0
	}
	return attempt
}

func (handler *Handler) retry(ctx context.Context, delivery amqp.Delivery, nextAttempt int) error {

	message := publishingFromDelivery(delivery)
	message.Headers[AttemptHeader] = int32(nextAttempt)
	// 每次重试生成新的尝试编号。
	message.Headers[correlation.AttemptIDHeader] = correlation.NewAttemptID()
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
		slog.String("attempt_id", correlation.AttemptID(ctx)),
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
	if envelope.DataEpoch != "" {
		attributes = append(attributes, slog.String("data_epoch", envelope.DataEpoch))
	}
	if envelope.CommandID != "" {
		attributes = append(attributes, slog.String("command_id", envelope.CommandID))
	}
	if handler.profile.Consumer != "" {
		attributes = append(attributes, slog.String("consumer", handler.profile.Consumer))
	}
	if outboxID := correlation.OutboxID(ctx); outboxID > 0 {
		attributes = append(attributes, slog.Uint64("outbox_id", outboxID))
	}
	handler.logger.Info(message, attributes...)
}

func (handler *Handler) logFailure(ctx context.Context, message string, delivery amqp.Delivery, attempt int, reason string) {
	eventID, eventType := deliveryIdentity(delivery)
	attributes := []any{
		slog.String("event_id", eventID),
		slog.String("event_type", eventType),
		slog.Int("attempt", attempt),
		slog.String("attempt_id", correlation.AttemptID(ctx)),
		slog.String("stage", "consume"),
		slog.String("result", "failure"),
		slog.String("reason", safeReason(reason)),
	}
	if outboxID := correlation.OutboxID(ctx); outboxID > 0 {
		attributes = append(attributes, slog.Uint64("outbox_id", outboxID))
	}
	if handler.profile.Consumer != "" {
		attributes = append(attributes, slog.String("consumer", handler.profile.Consumer))
	}
	if envelope, err := DecodeDelivery(delivery); err == nil {
		if envelope.DataEpoch != "" {
			attributes = append(attributes, slog.String("data_epoch", envelope.DataEpoch))
		}
		if envelope.CommandID != "" {
			attributes = append(attributes, slog.String("command_id", envelope.CommandID))
		}
		if handler.profile.IncludePostID && envelope.PostID > 0 {
			attributes = append(attributes, slog.Uint64("post_id", envelope.PostID))
			if envelope.ContentRevision > 0 {
				attributes = append(attributes, slog.Uint64("content_revision", envelope.ContentRevision))
			}
		}
	}
	handler.logger.Error(message, attributes...)
}

func messageHeader(headers amqp.Table, key string) (string, bool) {
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
	value, ok := headers[correlation.OutboxIDHeader]
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
	value, _ := messageHeader(headers, correlation.AttemptIDHeader)
	if len(value) != 32 {
		return ""
	}
	return correlation.AttemptID(correlation.WithAttemptID(context.Background(), value))
}

func permanentReason(err error) string {
	var permanent *bus.PermanentError
	if errors.As(err, &permanent) {
		return safeReason(permanent.Reason)
	}
	return "processing_failed"
}

func safeReason(reason string) string {
	switch reason {
	case "processed", "self_event", "retry_scheduled", "recovery_deferred", "retries_exhausted",
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

func (handler *Handler) ack(delivery amqp.Delivery) error {
	err := delivery.Ack(false)

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

// Keep a capacity-blocked delivery unacknowledged and back off on this session.
func (handler *Handler) parkMessage(ctx context.Context, message bus.ParkingMessage) error {
	delay := 100 * time.Millisecond
	for {
		err := handler.admission.ParkMessage(ctx, message)
		if !errors.Is(err, bus.ErrParkingCapacity) {
			return err
		}
		if err := waitContext(ctx, delay); err != nil {
			return err
		}
		delay = min(5*time.Second, delay*2)
	}
}
