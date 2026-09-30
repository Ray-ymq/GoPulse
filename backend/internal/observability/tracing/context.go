package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// StartFromWire creates a span using a valid persisted or transport parent.
// A malformed optional parent is ignored and counted by ContextFromWire.
func StartFromWire(ctx context.Context, name string, wire WireContext, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return Start(ContextFromWire(ctx, wire), name, attrs...)
}

func LinkFromWire(wire WireContext) (trace.Link, bool) {
	spanContext, ok := ParseWireContext(wire.TraceParent, wire.TraceState)
	if !ok {
		return trace.Link{}, false
	}
	return trace.Link{SpanContext: spanContext}, true
}
