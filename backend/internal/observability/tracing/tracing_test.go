package tracing

import (
	"context"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

func TestWireContextRoundTripAndInvalidParent(t *testing.T) {
	ResetInvalidContextCount()
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:  trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
	})
	ctx := trace.ContextWithSpanContext(context.Background(), parent)
	wire := WireFromContext(ctx)
	if wire.TraceParent == "" || wire.TraceState != "" {
		t.Fatalf("unexpected wire context: %#v", wire)
	}
	restored := ContextFromWire(context.Background(), wire)
	if got := trace.SpanContextFromContext(restored); !got.IsValid() || got.TraceID() != parent.TraceID() || got.SpanID() != parent.SpanID() {
		t.Fatalf("restored span context = %v", got)
	}

	invalid := ContextFromWire(context.Background(), WireContext{TraceParent: "00-00000000000000000000000000000001-0000000000000000-01"})
	if got := trace.SpanContextFromContext(invalid); got.IsValid() {
		t.Fatalf("invalid parent unexpectedly restored: %v", got)
	}
	if InvalidContextCount() != 1 {
		t.Fatalf("invalid context count = %d, want 1", InvalidContextCount())
	}
}

func TestExtractHTTPUsesOnlyW3CTraceContext(t *testing.T) {
	ResetInvalidContextCount()
	headers := make(http.Header)
	headers.Set(TraceParentHeader, "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01")
	headers.Set("baggage", "user_id=should-not-propagate")
	ctx := ExtractHTTP(context.Background(), headers)
	if got := trace.SpanContextFromContext(ctx); !got.IsValid() || got.TraceID().String() != "0102030405060708090a0b0c0d0e0f10" {
		t.Fatalf("extracted span context = %v", got)
	}
	if got := WireFromContext(ctx); got.TraceParent == "" {
		t.Fatal("expected traceparent after extraction")
	}
}

func TestAttemptIDIsBounded(t *testing.T) {
	id := NewAttemptID()
	if len(id) != 32 {
		t.Fatalf("attempt ID length = %d", len(id))
	}
	if AttemptID(WithAttemptID(context.Background(), id)) != id {
		t.Fatal("valid attempt ID was not stored")
	}
	if AttemptID(WithAttemptID(context.Background(), "arbitrary-header-value")) != "" {
		t.Fatal("arbitrary attempt ID was accepted")
	}
	if OutboxID(WithOutboxID(context.Background(), 41)) != 41 || OutboxID(WithOutboxID(context.Background(), 0)) != 0 {
		t.Fatal("bounded outbox ID context was not preserved")
	}
}

func TestConfigRejectsUnboundedEndpointAndAcceptsPrivateHostPort(t *testing.T) {
	config := DefaultConfig("backend")
	config.Enabled = true
	config.Endpoint = "collector:4317"
	if err := config.validate(); err != nil {
		t.Fatalf("valid collector endpoint rejected: %v", err)
	}
	for _, endpoint := range []string{"collector", "collector:0", "collector:65536", "http://collector:4317"} {
		config.Endpoint = endpoint
		if err := config.validate(); err == nil {
			t.Fatalf("endpoint %q was accepted", endpoint)
		}
	}
	config.Endpoint = "collector:4317"
	config.BatchTimeout = 11 * time.Second
	if err := config.validate(); err == nil {
		t.Fatal("batch timeout above the contract was accepted")
	}
}
