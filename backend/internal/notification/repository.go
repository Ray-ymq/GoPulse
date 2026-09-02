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
