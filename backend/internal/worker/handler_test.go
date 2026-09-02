package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	amqp "github.com/rabbitmq/amqp091-go"
)

type acknowledgerFake struct {
	acks, nacks int
	requeue     bool
	err         error
}

func (fake *acknowledgerFake) Ack(uint64, bool) error {
	fake.acks++
	return fake.err
}
func (fake *acknowledgerFake) Nack(_ uint64, _ bool, requeue bool) error {
	fake.nacks++
	fake.requeue = requeue
	return fake.err
}
func (fake *acknowledgerFake) Reject(uint64, bool) error { return nil }

type processorFake struct {
	calls int
	err   error
}

func (fake *processorFake) Process(context.Context, bus.Envelope) error {
	fake.calls++
	return fake.err
}

type publishCall struct {
	exchange, routingKey string
	message              amqp.Publishing
}

type publisherFake struct {
	calls []publishCall
	err   error
}

func (fake *publisherFake) Publish(_ context.Context, exchange, routingKey string, message amqp.Publishing) error {
	fake.calls = append(fake.calls, publishCall{exchange: exchange, routingKey: routingKey, message: message})
	return fake.err
}

func TestHandlerAcknowledgesSuccessfulDuplicateAndSelfEvents(t *testing.T) {
	for _, test := range []struct {
		name       string
		self       bool
		wantCalls  int
		processorE error
	}{
		{name: "created", wantCalls: 1},
		{name: "idempotent duplicate", wantCalls: 1},
		{name: "self event", self: true, wantCalls: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			processor := &processorFake{err: test.processorE}
			publisher := &publisherFake{}
			handler := newTestHandler(t, processor, publisher, nil)
			delivery, acknowledger := validDelivery(t, test.self)
			if err := handler.Handle(context.Background(), delivery); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if acknowledger.acks != 1 || acknowledger.nacks != 0 || processor.calls != test.wantCalls || len(publisher.calls) != 0 {
				t.Fatalf("acks=%d nacks=%d processor=%d publishes=%d", acknowledger.acks, acknowledger.nacks, processor.calls, len(publisher.calls))
			}
		})
	}
}

func TestHandlerRoutesTemporaryFailureThroughFiniteRetryThenDeadQueue(t *testing.T) {
	for _, test := range []struct {
		name         string
		attempt      int32
		wantExchange string
		wantAttempt  int32
	}{
		{name: "retry", attempt: 1, wantExchange: platform.BusinessRetryExchange, wantAttempt: 2},
		{name: "exhausted", attempt: 3, wantExchange: platform.BusinessDeadExchange, wantAttempt: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			processor := &processorFake{err: errors.New("mysql password=secret unavailable")}
			publisher := &publisherFake{}
			var logs []string
			handler := newTestHandler(t, processor, publisher, func(format string, values ...any) {
				logs = append(logs, fmt.Sprintf(format, values...))
			})
			delivery, acknowledger := validDelivery(t, false)
			delivery.Headers[AttemptHeader] = test.attempt
			if err := handler.Handle(context.Background(), delivery); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if acknowledger.acks != 1 || acknowledger.nacks != 0 || len(publisher.calls) != 1 {
				t.Fatalf("acks=%d nacks=%d publishes=%d", acknowledger.acks, acknowledger.nacks, len(publisher.calls))
			}
			call := publisher.calls[0]
			if call.exchange != test.wantExchange || call.routingKey != delivery.RoutingKey || call.message.Headers[AttemptHeader] != test.wantAttempt {
				t.Fatalf("publish call = %#v", call)
			}
			if call.message.MessageId != delivery.MessageId || call.message.Type != delivery.Type || string(call.message.Body) != string(delivery.Body) {
				t.Fatal("secondary publish did not preserve message identity")
			}
			joined := strings.Join(logs, " ")
			if strings.Contains(joined, "password") || strings.Contains(joined, "secret") || strings.Contains(joined, string(delivery.Body)) {
				t.Fatalf("safe log leaked processing detail: %q", joined)
			}
		})
	}
}

func TestHandlerDeadLettersPermanentMessagesWithoutBlockingAndUsesFallbackRoute(t *testing.T) {
	processor := &processorFake{}
	publisher := &publisherFake{}
	handler := newTestHandler(t, processor, publisher, nil)
	delivery, acknowledger := validDelivery(t, false)
	delivery.RoutingKey = "unknown.route"

	if err := handler.Handle(context.Background(), delivery); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if processor.calls != 0 || acknowledger.acks != 1 || acknowledger.nacks != 0 || len(publisher.calls) != 1 {
		t.Fatalf("processor=%d acks=%d nacks=%d publishes=%d", processor.calls, acknowledger.acks, acknowledger.nacks, len(publisher.calls))
	}
	if publisher.calls[0].exchange != platform.BusinessDeadExchange || publisher.calls[0].routingKey != platform.BusinessInvalidRoutingKey {
		t.Fatalf("dead publish = %#v", publisher.calls[0])
	}
}

func TestHandlerRequeuesOriginalWhenSecondaryPublishIsNotConfirmed(t *testing.T) {
	for _, permanentMessage := range []bool{false, true} {
		processor := &processorFake{err: errors.New("temporary")}
		publisher := &publisherFake{err: errors.New("confirm unavailable")}
		handler := newTestHandler(t, processor, publisher, nil)
		delivery, acknowledger := validDelivery(t, false)
		if permanentMessage {
			delivery.Body = []byte("not json")
		}
		if err := handler.Handle(context.Background(), delivery); err == nil {
			t.Fatal("Handle() error = nil")
		}
		if acknowledger.acks != 0 || acknowledger.nacks != 1 || !acknowledger.requeue {
			t.Fatalf("acks=%d nacks=%d requeue=%t", acknowledger.acks, acknowledger.nacks, acknowledger.requeue)
		}
	}
}

