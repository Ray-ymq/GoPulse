package worker

import (
	"context"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	amqp "github.com/rabbitmq/amqp091-go"
)

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
		if err != nil {
			t.Fatalf("consumeSession() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("consumeSession() did not return after canceling the processor")
	}
	select {
	case <-processor.stopped:
	default:
		t.Fatal("consumeSession() returned with the processor goroutine still running")
	}
	if acknowledger.acks != 0 || acknowledger.nacks != 1 || !acknowledger.requeue {
		t.Fatalf("acks=%d nacks=%d requeue=%t, want one requeue", acknowledger.acks, acknowledger.nacks, acknowledger.requeue)
	}
}
