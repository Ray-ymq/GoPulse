package businessstate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	PostCreateCommandType = "post.create"
	DigestVersionV1       = 1
	RetryWindow           = RetentionPeriod
)

// Command is the durable, minimal result of a business command. It never
// contains a copy of the user supplied title or body.
type Command struct {
	DataEpoch          string
	CommandID          string
	UserID             uint64
	CommandType        string
	IdempotencyKeyHash []byte
	RequestDigest      []byte
	PostID             uint64
	EventID            string
	Status             string
	CommittedAt        time.Time
	RetainedUntil      time.Time
}

// CommandContext is the public input needed to create one retryable command.
type CommandContext struct {
	DataEpoch        string
	RetryWindow      time.Duration
	RetryWindowUntil time.Time
}

// CommandReservation describes the rows which a post-create transaction must
// reserve before it commits its fact and Outbox event.
type CommandReservation struct {
	Command            Command
	RequestDigest      []byte
	IdempotencyKeyHash []byte
	Requirement        Requirement
	PayloadBytes       int
	Now                time.Time
}

// Hash returns a detached SHA-256 digest. Callers use this for both the
// idempotency key and the normalized request digest; the raw values never
// enter the database or a response.
func Hash(value []byte) []byte {
	digest := sha256.Sum256(value)
	return append([]byte(nil), digest[:]...)
}

// LockCurrentTx shares the durable environment row with other fact writers. All command/fact
// transactions use this before reading the current epoch so a restore cannot
// race a newly committed event.
func (repository *Repository) LockCurrentTx(ctx context.Context, executor Executor) (Environment, error) {
	if repository == nil || executor == nil || ctx == nil {
		return Environment{}, fmt.Errorf("%w: environment transaction is invalid", ErrInvalidArgument)
	}
	return scanEnvironment(executor.QueryRowContext(ctx, `
		SELECT data_epoch, status, mode, v1_compatibility_enabled, activation_revision,
		       COALESCE(restore_operation_id, ''), created_at, updated_at
		FROM business_environment WHERE singleton = 1 FOR SHARE`))
}

// ReadCommandContext returns the current ready epoch and the bounded retry
// window. It deliberately exposes no private activation or budget settings.
func (repository *Repository) ReadCommandContext(ctx context.Context) (CommandContext, error) {
	environment, err := repository.Current(ctx)
	if err != nil {
		return CommandContext{}, err
	}
	if environment.Status != StatusReady {
		return CommandContext{}, ErrNotReady
	}
	now := repository.now()
	return CommandContext{DataEpoch: environment.DataEpoch, RetryWindow: RetryWindow, RetryWindowUntil: now.Add(RetryWindow)}, nil
}

func validateCommand(command Command) error {
	if err := validateEpoch(command.DataEpoch); err != nil {
		return err
	}
	if err := validateEventID(command.CommandID); err != nil {
		return fmt.Errorf("%w: command ID is invalid", ErrInvalidArgument)
	}
	if command.UserID == 0 || command.CommandType == "" {
		return fmt.Errorf("%w: command identity is invalid", ErrInvalidArgument)
	}
	if len(command.RequestDigest) != sha256.Size || (command.IdempotencyKeyHash != nil && len(command.IdempotencyKeyHash) != sha256.Size) {
		return fmt.Errorf("%w: command digest is invalid", ErrInvalidArgument)
	}
	if command.EventID != "" {
		if err := validateEventID(command.EventID); err != nil {
			return err
		}
	}
	if command.Status != "" && command.Status != "succeeded" && command.Status != "failed" && command.Status != "unknown" {
		return fmt.Errorf("%w: command status is invalid", ErrInvalidArgument)
	}
	return nil
}

