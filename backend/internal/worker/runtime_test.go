package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRuntimeUsesBoundedUniqueInstanceConsumerIdentity(t *testing.T) {
	runtime, err := NewRuntime("amqp://user:password@127.0.0.1:5672/", &processorFake{}, RuntimeOptions{
		Profile:          BusinessProfile,
		InstanceID:       "business-worker-2",
		Prefetch:         2,
		RetryDelay:       time.Second,
		PublishTimeout:   time.Second,
		ShutdownTimeout:  time.Second,
		ReconnectMinimum: time.Millisecond,
		ReconnectMaximum: time.Second,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if runtime.identity != "business-worker-2" || runtime.options.InstanceID != runtime.identity {
		t.Fatalf("runtime identity = %q, options = %q", runtime.identity, runtime.options.InstanceID)
	}
	first := nextConsumerTag(BusinessProfile, runtime.identity)
	second := nextConsumerTag(BusinessProfile, runtime.identity)
	if first == second || !strings.HasPrefix(first, "gopulse-business-worker-business-worker-2-") {
		t.Fatalf("consumer tags are not unique and scoped: %q %q", first, second)
	}
}

type cancellationProcessor struct {
	started chan struct{}
	stopped chan struct{}
}

func (processor *cancellationProcessor) Process(ctx context.Context, _ bus.Envelope) error {
	close(processor.started)
	<-ctx.Done()
	close(processor.stopped)
	return ctx.Err()
}

func TestConsumeSessionCancelsAndJoinsInFlightHandlerAfterShutdownGrace(t *testing.T) {
	processor := &cancellationProcessor{started: make(chan struct{}), stopped: make(chan struct{})}
	publisher := &publisherFake{}
	handler := newTestHandler(t, processor, publisher, nil)
	deliveries := make(chan amqp.Delivery, 1)
	session := &amqpSession{
		deliveries:       deliveries,
		connectionClosed: make(chan *amqp.Error),
		channelClosed:    make(chan *amqp.Error),
	}
	runtime := &Runtime{options: RuntimeOptions{ShutdownTimeout: 20 * time.Millisecond}}
	delivery, acknowledger := validDelivery(t, false)
	deliveries <- delivery

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.consumeSession(ctx, session, handler) }()
	select {
	case <-processor.started:
	case <-time.After(time.Second):
		t.Fatal("processor did not start")
	}
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("shutdown deadline must return a failure")
		}
	case <-time.After(time.Second):
		t.Fatal("consumeSession() did not return after canceling the processor")
	}
	select {
	case <-processor.stopped:
	case <-time.After(time.Second):
		t.Fatal("consumeSession() returned with the processor goroutine still running")
	}
	if acknowledger.acks != 0 || acknowledger.nacks != 1 || !acknowledger.requeue {
		t.Fatalf("acks=%d nacks=%d requeue=%t, want one requeue", acknowledger.acks, acknowledger.nacks, acknowledger.requeue)
	}
}

func TestReadinessFollowsConsumerSession(t *testing.T) {
	runtime := &Runtime{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if runtime.Ready(ctx) == nil {
		t.Fatal("consumer ready before session")
	}
	session := &amqpSession{deliveries: make(chan amqp.Delivery), connectionClosed: make(chan *amqp.Error), channelClosed: make(chan *amqp.Error)}
	done := make(chan error, 1)
	go func() { done <- runtime.consumeSession(ctx, session, nil) }()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for runtime.Ready(ctx) != nil {
		select {
		case <-deadline.C:
			t.Fatal("session did not become ready")
		case <-ticker.C:
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if runtime.Ready(context.Background()) == nil {
		t.Fatal("closed consumer session ready")
	}
}

func TestSecondaryPublishDoesNotAcceptPreviousConfirmation(t *testing.T) {
	for _, ack := range []bool{true, false} {
		confirmations := make(chan amqp.Confirmation, 2)
		confirmations <- amqp.Confirmation{DeliveryTag: 1, Ack: true}
		confirmations <- amqp.Confirmation{DeliveryTag: 2, Ack: ack}
		session := &amqpSession{confirmations: confirmations, returns: make(chan amqp.Return)}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := session.awaitConfirmation(ctx, 2, "stable-event")
		cancel()
		if (err == nil) != ack {
			t.Fatalf("ack=%v err=%v", ack, err)
		}
	}
}
