package businessstate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
)

// CheckEnvelope is the persistent pre-side-effect gate used by both workers.
// v1 has no epoch field and is admitted only while the explicit compatibility
// license is active. It never creates a v2 completion promise.
func (repository *Repository) CheckEnvelope(ctx context.Context, envelope bus.Envelope) (AdmissionDecision, error) {
	if repository == nil || ctx == nil {
		return AdmissionPark, fmt.Errorf("%w: admission arguments are invalid", ErrInvalidArgument)
	}
	if err := envelope.Validate(); err != nil {
		return AdmissionPark, fmt.Errorf("%w: invalid envelope", ErrInvalidArgument)
	}
	environment, err := repository.Current(ctx)
	if err != nil {
		return AdmissionPark, err
	}
	if envelope.SchemaVersion == bus.SchemaVersionV1 {
		if environment.Status == StatusReady && environment.Mode == ModeLegacyV1 && environment.V1Compatibility {
			return AdmissionAccept, nil
		}
		return AdmissionPark, nil
	}
	if envelope.SchemaVersion != bus.SchemaVersionV2 {
		return AdmissionPark, nil
	}
	if envelope.DataEpoch != environment.DataEpoch {
		return AdmissionPark, nil
	}
	if environment.Status != StatusReady {
		return AdmissionPark, ErrNotReady
	}
	return AdmissionAccept, nil
}

// CheckEnvelopeForConsumer additionally verifies the durable obligation for
// the search consumer. A v2 search event without that obligation has no
// authoritative basis and is parked rather than inventing a requirement.
func (repository *Repository) CheckEnvelopeForConsumer(ctx context.Context, envelope bus.Envelope, consumer string) (AdmissionDecision, error) {
	decision, err := repository.CheckEnvelope(ctx, envelope)
	if err != nil || decision == AdmissionPark || envelope.SchemaVersion != bus.SchemaVersionV2 || consumer != ConsumerSearch {
		return decision, err
	}
	if envelope.EventType != bus.PostCreated && envelope.EventType != bus.PostUpdated && envelope.EventType != bus.PostDeleted {
		return decision, nil
	}
	exists, err := repository.HasRequirement(ctx, Requirement{
		DataEpoch: envelope.DataEpoch, EventID: envelope.EventID, Consumer: ConsumerSearch,
		RoutingContractRevision: SearchRoutingContractV1,
	})
	if err != nil {
		return AdmissionPark, err
	}
	if !exists {
		return AdmissionPark, nil
	}
	return AdmissionAccept, nil
}

