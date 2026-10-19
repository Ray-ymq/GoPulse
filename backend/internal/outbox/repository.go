package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/instrumentation"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/tracing"
)

const (
	DefaultMaxClaimBatch = 50
	maximumOwnerBytes    = 128
	minimumLeaseDuration = time.Second
	maximumLeaseDuration = 10 * time.Minute
	maximumCleanupBatch  = 500
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

// MetadataWriter is implemented by the durable repository so the business
// transaction can attach the database-assigned Outbox ID to its commit span.
// Writer remains the compatibility seam for callers and focused tests that do
// not need that identifier.
type MetadataWriter interface {
	InsertWithMetadata(context.Context, Executor, bus.Envelope) (uint64, error)
}

type Clock func() time.Time
type Backoff func(attempt uint32) time.Duration

type Options struct {
	DatabaseTimeout     time.Duration
	Clock               Clock
	Backoff             Backoff
	MaxClaimBatch       int
	RetentionAccounting RetentionAccounting
	// EnableV2 upgrades non-command events inside the caller's transaction.
	// post.created is produced as v2 by the post command writer because it
	// carries a command identity; all other existing producers can share this
	// transaction-local upgrade path.
	EnableV2 bool
}

type RetentionAccounting interface {
	ReleaseRetentionTx(context.Context, businessstate.Executor, []string, time.Time) error
}

type Repository struct {
	databaseTimeout time.Duration
	claimTurn       atomic.Uint64
	cleanupMu       sync.Mutex
	cleanupAt       time.Time
	cleanupID       uint64
	cleanupMore     bool
	database        *sql.DB
	clock           Clock
	backoff         Backoff
	maxClaimBatch   int
	accounting      RetentionAccounting
	enableV2        bool
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
	dbTimeout := options.DatabaseTimeout
	if dbTimeout == 0 {
		dbTimeout = 2 * time.Second
	}
	if dbTimeout < 10*time.Millisecond || dbTimeout > 30*time.Second {
		return nil, fmt.Errorf("%w: database timeout", ErrInvalidArgument)
	}
	return &Repository{databaseTimeout: dbTimeout, database: database, clock: clock, backoff: backoff, maxClaimBatch: maxClaimBatch, accounting: options.RetentionAccounting, enableV2: options.EnableV2}, nil
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
	_, err := repository.InsertWithMetadata(ctx, executor, envelope)
	return err
}

// InsertWithMetadata writes a validated event and returns the database row ID.
// A driver that does not expose LastInsertId still completes the same atomic
// insert and returns zero; the durable event ID remains the business identity.
func (repository *Repository) InsertWithMetadata(ctx context.Context, executor Executor, envelope bus.Envelope) (uint64, error) {
	if executor == nil {
		return 0, fmt.Errorf("%w: executor is required", ErrInvalidArgument)
	}
	if repository.enableV2 && envelope.SchemaVersion == bus.SchemaVersionV1 {
		reader, ok := executor.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		if !ok {
			return 0, fmt.Errorf("%w: v2 event upgrade requires a transactional reader", ErrInvalidArgument)
		}
		var epoch, status, mode string
		if err := reader.QueryRowContext(ctx, `
			SELECT data_epoch, status, mode FROM business_environment
			WHERE singleton=1 FOR SHARE`).Scan(&epoch, &status, &mode); err != nil {
			return 0, fmt.Errorf("read business event activation: %w", err)
		}
		if status != businessstate.StatusReady || mode != businessstate.ModeV2 {
			return 0, errors.New("business event activation is not ready")
		}
		var err error
		envelope, err = envelope.WithDataEpoch(epoch)
		if err != nil {
			return 0, fmt.Errorf("%w: upgrade business event", ErrInvalidArgument)
		}
	}
	if repository.enableV2 && envelope.SchemaVersion == bus.SchemaVersionV2 {
		reader, ok := executor.(interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		})
		if !ok {
			return 0, fmt.Errorf("%w: v2 event validation requires a transactional reader", ErrInvalidArgument)
		}
		var epoch, status, mode string
		if err := reader.QueryRowContext(ctx, `
			SELECT data_epoch, status, mode FROM business_environment
			WHERE singleton=1 FOR SHARE`).Scan(&epoch, &status, &mode); err != nil {
			return 0, fmt.Errorf("read business event activation: %w", err)
		}
		if status != businessstate.StatusReady || mode != businessstate.ModeV2 || envelope.DataEpoch != epoch {
			return 0, businessstate.ErrEpochMismatch
		}
	}
	if ctx != nil {
		if wire := tracing.WireFromContext(ctx); wire.TraceParent != "" {
			envelope.SetTraceContext(wire.TraceParent, wire.TraceState)
		}
	}
	payload, err := bus.Encode(envelope)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid business event", ErrInvalidArgument)
	}
	now := repository.now()
	result, err := executor.ExecContext(ctx, `
		INSERT INTO business_outbox (
			event_id, event_type, schema_version, payload, status, available_at,
			attempt_count, lease_owner, lease_expires_at, published_at, last_error,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, 'pending', ?, 0, NULL, NULL, NULL, NULL, ?, ?)`,
		envelope.EventID, string(envelope.EventType), envelope.SchemaVersion, payload, now, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert business outbox event: %w", err)
	}
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		return 0, nil
	}
	return uint64(identifier), nil
}

