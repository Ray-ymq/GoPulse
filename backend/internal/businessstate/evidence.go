package businessstate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (repository *Repository) RegisterRequirement(ctx context.Context, requirement Requirement) error {
	if err := validateRequirement(requirement); err != nil {
		return err
	}
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return errors.New("begin business event requirement")
	}
	defer func() { _ = tx.Rollback() }()
	if err := repository.RegisterRequirementTx(ctx, tx, requirement); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.New("commit business event requirement")
	}
	return nil
}

// RegisterRequirementTx is intended to be called by a future v2 fact writer
// using the same transaction as the post and Outbox rows.
func (repository *Repository) RegisterRequirementTx(ctx context.Context, executor Executor, requirement Requirement) error {
	if executor == nil {
		return fmt.Errorf("%w: transaction is required", ErrInvalidArgument)
	}
	if err := validateRequirement(requirement); err != nil {
		return err
	}
	if requirement.PayloadBytes == 0 {
		requirement.PayloadBytes = 1
	}
	if _, err := executor.ExecContext(ctx, `INSERT INTO business_async_budget (data_epoch) VALUES (?) ON DUPLICATE KEY UPDATE data_epoch=VALUES(data_epoch)`, requirement.DataEpoch); err != nil {
		return errors.New("initialize requirement budget")
	}
	var open uint64
	if err := executor.QueryRowContext(ctx, `SELECT open_requirements FROM business_async_budget WHERE data_epoch=? FOR UPDATE`, requirement.DataEpoch).Scan(&open); err != nil {
		return errors.New("read requirement budget")
	}
	result, err := executor.ExecContext(ctx, `
		INSERT IGNORE INTO business_event_requirements
			(data_epoch, event_id, consumer, routing_contract_revision, applicable_object_version, payload_bytes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, requirement.DataEpoch, requirement.EventID, requirement.Consumer,
		requirement.RoutingContractRevision, requirement.ApplicableObjectVersion, requirement.PayloadBytes, repository.now(), repository.now())
	if err != nil {
		return errors.New("insert business event requirement")
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return errors.New("inspect requirement insert")
	}
	if inserted == 0 {
		return nil
	}
	if open >= MaximumOpenRequirements {
		return ErrBudgetExceeded
	}
	if _, err := executor.ExecContext(ctx, `UPDATE business_async_budget SET open_requirements=open_requirements+1, updated_at=? WHERE data_epoch=?`, repository.now(), requirement.DataEpoch); err != nil {
		return errors.New("account business event requirement")
	}
	return nil
}

func (repository *Repository) HasRequirement(ctx context.Context, requirement Requirement) (bool, error) {
	if err := validateRequirement(requirement); err != nil {
		return false, err
	}
	var exists bool
	if err := repository.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM business_event_requirements WHERE data_epoch=? AND event_id=? AND consumer=? AND routing_contract_revision=?)`, requirement.DataEpoch, requirement.EventID, requirement.Consumer, requirement.RoutingContractRevision).Scan(&exists); err != nil {
		return false, errors.New("check business event requirement")
	}
	return exists, nil
}

func (repository *Repository) RecordApplication(ctx context.Context, application Application) error {
	if err := validateApplication(application); err != nil {
		return err
	}
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return errors.New("begin business application result")
	}
	defer func() { _ = tx.Rollback() }()
	if err := repository.RecordApplicationTx(ctx, tx, application); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.New("commit business application result")
	}
	return nil
}

