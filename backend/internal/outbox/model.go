package outbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusLeased    Status = "leased"
	StatusPublished Status = "published"
)

type FailureCode string

const (
	FailurePublishUnavailable FailureCode = "publish_unavailable"
	FailurePublishTimeout     FailureCode = "publish_timeout"
	FailurePublishNack        FailureCode = "publish_nack"
	FailurePublishUnroutable  FailureCode = "publish_unroutable"
	FailurePublisherClosed    FailureCode = "publisher_closed"
	FailureInternal           FailureCode = "internal"
)

var (
	ErrLeaseLost       = errors.New("outbox lease is not current")
	ErrInvalidArgument = errors.New("outbox argument is invalid")
)

type Record struct {
	ID             uint64
	EventID        string
	EventType      bus.EventType
	SchemaVersion  int
	Payload        json.RawMessage
	Status         Status
	AvailableAt    time.Time
	AttemptCount   uint32
	LeaseOwner     string
	LeaseExpiresAt *time.Time
	PublishedAt    *time.Time
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (record Record) Envelope() (bus.Envelope, error) {
	envelope, err := bus.Decode(record.Payload)
	if err != nil {
		return bus.Envelope{}, err
	}
	if envelope.EventID != record.EventID || envelope.EventType != record.EventType || envelope.SchemaVersion != record.SchemaVersion {
		return bus.Envelope{}, fmt.Errorf("outbox columns do not match the event envelope")
	}
	return envelope, nil
}

func validFailureCode(code FailureCode) bool {
	switch code {
	case FailurePublishUnavailable, FailurePublishTimeout, FailurePublishNack, FailurePublishUnroutable, FailurePublisherClosed, FailureInternal:
		return true
	default:
		return false
	}
}