func (repository *Repository) FindCommandTx(ctx context.Context, executor Executor, epoch string, userID uint64, keyHash []byte, now time.Time) (Command, bool, error) {
	if executor == nil || ctx == nil || userID == 0 || len(keyHash) != sha256.Size {
		return Command{}, false, fmt.Errorf("%w: command lookup is invalid", ErrInvalidArgument)
	}
	if err := validateEpoch(epoch); err != nil {
		return Command{}, false, err
	}
	command, found, err := scanCommand(executor.QueryRowContext(ctx, `
		SELECT data_epoch, command_id, user_id, command_type, idempotency_key_hash,
		       request_digest, COALESCE(post_id, 0), COALESCE(event_id, ''), status,
		       committed_at, retained_until
		FROM business_commands
		WHERE data_epoch=? AND user_id=? AND command_type=? AND idempotency_key_hash=?
		LIMIT 1 FOR UPDATE`, epoch, userID, PostCreateCommandType, keyHash))
	if errors.Is(err, sql.ErrNoRows) {
		return Command{}, false, nil
	}
	return command, found, err
}

func (repository *Repository) FindCommand(ctx context.Context, epoch string, userID uint64, keyHash []byte, now time.Time) (Command, bool, error) {
	if repository == nil || repository.database == nil {
		return Command{}, false, fmt.Errorf("%w: repository is unavailable", ErrInvalidArgument)
	}
	return repository.findCommand(ctx, repository.database, epoch, userID, keyHash, now)
}

func (repository *Repository) findCommand(ctx context.Context, executor Executor, epoch string, userID uint64, keyHash []byte, now time.Time) (Command, bool, error) {
	if executor == nil || ctx == nil || userID == 0 || len(keyHash) != sha256.Size {
		return Command{}, false, fmt.Errorf("%w: command lookup is invalid", ErrInvalidArgument)
	}
	if err := validateEpoch(epoch); err != nil {
		return Command{}, false, err
	}
	command, found, err := scanCommand(executor.QueryRowContext(ctx, `
		SELECT data_epoch, command_id, user_id, command_type, idempotency_key_hash,
		       request_digest, COALESCE(post_id, 0), COALESCE(event_id, ''), status,
		       committed_at, retained_until
		FROM business_commands
		WHERE data_epoch=? AND user_id=? AND command_type=? AND idempotency_key_hash=?
		  AND (retained_until IS NULL OR retained_until > ?)
		LIMIT 1`, epoch, userID, PostCreateCommandType, keyHash, normalizeTime(now)))
	if errors.Is(err, sql.ErrNoRows) {
		return Command{}, false, nil
	}
	if err != nil {
		return Command{}, false, errors.New("read business command")
	}
	return command, found, nil
}

func scanCommand(row *sql.Row) (Command, bool, error) {
	var command Command
	var keyHash []byte
	var digest []byte
	var committedAt, retainedUntil sql.NullTime
	if err := row.Scan(&command.DataEpoch, &command.CommandID, &command.UserID, &command.CommandType, &keyHash, &digest, &command.PostID, &command.EventID, &command.Status, &committedAt, &retainedUntil); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Command{}, false, sql.ErrNoRows
		}
		return Command{}, false, err
	}
	command.IdempotencyKeyHash = append([]byte(nil), keyHash...)
	command.RequestDigest = append([]byte(nil), digest...)
	if committedAt.Valid {
		command.CommittedAt = committedAt.Time
	}
	if retainedUntil.Valid {
		command.RetainedUntil = retainedUntil.Time
	}
	return command, true, nil
}