func TestDecodeDeliveryRejectsPropertyMismatches(t *testing.T) {
	mutations := map[string]func(*amqp.Delivery){
		"message id":    func(delivery *amqp.Delivery) { delivery.MessageId = "123e4567-e89b-12d3-a456-426614174999" },
		"content type":  func(delivery *amqp.Delivery) { delivery.ContentType = "text/plain" },
		"message type":  func(delivery *amqp.Delivery) { delivery.Type = "post.liked" },
		"routing key":   func(delivery *amqp.Delivery) { delivery.RoutingKey = bus.PostLikedRoutingKey },
		"delivery mode": func(delivery *amqp.Delivery) { delivery.DeliveryMode = amqp.Transient },
		"timestamp":     func(delivery *amqp.Delivery) { delivery.Timestamp = delivery.Timestamp.Add(time.Hour) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			delivery, _ := validDelivery(t, false)
			mutate(&delivery)
			if _, err := DecodeDelivery(delivery); !IsPermanent(err) {
				t.Fatalf("DecodeDelivery() error = %v, want permanent", err)
			}
		})
	}
}

func TestHandlerRejectsUntrustedAttemptHeader(t *testing.T) {
	for name, attempt := range map[string]any{"non-numeric": "999", "above configured maximum": int32(4)} {
		t.Run(name, func(t *testing.T) {
			processor := &processorFake{}
			publisher := &publisherFake{}
			handler := newTestHandler(t, processor, publisher, nil)
			delivery, acknowledger := validDelivery(t, false)
			delivery.Headers[AttemptHeader] = attempt
			if err := handler.Handle(context.Background(), delivery); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if acknowledger.acks != 1 || len(publisher.calls) != 1 || publisher.calls[0].exchange != platform.BusinessDeadExchange {
				t.Fatalf("acks=%d publishes=%#v", acknowledger.acks, publisher.calls)
			}
		})
	}
}

func newTestHandler(t *testing.T, processor Processor, publisher ConfirmingPublisher, logger func(string, ...any)) *Handler {
	t.Helper()
	handler, err := NewHandler(processor, publisher, HandlerOptions{MaxRetries: 3, PublishTimeout: time.Second, Logger: logger})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

func validDelivery(t *testing.T, self bool) (amqp.Delivery, *acknowledgerFake) {
	t.Helper()
	actorID, recipientID := uint64(1), uint64(2)
	if self {
		recipientID = actorID
	}
	envelope, err := bus.NewCommentCreated(time.Date(2026, time.September, 2, 5, 6, 7, 0, time.UTC), actorID, recipientID, 3, 4)
	if err != nil {
		t.Fatalf("NewCommentCreated() error = %v", err)
	}
	body, err := bus.Encode(envelope)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	metadata, _ := envelope.Metadata()
	acknowledger := &acknowledgerFake{}
	return amqp.Delivery{
		Acknowledger: acknowledger, DeliveryTag: 1, Headers: amqp.Table{},
		ContentType: metadata.ContentType, DeliveryMode: amqp.Persistent, MessageId: metadata.MessageID,
		Timestamp: metadata.Timestamp, Type: metadata.Type, RoutingKey: bus.CommentCreatedRoutingKey, Body: body,
	}, acknowledger
}

func TestSearchProfileRoutesPermanentProcessorFailureDirectlyToSearchDead(t *testing.T) {
	processor := &processorFake{err: NewPermanentError("post_not_found")}
	publisher := &publisherFake{}
	handler, err := NewHandler(processor, publisher, HandlerOptions{
		Profile: SearchProfile, MaxRetries: 3, PublishTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, acknowledger := validSearchDelivery(t)
	if err := handler.Handle(context.Background(), delivery); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if processor.calls != 1 || acknowledger.acks != 1 || acknowledger.nacks != 0 || len(publisher.calls) != 1 {
		t.Fatalf("processor=%d acks=%d nacks=%d publishes=%d", processor.calls, acknowledger.acks, acknowledger.nacks, len(publisher.calls))
	}
	call := publisher.calls[0]
	if call.exchange != platform.SearchDeadExchange || call.routingKey != bus.PostCreatedRoutingKey || call.message.Headers[AttemptHeader] != int32(0) {
		t.Fatalf("search dead publish = %#v", call)
	}
}

func validSearchDelivery(t *testing.T) (amqp.Delivery, *acknowledgerFake) {
	t.Helper()
	envelope, err := bus.NewPostCreated(time.Date(2026, time.September, 2, 5, 6, 7, 0, time.UTC), 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	body, err := bus.Encode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ := envelope.Metadata()
	acknowledger := &acknowledgerFake{}
	return amqp.Delivery{
		Acknowledger: acknowledger, DeliveryTag: 1, Headers: amqp.Table{},
		ContentType: metadata.ContentType, DeliveryMode: amqp.Persistent, MessageId: metadata.MessageID,
		Timestamp: metadata.Timestamp, Type: metadata.Type, RoutingKey: bus.PostCreatedRoutingKey, Body: body,
	}, acknowledger
}
