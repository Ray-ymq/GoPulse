package outbox

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
	DefaultMaxClaimBatch = 100
	maximumOwnerBytes    = 128
	minimumLeaseDuration = time.Second
	maximumLeaseDuration = 10 * time.Minute
	maximumCleanupBatch  = 1000
	maximumBackoff       = 5 * time.Minute
)

type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Writer is the minimal transactional boundary required by business fact
// repositories. Implementations must write the event with the Executor passed
// by the caller, which is normally the caller's active *sql.Tx.
type Writer interface {
	Insert(context.Context, Executor, bus.Envelope) error
}

type Clock func() time.Time
type Backoff func(attempt uint32) time.Duration

type Options struct {
	Clock         Clock
	Backoff       Backoff
	MaxClaimBatch int
}

type Repository struct {
	database      *sql.DB
	clock         Clock
	backoff       Backoff
	maxClaimBatch int
}

func NewRepository(database *sql.DB, options Options) (*Repository, error) {
	if database == nil {
		return nil, fmt.Errorf("%w: database is required", ErrInvalidArgument)
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	backoff := options.Backoff
	if backoff == nil {
		backoff = ExponentialBackoff
	}
	maxClaimBatch := options.MaxClaimBatch
	if maxClaimBatch == 0 {
		maxClaimBatch = DefaultMaxClaimBatch
	}
	if maxClaimBatch < 1 || maxClaimBatch > DefaultMaxClaimBatch {
		return nil, fmt.Errorf("%w: max claim batch must be between 1 and %d", ErrInvalidArgument, DefaultMaxClaimBatch)
	}
	return &Repository{database: database, clock: clock, backoff: backoff, maxClaimBatch: maxClaimBatch}, nil
}

func ExponentialBackoff(attempt uint32) time.Duration {
	if attempt == 0 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 8 {
		shift = 8
	}
	delay := time.Second * time.Duration(1<<shift)
	if delay > maximumBackoff {
		return maximumBackoff
	}
	return delay
}

// Insert writes a validated event through either *sql.DB or *sql.Tx. Callers
// that need atomic core-fact and Outbox writes must pass their active transaction.
func (repository *Repository) Insert(ctx context.Context, executor Executor, envelope bus.Envelope) error {
	if executor == nil {
		return fmt.Errorf("%w: executor is required", ErrInvalidArgument)
	}
	payload, err := bus.Encode(envelope)
	if err != nil {
		return fmt.Errorf("%w: invalid business event", ErrInvalidArgument)
	}
	now := repository.now()
	_, err = executor.ExecContext(ctx, `
		INSERT INTO business_outbox (
			event_id, event_type, schema_version, payload, status, available_at,
			attempt_count, lease_owner, lease_expires_at, published_at, last_error,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, 'pending', ?, 0, NULL, NULL, NULL, NULL, ?, ?)`,
		envelope.EventID, string(envelope.EventType), envelope.SchemaVersion, payload, now, now, now,
	)
	if err != nil {
		return errors.New("insert business outbox event")
	}
	return nil
}

// Claim leases a bounded, ID-ordered batch. The transaction ends before the
// records are returned, so callers never hold database locks while publishing.
func (repository *Repository) Claim(ctx context.Context, owner string, batch int, leaseDuration time.Duration) ([]Record, error) {
	if err := validateOwner(owner); err != nil {
		return nil, err
	}
	if batch < 1 || batch > repository.maxClaimBatch {
		return nil, fmt.Errorf("%w: claim batch must be between 1 and %d", ErrInvalidArgument, repository.maxClaimBatch)
	}
	if leaseDuration < minimumLeaseDuration || leaseDuration > maximumLeaseDuration {
		return nil, fmt.Errorf("%w: lease duration must be between %s and %s", ErrInvalidArgument, minimumLeaseDuration, maximumLeaseDuration)
	}

	now := repository.now()
	leaseExpiresAt := now.Add(leaseDuration)
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, errors.New("begin outbox claim")
	}
	defer transaction.Rollback()

	rows, err := transaction.QueryContext(ctx, `
		SELECT id, event_id, event_type, schema_version, payload, status, available_at,
		       attempt_count, lease_owner, lease_expires_at, published_at, last_error,
		       created_at, updated_at
		FROM business_outbox
		WHERE (status = 'pending' AND available_at <= ?)
		   OR (status = 'leased' AND lease_expires_at <= ?)
		ORDER BY id
		LIMIT ?
		FOR UPDATE SKIP LOCKED`, now, now, batch)
	if err != nil {
		return nil, errors.New("select claimable outbox events")
	}

	records := make([]Record, 0, batch)
	for rows.Next() {
		record, scanErr := scanRecord(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, errors.New("scan claimable outbox event")
		}
		records = append(records, record)
	}
	if err := rows.Close(); err != nil {
		return nil, errors.New("close claimable outbox events")
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("iterate claimable outbox events")
	}

	for index := range records {
		if _, err := transaction.ExecContext(ctx, `
			UPDATE business_outbox
			SET status = 'leased', lease_owner = ?, lease_expires_at = ?, updated_at = ?
			WHERE id = ?`, owner, leaseExpiresAt, now, records[index].ID); err != nil {
			return nil, errors.New("lease outbox event")
		}
		records[index].Status = StatusLeased
		records[index].LeaseOwner = owner
		records[index].LeaseExpiresAt = timePointer(leaseExpiresAt)
		records[index].UpdatedAt = now
	}
	if err := transaction.Commit(); err != nil {
		return nil, errors.New("commit outbox claim")
	}
	return records, nil
}

