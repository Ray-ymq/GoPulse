package bus

import (
	"context"
	"errors"
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

type Admission interface {
	CheckEnvelope(context.Context, Envelope) (AdmissionDecision, error)
	ParkMessage(context.Context, ParkingMessage) error
}

var ErrParkingCapacity = errors.New("business message parking capacity is exhausted")
