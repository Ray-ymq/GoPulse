package businessstate

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// Ensure creates the initial environment exactly once. The migration seeds a
// row for a clean deployment, while this method also supports a test database
// that only has the migration structure and concurrent first starts.
func (repository *Repository) Ensure(ctx context.Context) (Environment, error) {
	if repository == nil || repository.database == nil {
		return Environment{}, fmt.Errorf("%w: repository is unavailable", ErrInvalidArgument)
	}
	if ctx == nil {
		return Environment{}, fmt.Errorf("%w: context is required", ErrInvalidArgument)
	}
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Environment{}, errors.New("begin business environment initialization")
	}
	defer func() { _ = tx.Rollback() }()
	epoch, err := newUUID()
	if err != nil {
		return Environment{}, errors.New("generate business data epoch")
	}
	now := repository.now()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO business_environment
			(singleton, data_epoch, status, mode, v1_compatibility_enabled, activation_revision, created_at, updated_at)
		VALUES (1, ?, 'ready', 'legacy_v1', TRUE, 'phase-24-01', ?, ?)
		ON DUPLICATE KEY UPDATE singleton = VALUES(singleton)`, epoch, now, now); err != nil {
		return Environment{}, errors.New("initialize business environment")
	}
	var current string
	if err := tx.QueryRowContext(ctx, `SELECT data_epoch FROM business_environment WHERE singleton = 1 FOR UPDATE`).Scan(&current); err != nil {
		return Environment{}, errors.New("read initialized business environment")
	}
	if err := ensureBudgetTx(ctx, tx, current); err != nil {
		return Environment{}, err
	}
	if err := tx.Commit(); err != nil {
		return Environment{}, errors.New("commit business environment initialization")
	}
	return repository.Current(ctx)
}

func (repository *Repository) Current(ctx context.Context) (Environment, error) {
	if repository == nil || repository.database == nil {
		return Environment{}, fmt.Errorf("%w: repository is unavailable", ErrInvalidArgument)
	}
	if ctx == nil {
		return Environment{}, fmt.Errorf("%w: context is required", ErrInvalidArgument)
	}
	return scanEnvironment(repository.database.QueryRowContext(ctx, `
		SELECT data_epoch, status, mode, v1_compatibility_enabled, activation_revision,
		       COALESCE(restore_operation_id, ''), created_at, updated_at
		FROM business_environment WHERE singleton = 1`))
}

func (repository *Repository) CurrentEpoch(ctx context.Context) (string, error) {
	environment, err := repository.Current(ctx)
	if err != nil {
		return "", err
	}
	return environment.DataEpoch, nil
}

func scanEnvironment(row *sql.Row) (Environment, error) {
	var environment Environment
	if err := row.Scan(
		&environment.DataEpoch, &environment.Status, &environment.Mode, &environment.V1Compatibility,
		&environment.ActivationRevision, &environment.RestoreOperationID, &environment.CreatedAt, &environment.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Environment{}, ErrNotReady
		}
		return Environment{}, errors.New("read business environment")
	}
	if validateEpoch(environment.DataEpoch) != nil || !validMode(environment.Mode) || (environment.Status != StatusReady && environment.Status != StatusRestoring) {
		return Environment{}, errors.New("business environment is invalid")
	}
	return environment, nil
}

func (repository *Repository) SetMode(ctx context.Context, mode string, revision string, v1Compatibility bool) error {
	if !validMode(mode) || revision == "" || len(revision) > 64 {
		return fmt.Errorf("%w: environment mode is invalid", ErrInvalidArgument)
	}
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return errors.New("begin business mode update")
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE business_environment SET mode=?, activation_revision=?, v1_compatibility_enabled=?, updated_at=? WHERE singleton=1`, mode, revision, v1Compatibility, repository.now()); err != nil {
		return errors.New("update business mode")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("commit business mode update")
	}
	return nil
}

// BeginRestore changes the epoch only after the caller proves the old epoch.
// Historical budget rows are deliberately left untouched.
func (repository *Repository) BeginRestore(ctx context.Context, operationID, expectedOldEpoch, newEpoch string) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return fmt.Errorf("%w: restore arguments are invalid", ErrInvalidArgument)
	}
	if err := validateEpoch(expectedOldEpoch); err != nil {
		return err
	}
	if err := validateEpoch(newEpoch); err != nil || newEpoch == expectedOldEpoch {
		return fmt.Errorf("%w: restore epoch is invalid", ErrInvalidArgument)
	}
	if len(operationID) != 64 || !isLowerHex(operationID) {
		return fmt.Errorf("%w: restore operation is invalid", ErrInvalidArgument)
	}
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return errors.New("begin business restore")
	}
	defer func() { _ = tx.Rollback() }()
	var current string
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT data_epoch, status FROM business_environment WHERE singleton=1 FOR UPDATE`).Scan(&current, &status); err != nil {
		return errors.New("read business restore environment")
	}
	if current != expectedOldEpoch || status != StatusReady {
		return ErrEpochMismatch
	}
	now := repository.now()
	if _, err := tx.ExecContext(ctx, `UPDATE business_environment SET data_epoch=?, status='restoring', restore_operation_id=?, updated_at=? WHERE singleton=1`, newEpoch, operationID, now); err != nil {
		return errors.New("set business environment restoring")
	}
	if err := ensureBudgetTx(ctx, tx, newEpoch); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.New("commit business restore")
	}
	return nil
}

func (repository *Repository) MarkReady(ctx context.Context, operationID, epoch string) error {
	if repository == nil || repository.database == nil || ctx == nil {
		return fmt.Errorf("%w: ready arguments are invalid", ErrInvalidArgument)
	}
	if len(operationID) != 64 || !isLowerHex(operationID) {
		return fmt.Errorf("%w: restore operation is invalid", ErrInvalidArgument)
	}
	if err := validateEpoch(epoch); err != nil {
		return err
	}
	result, err := repository.database.ExecContext(ctx, `
		UPDATE business_environment SET status='ready', restore_operation_id=NULL, updated_at=?
		WHERE singleton=1 AND status='restoring' AND data_epoch=? AND restore_operation_id=?`, repository.now(), epoch, operationID)
	if err != nil {
		return errors.New("mark business environment ready")
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return ErrNotReady
	}
	return nil
}

func ensureBudgetTx(ctx context.Context, executor Executor, epoch string) error {
	if err := validateEpoch(epoch); err != nil {
		return err
	}
	if _, err := executor.ExecContext(ctx, `INSERT INTO business_async_budget (data_epoch) VALUES (?) ON DUPLICATE KEY UPDATE data_epoch=VALUES(data_epoch)`, epoch); err != nil {
		return errors.New("initialize business async budget")
	}
	return nil
}

func newUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return string(encoded), nil
}

func isLowerHex(value string) bool {
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return value != ""
}
