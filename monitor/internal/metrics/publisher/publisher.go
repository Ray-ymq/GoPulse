package publisher

import (
	"context"

	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
)

type Publisher interface {
	Publish(context.Context, envelope.Envelope) error
}

type Transport interface {
	Publisher
	PublishRaw(context.Context, string, any) error
}

type Discard struct{}

func (Discard) Publish(context.Context, envelope.Envelope) error { return nil }
func (Discard) PublishRaw(context.Context, string, any) error    { return nil }
