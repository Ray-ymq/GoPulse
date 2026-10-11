package bus

import (
	"context"
	"time"
)

// AdmissionDecision is the broker-side result of a persistent environment
// check. A parked delivery may be acknowledged only after ParkMessage returns
// successfully.
type AdmissionDecision int

const (
	AdmissionAccept AdmissionDecision = iota
	AdmissionPark
)

type ParkingMessage struct {
	DataEpoch     string
	SourceEpoch   string
	EventID       string
	EventType     string
	SchemaVersion int
	RoutingKey    string
	SourceKind    string
	Payload       []byte
	Reason        string
	AttemptCount  int
	ParkedAt      time.Time
}

// FailureRecord is the bounded durable summary a consumer may write before it
// retries or dead-letters a v2 event. It intentionally carries no dependency
// error text or payload.
type FailureRecord struct {
	DataEpoch  string
	EventID    string
	Consumer   string
	Kind       string
	Reason     string
	OccurredAt time.Time
}

type FailureRecorder interface {
	RecordFailure(context.Context, FailureRecord) error
}

type Admission interface {
	CheckEnvelope(context.Context, Envelope) (AdmissionDecision, error)
	ParkMessage(context.Context, ParkingMessage) error
}
