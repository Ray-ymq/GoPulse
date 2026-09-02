package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/go-sql-driver/mysql"
)

// Repository persists notifications and uses source_event_id as the final
// idempotency boundary for at-least-once message delivery.
type Repository struct {
	database *sql.DB
}

func NewRepository(database *sql.DB) (*Repository, error) {
	if database == nil {
		return nil, fmt.Errorf("%w: database is required", ErrInvalidArgument)
	}
	return &Repository{database: database}, nil
}

// Insert stores a notification transactionally. created is false when the
// event was already handled; duplicate delivery is a successful outcome.
func (repository *Repository) Insert(ctx context.Context, envelope bus.Envelope) (created bool, err error) {
	if ctx == nil {
		return false, fmt.Errorf("%w: context is required", ErrInvalidArgument)
	}
	if err := envelope.Validate(); err != nil {
		return false, fmt.Errorf("%w: invalid business event", ErrInvalidArgument)
	}
	if envelope.ActorID == envelope.RecipientID {
		return false, nil
	}

	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, errors.New("begin notification transaction")
	}
	defer transaction.Rollback()

	_, err = transaction.ExecContext(ctx, `
		INSERT INTO notifications
			(source_event_id, type, recipient_id, actor_id, post_id, comment_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		envelope.EventID, string(envelope.EventType), envelope.RecipientID,
		envelope.ActorID, envelope.PostID, envelope.CommentID, envelope.OccurredAt.UTC(),
	)
	if isDuplicateEntry(err) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("insert notification")
	}
	if err := transaction.Commit(); err != nil {
		return false, errors.New("commit notification transaction")
	}
	return true, nil
}

func (repository *Repository) FindBySourceEventID(ctx context.Context, eventID string) (Notification, error) {
	var record Notification
	var eventType string
	var commentID sql.NullInt64
	var readAt sql.NullTime
	err := repository.database.QueryRowContext(ctx, `
		SELECT id, source_event_id, type, recipient_id, actor_id, post_id, comment_id, created_at, read_at
		FROM notifications WHERE source_event_id = ?`, eventID).Scan(
		&record.ID, &record.SourceEventID, &eventType, &record.RecipientID, &record.ActorID,
		&record.PostID, &commentID, &record.CreatedAt, &readAt,
	)
	if err != nil {
		return Notification{}, err
	}
	record.Type = bus.EventType(eventType)
	if commentID.Valid {
		value := uint64(commentID.Int64)
		record.CommentID = &value
	}
	if readAt.Valid {
		value := readAt.Time
		record.ReadAt = &value
	}
	return record, nil
}

func isDuplicateEntry(err error) bool {
	var mysqlError *mysql.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}

// ListByRecipient returns at most limit+1 notifications in strict descending
// keyset order so the service can determine whether another page exists.
func (repository *Repository) ListByRecipient(ctx context.Context, recipientID uint64, options ListOptions) ([]Public, error) {
	arguments := []any{recipientID}
	cursorClause := ""
	if options.Cursor != nil {
		cursorClause = " AND (n.created_at < ? OR (n.created_at = ? AND n.id < ?))"
		arguments = append(arguments, options.Cursor.CreatedAt.UTC(), options.Cursor.CreatedAt.UTC(), options.Cursor.ID)
	}
	arguments = append(arguments, options.Limit+1)
	rows, err := repository.database.QueryContext(ctx, `
		SELECT n.id, n.type, n.created_at, n.read_at,
		       u.id, u.username, n.post_id, n.comment_id
		FROM notifications n
		JOIN users u ON u.id = n.actor_id
		WHERE n.recipient_id = ?`+cursorClause+`
		ORDER BY n.created_at DESC, n.id DESC
		LIMIT ?`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list recipient notifications: %w", err)
	}
	defer rows.Close()

	records := make([]Public, 0, options.Limit+1)
	for rows.Next() {
		var record Public
		var eventType string
		var commentID sql.NullInt64
		var readAt sql.NullTime
		if err := rows.Scan(
			&record.ID, &eventType, &record.CreatedAt, &readAt,
			&record.Actor.ID, &record.Actor.Username, &record.PostID, &commentID,
		); err != nil {
			return nil, fmt.Errorf("scan recipient notification: %w", err)
		}
		record.Type = bus.EventType(eventType)
		if readAt.Valid {
			value := readAt.Time
			record.ReadAt = &value
		}
		if commentID.Valid {
			value := uint64(commentID.Int64)
			record.CommentID = &value
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recipient notifications: %w", err)
	}
	return records, nil
}

// MarkRead is recipient-scoped and idempotent. COALESCE preserves the original
// read timestamp on repeated requests.
func (repository *Repository) MarkRead(ctx context.Context, recipientID, notificationID uint64) error {
	result, err := repository.database.ExecContext(ctx, `
		UPDATE notifications
		SET read_at = COALESCE(read_at, CURRENT_TIMESTAMP(6))
		WHERE id = ? AND recipient_id = ?`, notificationID, recipientID)
	if err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read notification update result: %w", err)
	}
	if affected > 0 {
		return nil
	}
	var exists bool
	if err := repository.database.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM notifications WHERE id = ? AND recipient_id = ?)`,
		notificationID, recipientID,
	).Scan(&exists); err != nil {
		return fmt.Errorf("check notification after idempotent read: %w", err)
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}
