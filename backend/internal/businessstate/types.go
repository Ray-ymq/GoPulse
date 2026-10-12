// Package businessstate owns the durable identity and evidence records used by
// the business event boundary. It deliberately does not contain HTTP or
// message-broker code; callers provide the transaction that must include a
// fact, Outbox row, or completion record.
package businessstate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
)

const (
	StatusReady     = "ready"
	StatusRestoring = "restoring"
	ModeLegacyV1    = "legacy_v1"
	ModeV2          = "v2"

	ConsumerSearch            = "search"
	SearchRoutingContractV1   = "post-created-search/1"
	MaximumAttempts           = 1000
	MaximumParkedMessages     = 10000
	MaximumParkedPayloadBytes = 160 << 20
	MaximumOpenRequirements   = 10000
	MaximumRetainedGroups     = 100000
	MaximumRetainedBytes      = 1 << 30
	RetentionPeriod           = 7 * 24 * time.Hour
	RetentionMetadataBytes    = 1024
)

var (
	ErrInvalidArgument      = errors.New("business state argument is invalid")
	ErrNotReady             = errors.New("business state is not ready")
	ErrEpochMismatch        = errors.New("business event belongs to another data epoch")
	ErrBudgetExceeded       = errors.New("business async budget is exhausted")
	ErrParkingCapacity      = errors.New("business message parking capacity is exhausted")
	ErrRequirementAbsent    = errors.New("business event requirement is absent")
	ErrCommandAlreadyExists = errors.New("business command already exists")
	ErrActivationNotDrained = errors.New("business v1 activation drain is incomplete")
)

type Environment struct {
	DataEpoch          string
	Status             string
	Mode               string
	V1Compatibility    bool
	ActivationRevision string
	RestoreOperationID string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Requirement struct {
	DataEpoch               string
	EventID                 string
	Consumer                string
	RoutingContractRevision string
	ApplicableObjectVersion uint64
	PayloadBytes            int
}

type Application struct {
	DataEpoch               string
	EventID                 string
	Consumer                string
	RoutingContractRevision string
	ProjectionGeneration    string
	Result                  string
	ActualVersion           uint64
	CompletedAt             time.Time
}

type Failure = bus.FailureRecord

type Budget struct {
	DataEpoch        string
	OpenRequirements uint64
	RetainedGroups   uint64
	RetainedBytes    uint64
	ParkedMessages   uint64
	ParkedBytes      uint64
	UpdatedAt        time.Time
}

type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Keep the domain package's names convenient while making the worker-facing
// contract live in bus, which avoids an import cycle.
type AdmissionDecision = bus.AdmissionDecision

const (
	AdmissionAccept = bus.AdmissionAccept
	AdmissionPark   = bus.AdmissionPark
)

type ParkingMessage = bus.ParkingMessage

type Repository struct {
	database *sql.DB
	clock    func() time.Time
}

func NewRepository(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, fmt.Errorf("%w: database is required", ErrInvalidArgument)
	}
	return &Repository{database: database, clock: time.Now}, nil
}

func (repository *Repository) WithClock(clock func() time.Time) *Repository {
	if repository != nil && clock != nil {
		repository.clock = clock
	}
	return repository
}

func (repository *Repository) now() time.Time {
	clock := time.Now
	if repository != nil && repository.clock != nil {
		clock = repository.clock
	}
	return clock().UTC().Truncate(time.Microsecond)
}

func normalizeTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC().Truncate(time.Microsecond)
	}
	return value.UTC().Truncate(time.Microsecond)
}

func validateEpoch(value string) error {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return fmt.Errorf("%w: epoch must be a canonical UUID", ErrInvalidArgument)
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("%w: epoch must be a canonical UUID", ErrInvalidArgument)
		}
	}
	if value[14] < '1' || value[14] > '5' || (value[19] != '8' && value[19] != '9' && value[19] != 'a' && value[19] != 'b') {
		return fmt.Errorf("%w: epoch UUID variant is invalid", ErrInvalidArgument)
	}
	return nil
}

// ValidateEpoch is shared by maintenance entrances that must validate an
// epoch before opening the database. Keeping the canonical UUID check here
// prevents a restore command from accepting a value the runtime would reject.
func ValidateEpoch(value string) error { return validateEpoch(value) }

// ValidateOperationID validates the lifecycle operation identity used by a
// restore preparation. Operation IDs are deliberately opaque lowercase hex,
// not UUIDs, because lifecycle journals already use this bounded format.
func ValidateOperationID(value string) error {
	if len(value) != 64 || !isLowerHex(value) {
		return fmt.Errorf("%w: operation ID is invalid", ErrInvalidArgument)
	}
	return nil
}

func validateEventID(value string) error {
	if err := validateEpoch(value); err != nil {
		return fmt.Errorf("%w: event ID is invalid", ErrInvalidArgument)
	}
	return nil
}

func validateText(value, name string, maximum int) error {
	if value == "" || strings.TrimSpace(value) != value || len(value) > maximum {
		return fmt.Errorf("%w: %s is invalid", ErrInvalidArgument, name)
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return fmt.Errorf("%w: %s contains unsupported characters", ErrInvalidArgument, name)
		}
	}
	return nil
}

func validateRequirement(requirement Requirement) error {
	if err := validateEpoch(requirement.DataEpoch); err != nil {
		return err
	}
	if err := validateEventID(requirement.EventID); err != nil {
		return err
	}
	if err := validateText(requirement.Consumer, "consumer", 32); err != nil {
		return err
	}
	if err := validateText(requirement.RoutingContractRevision, "routing contract revision", 64); err != nil {
		return err
	}
	if requirement.PayloadBytes < 0 || requirement.PayloadBytes > bus.MaxMessageBytes {
		return fmt.Errorf("%w: payload size is invalid", ErrInvalidArgument)
	}
	return nil
}

func validateApplication(application Application) error {
	if err := validateApplicationLookup(application); err != nil {
		return err
	}
	if application.Result != "applied" && application.Result != "no_effect_resource_absent" {
		return fmt.Errorf("%w: application result is invalid", ErrInvalidArgument)
	}
	return nil
}

func validateApplicationLookup(application Application) error {
	if err := validateRequirement(Requirement{
		DataEpoch: application.DataEpoch, EventID: application.EventID,
		Consumer: application.Consumer, RoutingContractRevision: application.RoutingContractRevision,
	}); err != nil {
		return err
	}
	if err := validateText(application.ProjectionGeneration, "projection generation", 255); err != nil {
		return err
	}
	return nil
}

func validMode(value string) bool { return value == ModeLegacyV1 || value == ModeV2 }
