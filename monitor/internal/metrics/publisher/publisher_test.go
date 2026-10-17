package publisher

import (
	"context"
	"testing"

	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
)

func TestDiscardPublisherDoesNotRequireExternalTransport(t *testing.T) {
	var transport Transport = Discard{}
	message := envelope.Envelope{MessageID: "0123456789abcdef0123456789abcdef"}
	if err := transport.Publish(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if err := transport.PublishRaw(context.Background(), message.MessageID, message); err != nil {
		t.Fatal(err)
	}
}