func (repository *Repository) RecordApplicationTx(ctx context.Context, executor Executor, application Application) error {
	if executor == nil {
		return fmt.Errorf("%w: transaction is required", ErrInvalidArgument)
	}
	if err := validateApplication(application); err != nil {
		return err
	}
	completedAt := normalizeTime(application.CompletedAt)
	var payloadBytes int
	var closedAt sql.NullTime
	if err := executor.QueryRowContext(ctx, `
		SELECT payload_bytes, closed_at FROM business_event_requirements
		WHERE data_epoch=? AND event_id=? AND consumer=? AND routing_contract_revision=? FOR UPDATE`,
		application.DataEpoch, application.EventID, application.Consumer, application.RoutingContractRevision).Scan(&payloadBytes, &closedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRequirementAbsent
		}
		return errors.New("lock business event requirement")
	}
	result, err := executor.ExecContext(ctx, `
		INSERT IGNORE INTO business_application_results
			(data_epoch, event_id, consumer, routing_contract_revision, projection_generation, result, actual_version, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, application.DataEpoch, application.EventID, application.Consumer,
		application.RoutingContractRevision, application.ProjectionGeneration, application.Result, application.ActualVersion, completedAt)
	if err != nil {
		return errors.New("insert business application result")
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return errors.New("inspect application result")
	}
	if inserted == 0 || closedAt.Valid {
		return nil
	}
	retainUntil := completedAt.Add(RetentionPeriod)
	if _, err := executor.ExecContext(ctx, `
		UPDATE business_event_requirements SET closed_at=?, retain_until=?, updated_at=?
		WHERE data_epoch=? AND event_id=? AND consumer=? AND routing_contract_revision=? AND closed_at IS NULL`,
		completedAt, retainUntil, completedAt, application.DataEpoch, application.EventID, application.Consumer, application.RoutingContractRevision); err != nil {
		return errors.New("close business event requirement")
	}
	bytes := uint64(payloadBytes + RetentionMetadataBytes)
	if _, err := executor.ExecContext(ctx, `
		UPDATE business_async_budget
		SET open_requirements=CASE WHEN open_requirements > 0 THEN open_requirements-1 ELSE 0 END,
		    retained_groups=retained_groups+1, retained_bytes=retained_bytes+?, updated_at=?
		WHERE data_epoch=?`, bytes, completedAt, application.DataEpoch); err != nil {
		return errors.New("account closed business event requirement")
	}
	return nil
}

func (repository *Repository) FindApplication(ctx context.Context, application Application) (bool, error) {
	if err := validateApplication(application); err != nil {
		return false, err
	}
	var exists bool
	if err := repository.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM business_application_results WHERE data_epoch=? AND event_id=? AND consumer=? AND routing_contract_revision=? AND projection_generation=?)`, application.DataEpoch, application.EventID, application.Consumer, application.RoutingContractRevision, application.ProjectionGeneration).Scan(&exists); err != nil {
		return false, errors.New("check business application result")
	}
	return exists, nil
}

func (repository *Repository) Budget(ctx context.Context, epoch string) (Budget, error) {
	if err := validateEpoch(epoch); err != nil {
		return Budget{}, err
	}
	var budget Budget
	err := repository.database.QueryRowContext(ctx, `SELECT data_epoch, open_requirements, retained_groups, retained_bytes, parked_messages, parked_bytes, updated_at FROM business_async_budget WHERE data_epoch=?`, epoch).Scan(&budget.DataEpoch, &budget.OpenRequirements, &budget.RetainedGroups, &budget.RetainedBytes, &budget.ParkedMessages, &budget.ParkedBytes, &budget.UpdatedAt)
	if err != nil {
		return Budget{}, errors.New("read business async budget")
	}
	return budget, nil
}

// ReleaseRetentionTx is called by the Outbox cleanup transaction. It only
// releases counters for rows that are actually deleted and leaves historical
// completion evidence available for stage queries.
func (repository *Repository) ReleaseRetentionTx(ctx context.Context, executor Executor, eventIDs []string, now time.Time) error {
	if executor == nil {
		return fmt.Errorf("%w: transaction is required", ErrInvalidArgument)
	}
	for _, eventID := range eventIDs {
		if err := validateEventID(eventID); err != nil {
			return err
		}
		rows, err := executor.QueryContext(ctx, `
			SELECT data_epoch, payload_bytes FROM business_event_requirements
			WHERE event_id=? AND closed_at IS NOT NULL AND retain_until < ? AND retention_released=FALSE
			FOR UPDATE`, eventID, normalizeTime(now))
		if err != nil {
			return errors.New("select retained business requirement")
		}
		type released struct {
			epoch string
			bytes uint64
		}
		values := make([]released, 0)
		for rows.Next() {
			var epoch string
			var payload int
			if err := rows.Scan(&epoch, &payload); err != nil {
				_ = rows.Close()
				return errors.New("scan retained business requirement")
			}
			values = append(values, released{epoch: epoch, bytes: uint64(payload + RetentionMetadataBytes)})
		}
		if err := rows.Close(); err != nil {
			return errors.New("close retained business requirements")
		}
		for _, value := range values {
			if _, err := executor.ExecContext(ctx, `UPDATE business_event_requirements SET retention_released=TRUE, updated_at=? WHERE data_epoch=? AND event_id=? AND closed_at IS NOT NULL AND retain_until < ? AND retention_released=FALSE`, normalizeTime(now), value.epoch, eventID, normalizeTime(now)); err != nil {
				return errors.New("release business requirement retention")
			}
			if _, err := executor.ExecContext(ctx, `UPDATE business_async_budget SET retained_groups=CASE WHEN retained_groups > 0 THEN retained_groups-1 ELSE 0 END, retained_bytes=CASE WHEN retained_bytes >= ? THEN retained_bytes-? ELSE 0 END, updated_at=? WHERE data_epoch=?`, value.bytes, value.bytes, normalizeTime(now), value.epoch); err != nil {
				return errors.New("release business retention budget")
			}
		}
	}
	return nil
}