// ParkMessage stores the original payload before a caller acknowledges its
// delivery. It uses a unique payload/reason pair so a broker redelivery cannot
// grow the table indefinitely.
func (repository *Repository) ParkMessage(ctx context.Context, message ParkingMessage) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return fmt.Errorf("%w: parking arguments are invalid", ErrInvalidArgument)
	}
	if len(message.Payload) == 0 || len(message.Payload) > bus.MaxMessageBytes {
		return fmt.Errorf("%w: parked payload size is invalid", ErrInvalidArgument)
	}
	if message.DataEpoch != "" {
		if err := validateEpoch(message.DataEpoch); err != nil {
			return err
		}
	}
	if message.SourceEpoch != "" {
		if err := validateEpoch(message.SourceEpoch); err != nil {
			return err
		}
	}
	if message.EventID != "" {
		if err := validateEventID(message.EventID); err != nil {
			return err
		}
	}
	reason := safeReason(message.Reason)
	sourceKind := message.SourceKind
	if sourceKind == "" {
		sourceKind = "broker"
	}
	if err := validateText(sourceKind, "source kind", 32); err != nil {
		return err
	}
	if err := validateText(reason, "parking reason", 64); err != nil {
		return err
	}
	now := normalizeTime(message.ParkedAt)
	hash := sha256.Sum256(message.Payload)
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return errors.New("begin business message parking")
	}
	defer func() { _ = tx.Rollback() }()
	environment, err := scanEnvironment(tx.QueryRowContext(ctx, `
		SELECT data_epoch, status, mode, v1_compatibility_enabled, activation_revision,
		       COALESCE(restore_operation_id, ''), created_at, updated_at
		FROM business_environment WHERE singleton=1 FOR UPDATE`))
	if err != nil {
		return err
	}
	if message.SourceEpoch == "" && message.DataEpoch != "" && message.DataEpoch != environment.DataEpoch {
		message.SourceEpoch = message.DataEpoch
	}
	// data_epoch is the accounting/current-domain epoch. A mismatched source is
	// retained separately so a parked message can be inspected without charging
	// an obsolete budget row.
	message.DataEpoch = environment.DataEpoch
	var parkedMessages, parkedBytes uint64
	if err := tx.QueryRowContext(ctx, `SELECT parked_messages, parked_bytes FROM business_async_budget WHERE data_epoch=? FOR UPDATE`, environment.DataEpoch).Scan(&parkedMessages, &parkedBytes); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := ensureBudgetTx(ctx, tx, environment.DataEpoch); err != nil {
				return err
			}
			parkedMessages, parkedBytes = 0, 0
		} else {
			return errors.New("read business parking budget")
		}
	}
	if parkedMessages >= MaximumParkedMessages || parkedBytes > MaximumParkedPayloadBytes-uint64(len(message.Payload)) {
		return ErrParkingCapacity
	}
	result, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO business_message_parking
			(data_epoch, source_epoch, event_id, event_type, schema_version, routing_key, source_kind, payload, payload_sha256, reason, attempt_count, parked_at)
		VALUES (?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		message.DataEpoch, message.SourceEpoch, message.EventID, boundedASCII(message.EventType, 64), message.SchemaVersion,
		boundedASCII(message.RoutingKey, 128), sourceKind, message.Payload, hash[:], reason, max(0, message.AttemptCount), now)
	if err != nil {
		return errors.New("park business message")
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return errors.New("inspect business parking result")
	}
	if inserted == 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE business_async_budget SET parked_messages=parked_messages+1, parked_bytes=parked_bytes+?, updated_at=? WHERE data_epoch=?`, len(message.Payload), now, environment.DataEpoch); err != nil {
			return errors.New("account parked business message")
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.New("commit business message parking")
	}
	return nil
}

func (repository *Repository) RecordFailure(ctx context.Context, failure Failure) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return fmt.Errorf("%w: failure arguments are invalid", ErrInvalidArgument)
	}
	if err := validateEpoch(failure.DataEpoch); err != nil {
		return err
	}
	if err := validateEventID(failure.EventID); err != nil {
		return err
	}
	if err := validateText(failure.Consumer, "consumer", 32); err != nil {
		return err
	}
	reason := safeReason(failure.Reason)
	now := normalizeTime(failure.OccurredAt)
	kind := failure.Kind
	if kind != "retry" && kind != "dead" {
		return fmt.Errorf("%w: failure kind is invalid", ErrInvalidArgument)
	}
	_, err := repository.database.ExecContext(ctx, `
		INSERT INTO business_event_failures
			(data_epoch, event_id, consumer, attempt_count, retry_count, dead_count, last_reason, last_failed_at, last_retry_at, last_dead_at, retry_summary, dead_summary)
		VALUES (?, ?, ?, 1, IF(?='retry',1,0), IF(?='dead',1,0), ?, ?, IF(?='retry',?,NULL), IF(?='dead',?,NULL), IF(?='retry',?,'') , IF(?='dead',?,'') )
		ON DUPLICATE KEY UPDATE
			attempt_count=LEAST(attempt_count+1, 1000),
			retry_count=LEAST(retry_count+IF(VALUES(last_retry_at) IS NULL,0,1), 1000),
			dead_count=LEAST(dead_count+IF(VALUES(last_dead_at) IS NULL,0,1), 1000),
			last_reason=VALUES(last_reason), last_failed_at=VALUES(last_failed_at),
			last_retry_at=COALESCE(VALUES(last_retry_at), last_retry_at),
			last_dead_at=COALESCE(VALUES(last_dead_at), last_dead_at),
			retry_summary=IF(VALUES(last_retry_at) IS NULL, retry_summary, VALUES(retry_summary)),
			dead_summary=IF(VALUES(last_dead_at) IS NULL, dead_summary, VALUES(dead_summary))`,
		failure.DataEpoch, failure.EventID, failure.Consumer, kind, kind, reason, now,
		kind, now, kind, now, kind, reason, kind, reason)
	if err != nil {
		return errors.New("record business event failure")
	}
	return nil
}

func safeReason(reason string) string {
	if reason == "" || len(reason) > 64 {
		return "unknown"
	}
	for _, character := range reason {
		allowed := (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' || character == '-'
		if !allowed {
			return "unknown"
		}
	}
	return reason
}

func boundedASCII(value string, maximum int) string {
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}