// Claim alternates bounded time-ordered pending and expired-lease scans. The transaction ends before the
// records are returned, so callers never hold database locks while publishing.
func (repository *Repository) Claim(ctx context.Context, owner string, batch int, leaseDuration time.Duration) (claimed []Record, operationErr error) {
	started := time.Now()
	defer func() {
		if m := instrumentation.BackendActive(); m != nil {
			m.ObserveOutboxOperation("claim", time.Since(started), operationErr)
		}
	}()
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
		return nil, fmt.Errorf("begin outbox claim: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	records := make([]Record, 0, batch)
	branches := []struct{ status, order string }{{"pending", "available_at"}, {"leased", "lease_expires_at"}}
	if repository.claimTurn.Add(1)%2 == 0 {
		branches[0], branches[1] = branches[1], branches[0]
	}
	for _, branch := range branches {
		if len(records) == batch {
			break
		}
		rows, err := transaction.QueryContext(ctx, `SELECT id, event_id, event_type, schema_version, payload, status, available_at,
   attempt_count, lease_owner, lease_expires_at, published_at, last_error, created_at, updated_at
   FROM business_outbox WHERE status = ? AND `+branch.order+` <= ?
   ORDER BY `+branch.order+`, id LIMIT ? FOR UPDATE SKIP LOCKED`, branch.status, now, batch-len(records))
		if err != nil {
			return nil, fmt.Errorf("select claimable outbox events: %w", err)
		}
		for rows.Next() {
			record, err := scanRecord(rows)
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan claimable outbox event: %w", err)
			}
			records = append(records, record)
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return nil, fmt.Errorf("iterate claimable outbox events: %w", err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close claimable outbox events: %w", closeErr)
		}
	}
	if len(records) > 0 {
		args := []any{owner, leaseExpiresAt, now}
		for _, record := range records {
			args = append(args, record.ID)
		}
		_, err = transaction.ExecContext(ctx, `UPDATE business_outbox SET status='leased', lease_owner=?, lease_expires_at=?, updated_at=? WHERE id IN (`+strings.TrimRight(strings.Repeat("?,", len(records)), ",")+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("lease outbox events: %w", err)
		}
	}
	for index := range records {
		records[index].Status = StatusLeased
		records[index].LeaseOwner = owner
		records[index].LeaseExpiresAt = timePointer(leaseExpiresAt)
		records[index].UpdatedAt = now
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit outbox claim: %w", err)
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
		return fmt.Errorf("mark outbox event published: %w", err)
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
		return fmt.Errorf("begin failed outbox release: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

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
		return fmt.Errorf("lock failed outbox event: %w", err)
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
		return fmt.Errorf("release failed outbox event: %w", err)
	}
	if err := requireChangedLease(result); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit failed outbox release: %w", err)
	}
	return nil
}

func (repository *Repository) CleanupPublished(ctx context.Context, publishedBefore time.Time, batch int) (deletedCount int64, operationErr error) {
	started := time.Now()
	candidateCount := 0
	defer func() {
		if m := instrumentation.BackendActive(); m != nil {
			m.ObserveOutboxOperation("cleanup", time.Since(started), operationErr)
			m.ObserveCleanupRows(candidateCount, int(deletedCount))
		}
	}()
	if publishedBefore.IsZero() {
		return 0, fmt.Errorf("%w: published cutoff is required", ErrInvalidArgument)
	}
	if batch < 1 || batch > maximumCleanupBatch {
		return 0, fmt.Errorf("%w: cleanup batch must be between 1 and %d", ErrInvalidArgument, maximumCleanupBatch)
	}
	repository.cleanupMu.Lock()
	defer repository.cleanupMu.Unlock()
	candidates, err := repository.database.QueryContext(ctx, `SELECT id, published_at FROM business_outbox
 WHERE status='published' AND published_at < ? AND (published_at > ? OR (published_at = ? AND id > ?))
 ORDER BY published_at, id LIMIT ?`, publishedBefore.UTC(), repository.cleanupAt, repository.cleanupAt, repository.cleanupID, batch)
	if err != nil {
		return 0, fmt.Errorf("scan cleanup candidates: %w", err)
	}
	candidateIDs := []uint64{}
	nextAt, nextID := repository.cleanupAt, repository.cleanupID
	for candidates.Next() {
		if err := candidates.Scan(&nextID, &nextAt); err != nil {
			candidates.Close()
			return 0, fmt.Errorf("read cleanup candidate: %w", err)
		}
		candidateIDs = append(candidateIDs, nextID)
	}
	err = candidates.Err()
	closeErr := candidates.Close()
	if err != nil {
		return 0, fmt.Errorf("iterate cleanup candidates: %w", err)
	}
	if closeErr != nil {
		return 0, fmt.Errorf("close cleanup candidates: %w", closeErr)
	}
	candidateCount = len(candidateIDs)
	if len(candidateIDs) == 0 {
		repository.cleanupAt = time.Time{}
		repository.cleanupID = 0
		repository.cleanupMore = false
		return 0, nil
	}
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin cleanup published outbox events: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
		SELECT o.id, o.event_id
		FROM business_outbox AS o
		WHERE o.id IN (`+strings.TrimRight(strings.Repeat("?,", len(candidateIDs)), ",")+`) AND o.status = 'published' AND o.published_at < ?
		  AND NOT EXISTS (
			SELECT 1 FROM business_event_requirements AS r
			WHERE r.event_id = o.event_id
			  AND (r.closed_at IS NULL OR r.retain_until IS NULL OR r.retain_until >= ?)
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM business_commands AS c
			WHERE c.event_id = o.event_id
			  AND (c.retained_until IS NULL OR c.retained_until >= ?)
		  )
		ORDER BY o.published_at, o.id
		FOR UPDATE`, append(idsToAny(candidateIDs), publishedBefore.UTC(), publishedBefore.UTC(), publishedBefore.UTC())...)
	if err != nil {
		return 0, fmt.Errorf("select cleanup published outbox events: %w", err)
	}
	ids := make([]uint64, 0, batch)
	eventIDs := make([]string, 0, batch)
	for rows.Next() {
		var id uint64
		var eventID string
		if err := rows.Scan(&id, &eventID); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan cleanup published outbox event: %w", err)
		}
		ids = append(ids, id)
		eventIDs = append(eventIDs, eventID)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close cleanup published outbox events: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate cleanup published outbox events: %w", err)
	}
	if len(ids) == 0 {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("commit empty outbox cleanup: %w", err)
		}
		repository.cleanupAt, repository.cleanupID = nextAt, nextID
		repository.cleanupMore = len(candidateIDs) == batch
		if !repository.cleanupMore {
			repository.cleanupAt = time.Time{}
			repository.cleanupID = 0
		}
		return 0, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	arguments := make([]any, 0, len(ids)+2)
	arguments = append(arguments, idsToAny(ids)...)
	arguments = append(arguments, StatusPublished, publishedBefore.UTC())
	result, err := tx.ExecContext(ctx, `DELETE FROM business_outbox WHERE id IN (`+placeholders+`) AND status=? AND published_at < ?`, arguments...)
	if err != nil {
		return 0, fmt.Errorf("delete cleanup published outbox events: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count cleaned outbox events: %w", err)
	}
	if deleted != int64(len(ids)) {
		return 0, errors.New("cleanup deleted count differs from locked candidates")
	}
	if repository.accounting != nil && deleted > 0 {
		if err := repository.accounting.ReleaseRetentionTx(ctx, tx, eventIDs, repository.now()); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit cleanup published outbox events: %w", err)
	}
	repository.cleanupAt, repository.cleanupID = nextAt, nextID
	repository.cleanupMore = len(candidateIDs) == batch
	if !repository.cleanupMore {
		repository.cleanupAt = time.Time{}
		repository.cleanupID = 0
	}
	return deleted, nil
}

func idsToAny(ids []uint64) []any {
	values := make([]any, len(ids))
	for index, id := range ids {
		values[index] = id
	}
	return values
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
		return fmt.Errorf("inspect outbox lease transition: %w", err)
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

// CleanupHasMore reports candidate exhaustion, independently of protected rows.
func (repository *Repository) CleanupHasMore() bool {
	repository.cleanupMu.Lock()
	defer repository.cleanupMu.Unlock()
	return repository.cleanupMore
}

// MarkPublishedBatch returns one lease result per input ID. All transitions commit
// together; a storage error leaves every input eligible for at-least-once retry.
func (repository *Repository) MarkPublishedBatch(ctx context.Context, ids []uint64, owner string) ([]error, error) {
	if err := validateOwner(owner); err != nil {
		return nil, err
	}
	if len(ids) > DefaultMaxClaimBatch {
		return nil, ErrInvalidArgument
	}
	results := make([]error, len(ids))
	if len(ids) == 0 {
		return results, nil
	}
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin batch acknowledgement: %w", err)
	}
	defer tx.Rollback()
	now := repository.now()
	args := []any{owner, now}
	args = append(args, idsToAny(ids)...)
	rows, err := tx.QueryContext(ctx, `SELECT id FROM business_outbox WHERE status='leased' AND lease_owner=? AND lease_expires_at > ? AND id IN (`+strings.TrimRight(strings.Repeat("?,", len(ids)), ",")+`) FOR UPDATE`, args...)
	if err != nil {
		return nil, fmt.Errorf("lock acknowledgements: %w", err)
	}
	current := map[uint64]bool{}
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		current[id] = true
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	active := []uint64{}
	for i, id := range ids {
		if current[id] {
			active = append(active, id)
		} else {
			results[i] = ErrLeaseLost
		}
	}
	if len(active) > 0 {
		args = []any{now, now}
		args = append(args, idsToAny(active)...)
		if _, err = tx.ExecContext(ctx, `UPDATE business_outbox SET status='published',lease_owner=NULL,lease_expires_at=NULL,published_at=?,last_error=NULL,updated_at=? WHERE id IN (`+strings.TrimRight(strings.Repeat("?,", len(active)), ",")+`)`, args...); err != nil {
			return nil, fmt.Errorf("acknowledge batch: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit acknowledgements: %w", err)
	}
	return results, nil
}
