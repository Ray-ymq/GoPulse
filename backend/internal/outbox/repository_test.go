package outbox

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/bus"

	"errors"
	"testing"
	"time"
)

func TestExponentialBackoffIsBounded(t *testing.T) {
	tests := map[uint32]time.Duration{
		0:   time.Second,
		1:   time.Second,
		2:   2 * time.Second,
		5:   16 * time.Second,
		10:  256 * time.Second,
		100: 256 * time.Second,
	}
	for attempt, want := range tests {
		if got := ExponentialBackoff(attempt); got != want {
			t.Fatalf("ExponentialBackoff(%d) = %s, want %s", attempt, got, want)
		}
	}
}

func TestValidationRejectsUnsafeLeaseAndFailureValues(t *testing.T) {
	for _, owner := range []string{"", " owner", "owner ", "owner\n", string(make([]byte, maximumOwnerBytes+1))} {
		if err := validateOwner(owner); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("validateOwner(%q) error = %v", owner, err)
		}
	}
	if err := validateOwner("dispatcher-01"); err != nil {
		t.Fatalf("validateOwner() error = %v", err)
	}
	if validFailureCode("amqp://user:password@example.invalid/") {
		t.Fatal("arbitrary failure detail accepted")
	}
}

func TestRecordEnvelopeRejectsColumnAndPayloadDrift(t *testing.T) {
	event, err := bus.NewPostLiked(time.Date(2026, time.September, 2, 1, 0, 0, 0, time.UTC), 1, 2, 3)
	if err != nil {
		t.Fatalf("NewPostLiked() error = %v", err)
	}
	payload, err := bus.Encode(event)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	record := Record{EventID: event.EventID, EventType: event.EventType, SchemaVersion: event.SchemaVersion, Payload: payload}
	if _, err := record.Envelope(); err != nil {
		t.Fatalf("Envelope() error = %v", err)
	}
	record.EventType = bus.CommentCreated
	if _, err := record.Envelope(); err == nil {
		t.Fatal("Envelope() accepted mismatched event type")
	}
}
