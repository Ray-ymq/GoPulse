package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	InstrumentationName = "github.com/Ray-ymq/GoPulse/backend"
	TraceParentHeader   = "traceparent"
	TraceStateHeader    = "tracestate"
	AttemptIDHeader     = "x-gopulse-attempt-id"
	OutboxIDHeader      = "x-gopulse-outbox-id"
)

const (
	DefaultSampleRatio     = 0.10
	DefaultQueueCapacity   = 2048
	DefaultBatchSize       = 256
	DefaultBatchTimeout    = time.Second
	DefaultExportTimeout   = 2 * time.Second
	DefaultShutdownTimeout = 5 * time.Second
	MaxClockError          = 250 * time.Millisecond
)

// Config is the small, bounded runtime contract shared by the HTTP server and
// the search indexer. The endpoint is an OTLP gRPC host:port value; it is never
// accepted from request data or arbitrary baggage.
type Config struct {
	Enabled         bool
	Endpoint        string
	ServiceName     string
	SampleRatio     float64
	QueueCapacity   int
	BatchSize       int
	BatchTimeout    time.Duration
	ExportTimeout   time.Duration
	ShutdownTimeout time.Duration
}

// Provider owns the SDK provider and its exporter. A disabled provider keeps
// the OpenTelemetry API on its safe no-op implementation.
type Provider struct {
	provider *sdktrace.TracerProvider
	closed   atomic.Bool
}

var invalidContexts atomic.Uint64

var (
	hexTraceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	hexSpanIDPattern  = regexp.MustCompile(`^[0-9a-f]{16}$`)
	hexFlagsPattern   = regexp.MustCompile(`^[0-9a-f]{2}$`)
	hexVersionPattern = regexp.MustCompile(`^[0-9a-f]{2}$`)
)

func DefaultConfig(serviceName string) Config {
	return Config{
		ServiceName:     serviceName,
		SampleRatio:     DefaultSampleRatio,
		QueueCapacity:   DefaultQueueCapacity,
		BatchSize:       DefaultBatchSize,
		BatchTimeout:    DefaultBatchTimeout,
		ExportTimeout:   DefaultExportTimeout,
		ShutdownTimeout: DefaultShutdownTimeout,
	}
}

func (c Config) withDefaults() Config {
	if c.SampleRatio == 0 {
		c.SampleRatio = DefaultSampleRatio
	}
	if c.QueueCapacity == 0 {
		c.QueueCapacity = DefaultQueueCapacity
	}
	if c.BatchSize == 0 {
		c.BatchSize = DefaultBatchSize
	}
	if c.BatchTimeout == 0 {
		c.BatchTimeout = DefaultBatchTimeout
	}
	if c.ExportTimeout == 0 {
		c.ExportTimeout = DefaultExportTimeout
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = DefaultShutdownTimeout
	}
	return c
}

func (c Config) validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.ServiceName) == "" || len(c.ServiceName) > 64 {
		return errors.New("trace service name must be between 1 and 64 characters")
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(c.Endpoint))
	if err != nil || host == "" || port == "" || strings.ContainsAny(c.Endpoint, "/?#@ \t\r\n") {
		return errors.New("trace endpoint must be a private host:port value")
	}
	parsedPort, parseErr := strconv.Atoi(port)
	if parseErr != nil || parsedPort < 1 || parsedPort > 65535 {
		return errors.New("trace endpoint port must be between 1 and 65535")
	}
	if c.SampleRatio <= 0 || c.SampleRatio > 1 {
		return errors.New("trace sample ratio must be greater than 0 and at most 1")
	}
	if c.QueueCapacity < 1 || c.QueueCapacity > 2048 {
		return errors.New("trace queue capacity must be between 1 and 2048")
	}
	if c.BatchSize < 1 || c.BatchSize > c.QueueCapacity {
		return errors.New("trace batch size must be between 1 and the queue capacity")
	}
	if c.BatchTimeout <= 0 || c.BatchTimeout > 10*time.Second {
		return errors.New("trace batch timeout must be between 1ms and 10s")
	}
	if c.ExportTimeout <= 0 || c.ExportTimeout > 10*time.Second {
		return errors.New("trace export timeout must be between 1ms and 10s")
	}
	if c.ShutdownTimeout <= 0 || c.ShutdownTimeout > 30*time.Second {
		return errors.New("trace shutdown timeout must be between 1ms and 30s")
	}
	return nil
}

// New creates the SDK provider. BatchSpanProcessor intentionally uses its
// default non-blocking enqueue behavior so trace export cannot hold a request
// or a business-message acknowledgement when its bounded queue is full.
func New(ctx context.Context, config Config) (*Provider, error) {
	config = config.withDefaults()
	if err := config.validate(); err != nil {
		return nil, err
	}
	provider := &Provider{}
	if !config.Enabled {
		return provider, nil
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(config.Endpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithTimeout(config.ExportTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	res := resource.NewWithAttributes("",
		attribute.String("service.name", config.ServiceName),
		attribute.String("service.namespace", "gopulse"),
	)
	provider.provider = sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(config.SampleRatio))),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exporter,
			sdktrace.WithMaxQueueSize(config.QueueCapacity),
			sdktrace.WithMaxExportBatchSize(config.BatchSize),
			sdktrace.WithBatchTimeout(config.BatchTimeout),
			sdktrace.WithExportTimeout(config.ExportTimeout),
		)),
	)
	otel.SetTracerProvider(provider.provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return provider, nil
}