// ReservePostCreateTx atomically reserves the open obligation and retained
// command/event budget, then writes the command and search requirement. It is
// intentionally transaction-only: the post fact and Outbox row are written by
// the caller in the same transaction.
func (repository *Repository) ReservePostCreateTx(ctx context.Context, executor Executor, reservation CommandReservation) error {
	if repository == nil || executor == nil || ctx == nil {
		return fmt.Errorf("%w: command reservation is invalid", ErrInvalidArgument)
	}
	if err := validateCommand(reservation.Command); err != nil {
		return err
	}
	if len(reservation.RequestDigest) != sha256.Size || len(reservation.IdempotencyKeyHash) != 0 && len(reservation.IdempotencyKeyHash) != sha256.Size {
		return fmt.Errorf("%w: command reservation digest is invalid", ErrInvalidArgument)
	}
	if err := validateRequirement(reservation.Requirement); err != nil {
		return err
	}
	if reservation.Requirement.DataEpoch != reservation.Command.DataEpoch || reservation.Requirement.EventID != reservation.Command.EventID {
		return fmt.Errorf("%w: command and requirement identities differ", ErrInvalidArgument)
	}
	if reservation.PayloadBytes < 1 || reservation.PayloadBytes > busMaxMessageBytes() {
		return fmt.Errorf("%w: command payload size is invalid", ErrInvalidArgument)
	}
	now := normalizeTime(reservation.Now)
	if _, err := executor.ExecContext(ctx, `INSERT INTO business_async_budget (data_epoch) VALUES (?) ON DUPLICATE KEY UPDATE data_epoch=VALUES(data_epoch)`, reservation.Command.DataEpoch); err != nil {
		return errors.New("initialize command budget")
	}
	rows, err := executor.QueryContext(ctx, `
		SELECT data_epoch, open_requirements, retained_groups, retained_bytes
		FROM business_async_budget ORDER BY data_epoch FOR UPDATE`)
	if err != nil {
		return errors.New("lock command budgets")
	}
	var currentOpen, totalGroups, totalBytes uint64
	foundCurrent := false
	for rows.Next() {
		var epoch string
		var open, groups, bytes uint64
		if err := rows.Scan(&epoch, &open, &groups, &bytes); err != nil {
			_ = rows.Close()
			return errors.New("scan command budgets")
		}
		if epoch == reservation.Command.DataEpoch {
			foundCurrent = true
			currentOpen = open
		}
		totalGroups += groups
		totalBytes += bytes
	}
	if err := rows.Close(); err != nil {
		return errors.New("close command budgets")
	}
	if !foundCurrent || currentOpen >= repository.BudgetLimits().OpenRequirements || totalGroups >= MaximumRetainedGroups || totalBytes > MaximumRetainedBytes-uint64(reservation.PayloadBytes+RetentionMetadataBytes) {
		return ErrBudgetExceeded
	}
	if _, err := executor.ExecContext(ctx, `
		INSERT INTO business_commands
			(data_epoch, command_id, user_id, command_type, idempotency_key_hash, request_digest,
			 post_id, event_id, status, committed_at, retained_until, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'succeeded', ?, ?, ?, ?)`,
		reservation.Command.DataEpoch, reservation.Command.CommandID, reservation.Command.UserID, reservation.Command.CommandType,
		reservation.IdempotencyKeyHash, reservation.RequestDigest, reservation.Command.PostID, reservation.Command.EventID,
		now, now.Add(RetryWindow), now, now); err != nil {
		if reservation.Command.IdempotencyKeyHash != nil && isCommandIdempotencyDuplicate(err) {
			return ErrCommandAlreadyExists
		}
		return errors.New("insert business command")
	}
	if _, err := executor.ExecContext(ctx, `
		INSERT INTO business_event_requirements
			(data_epoch, event_id, consumer, routing_contract_revision, applicable_object_version, payload_bytes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, reservation.Requirement.DataEpoch, reservation.Requirement.EventID,
		reservation.Requirement.Consumer, reservation.Requirement.RoutingContractRevision, reservation.Requirement.ApplicableObjectVersion,
		reservation.PayloadBytes, now, now); err != nil {
		return errors.New("insert command event requirement")
	}
	if _, err := executor.ExecContext(ctx, `
		UPDATE business_async_budget
		SET open_requirements=open_requirements+1,
		    retained_groups=retained_groups+1,
		    retained_bytes=retained_bytes+?, updated_at=?
		WHERE data_epoch=?`, reservation.PayloadBytes+RetentionMetadataBytes, now, reservation.Command.DataEpoch); err != nil {
		return errors.New("reserve command budget")
	}
	return nil
}

func isCommandIdempotencyDuplicate(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 && strings.Contains(mysqlErr.Message, "uq_business_command_idempotency")
}

// busMaxMessageBytes is kept local to avoid exporting another mutable-looking
// budget constant from businessstate's public API.
func busMaxMessageBytes() int { return 16 * 1024 }