func (repository *Repository) MarkPublished(ctx context.Context, id uint64, owner string) error {
	if id == 0 {
		return fmt.Errorf("%w: outbox ID must be positive", ErrInvalidArgument)
	}
	if err := validateOwner(owner); err != nil {
		return err
	}
	now := repository.now()
	result, err := repository.database.ExecContext(ctx, `
		UPDATE business_outbox
		SET status = 'published', lease_owner = NULL, lease_expires_at = NULL,
		    published_at = ?, last_error = NULL, updated_at = ?
		WHERE id = ? AND status = 'leased' AND lease_owner = ? AND lease_expires_at > ?`,
		now, now, id, owner, now)
	if err != nil {
		return errors.New("mark outbox event published")
	}
	return requireChangedLease(result)
}

func (repository *Repository) ReleaseFailed(ctx context.Context, id uint64, owner string, failure FailureCode) error {
	if id == 0 {
		return fmt.Errorf("%w: outbox ID must be positive", ErrInvalidArgument)
	}
	if err := validateOwner(owner); err != nil {
		return err
	}
	if !validFailureCode(failure) {
		return fmt.Errorf("%w: failure code is unsupported", ErrInvalidArgument)
	}
	now := repository.now()
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return errors.New("begin failed outbox release")
	}
	defer transaction.Rollback()

	var attemptCount uint32
	err = transaction.QueryRowContext(ctx, `
		SELECT attempt_count
		FROM business_outbox
		WHERE id = ? AND status = 'leased' AND lease_owner = ? AND lease_expires_at > ?
		FOR UPDATE`, id, owner, now).Scan(&attemptCount)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLeaseLost
	}
	if err != nil {
		return errors.New("lock failed outbox event")
	}

	nextAttempt := attemptCount + 1
	delay := repository.backoff(nextAttempt)
	if delay < 0 || delay > maximumBackoff {
		return fmt.Errorf("%w: backoff must be between 0 and %s", ErrInvalidArgument, maximumBackoff)
	}
	availableAt := now.Add(delay)
	result, err := transaction.ExecContext(ctx, `
		UPDATE business_outbox
		SET status = 'pending', available_at = ?, attempt_count = ?,
		    lease_owner = NULL, lease_expires_at = NULL, last_error = ?, updated_at = ?
		WHERE id = ? AND status = 'leased' AND lease_owner = ? AND lease_expires_at > ?`,
		availableAt, nextAttempt, string(failure), now, id, owner, now)
	if err != nil {
		return errors.New("release failed outbox event")
	}
	if err := requireChangedLease(result); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return errors.New("commit failed outbox release")
	}
	return nil
}

func (repository *Repository) CleanupPublished(ctx context.Context, publishedBefore time.Time, batch int) (int64, error) {
	if publishedBefore.IsZero() {
		return 0, fmt.Errorf("%w: published cutoff is required", ErrInvalidArgument)
	}
	if batch < 1 || batch > maximumCleanupBatch {
		return 0, fmt.Errorf("%w: cleanup batch must be between 1 and %d", ErrInvalidArgument, maximumCleanupBatch)
	}
	result, err := repository.database.ExecContext(ctx, `
		DELETE FROM business_outbox
		WHERE id IN (
			SELECT id FROM (
				SELECT id
				FROM business_outbox
				WHERE status = 'published' AND published_at < ?
				ORDER BY id
				LIMIT ?
			) AS expired_published
		)`, publishedBefore.UTC(), batch)
	if err != nil {
		return 0, errors.New("cleanup published outbox events")
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, errors.New("count cleaned outbox events")
	}
	return deleted, nil
}

func (repository *Repository) now() time.Time {
	return repository.clock().UTC().Truncate(time.Microsecond)
}

type rowScanner interface {
	Scan(...any) error
}

func scanRecord(scanner rowScanner) (Record, error) {
	var record Record
	var eventType string
	var status string
	var leaseOwner sql.NullString
	var leaseExpiresAt sql.NullTime
	var publishedAt sql.NullTime
	var lastError sql.NullString
	if err := scanner.Scan(
		&record.ID, &record.EventID, &eventType, &record.SchemaVersion, &record.Payload,
		&status, &record.AvailableAt, &record.AttemptCount, &leaseOwner, &leaseExpiresAt,
		&publishedAt, &lastError, &record.CreatedAt, &record.UpdatedAt,
	); err != nil {
		return Record{}, err
	}
	record.EventType = bus.EventType(eventType)
	record.Status = Status(status)
	if leaseOwner.Valid {
		record.LeaseOwner = leaseOwner.String
	}
	if leaseExpiresAt.Valid {
		record.LeaseExpiresAt = timePointer(leaseExpiresAt.Time)
	}
	if publishedAt.Valid {
		record.PublishedAt = timePointer(publishedAt.Time)
	}
	if lastError.Valid {
		record.LastError = lastError.String
	}
	return record, nil
}

func requireChangedLease(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return errors.New("inspect outbox lease transition")
	}
	if changed != 1 {
		return ErrLeaseLost
	}
	return nil
}

func validateOwner(owner string) error {
	if owner == "" || strings.TrimSpace(owner) != owner || len(owner) > maximumOwnerBytes {
		return fmt.Errorf("%w: lease owner must be 1-%d trimmed bytes", ErrInvalidArgument, maximumOwnerBytes)
	}
	for index := range owner {
		character := owner[index]
		if character < 0x21 || character > 0x7e {
			return fmt.Errorf("%w: lease owner must contain printable ASCII", ErrInvalidArgument)
		}
	}
	return nil
}

func timePointer(value time.Time) *time.Time {
	return &value
}