// Shutdown flushes the bounded processor with the caller's deadline.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.provider == nil || p.closed.Swap(true) {
		return nil
	}
	return p.provider.Shutdown(ctx)
}

func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer(InstrumentationName).Start(ctx, name, trace.WithAttributes(attrs...))
}

// WireContext contains only the W3C Trace Context fields. Baggage and arbitrary
// headers are intentionally excluded from the business message contract.
type WireContext struct {
	TraceParent string
	TraceState  string
}

func WireFromContext(ctx context.Context) WireContext {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	parent := string(carrier.Get(TraceParentHeader))
	state := string(carrier.Get(TraceStateHeader))
	if _, ok := ParseWireContext(parent, state); !ok {
		return WireContext{}
	}
	return WireContext{TraceParent: parent, TraceState: state}
}

func ExtractHTTP(ctx context.Context, headers http.Header) context.Context {
	result, _ := ExtractHTTPWithStatus(ctx, headers)
	return result
}

func ExtractHTTPWithStatus(ctx context.Context, headers http.Header) (context.Context, bool) {
	return ContextFromWireWithStatus(ctx, WireContext{
		TraceParent: headers.Get(TraceParentHeader),
		TraceState:  headers.Get(TraceStateHeader),
	})
}

// ContextFromWire returns the original context for an absent or invalid
// optional parent. Invalid input is counted and starts a new root span at the
// next Start call; the business payload remains processable.
func ContextFromWire(ctx context.Context, wire WireContext) context.Context {
	result, _ := ContextFromWireWithStatus(ctx, wire)
	return result
}

func ContextFromWireWithStatus(ctx context.Context, wire WireContext) (context.Context, bool) {
	if wire.TraceParent == "" && wire.TraceState == "" {
		return ctx, false
	}
	sc, ok := ParseWireContext(wire.TraceParent, wire.TraceState)
	if !ok {
		invalidContexts.Add(1)
		return ctx, true
	}
	return trace.ContextWithRemoteSpanContext(ctx, sc), false
}

func ParseWireContext(traceParent, traceState string) (trace.SpanContext, bool) {
	if !validTraceParent(traceParent) {
		return trace.SpanContext{}, false
	}
	if traceState != "" {
		if _, err := trace.ParseTraceState(traceState); err != nil {
			return trace.SpanContext{}, false
		}
	}
	flags := traceParent[53:55]
	traceID, _ := trace.TraceIDFromHex(traceParent[3:35])
	spanID, _ := trace.SpanIDFromHex(traceParent[36:52])
	flagBytes, _ := hex.DecodeString(flags)
	if len(flagBytes) != 1 {
		return trace.SpanContext{}, false
	}
	state, _ := trace.ParseTraceState(traceState)
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.TraceFlags(flagBytes[0]),
		TraceState: state,
		Remote:     true,
	}), true
}

func validTraceParent(value string) bool {
	if len(value) != 55 || value[2] != '-' || value[35] != '-' || value[52] != '-' {
		return false
	}
	if !hexVersionPattern.MatchString(value[:2]) || value[:2] == "ff" {
		return false
	}
	if !hexTraceIDPattern.MatchString(value[3:35]) || strings.Trim(value[3:35], "0") == "" {
		return false
	}
	if !hexSpanIDPattern.MatchString(value[36:52]) || strings.Trim(value[36:52], "0") == "" {
		return false
	}
	if !hexFlagsPattern.MatchString(value[53:55]) {
		return false
	}
	return true
}

func InvalidContextCount() uint64 { return invalidContexts.Load() }

func ResetInvalidContextCount() { invalidContexts.Store(0) }

func RecordInvalidContext() { invalidContexts.Add(1) }

type attemptIDKey struct{}

type outboxIDKey struct{}

func WithAttemptID(ctx context.Context, id string) context.Context {
	if !validID(id) {
		return ctx
	}
	return context.WithValue(ctx, attemptIDKey{}, id)
}

func AttemptID(ctx context.Context) string {
	id, _ := ctx.Value(attemptIDKey{}).(string)
	if !validID(id) {
		return ""
	}
	return id
}

func WithOutboxID(ctx context.Context, id uint64) context.Context {
	if id == 0 {
		return ctx
	}
	return context.WithValue(ctx, outboxIDKey{}, id)
}

func OutboxID(ctx context.Context) uint64 {
	id, _ := ctx.Value(outboxIDKey{}).(uint64)
	return id
}

func NewAttemptID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	// Entropy failure must not turn a processable message into a lost message;
	// the timestamp is only a bounded fallback identifier, never a trace ID.
	return fmt.Sprintf("%032x", time.Now().UnixNano())
}

func TraceFields(ctx context.Context) (traceID, spanID string, ok bool) {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return "", "", false
	}
	return spanContext.TraceID().String(), spanContext.SpanID().String(), true
}

func validID(value string) bool {
	return hexTraceIDPattern.MatchString(value) && strings.Trim(value, "0") != ""
}
