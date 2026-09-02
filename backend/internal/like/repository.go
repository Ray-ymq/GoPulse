package like

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/go-sql-driver/mysql"
)

var ErrAlreadyExists = errors.New("post like already exists")

type Repository interface {
	Create(context.Context, uint64, uint64) error
	Delete(context.Context, uint64, uint64) error
	Exists(context.Context, uint64, uint64) (bool, error)
}

type database interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type transactionStarter interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// RepositoryOptions controls the optional event written with a newly created
// like. A nil Outbox preserves the synchronous repository behavior.
type RepositoryOptions struct {
	Outbox outbox.Writer
	Clock  func() time.Time
}

type MySQLRepository struct {
	database database
	outbox   outbox.Writer
	clock    func() time.Time
}

func NewMySQLRepository(database database, options ...RepositoryOptions) *MySQLRepository {
	repository := &MySQLRepository{database: database, clock: time.Now}
	if len(options) > 0 {
		repository.outbox = options[0].Outbox
		if options[0].Clock != nil {
			repository.clock = options[0].Clock
		}
	}
	return repository
}

// NewMySQLRepositoryWithOutbox constructs the transactional repository used by
// the Backend application while keeping the existing one-argument constructor
// useful for synchronous tests and tooling.
func NewMySQLRepositoryWithOutbox(database database, eventOutbox outbox.Writer) *MySQLRepository {
	return NewMySQLRepository(database, RepositoryOptions{Outbox: eventOutbox})
}

func (repository *MySQLRepository) Create(ctx context.Context, postID, userID uint64) error {
	if repository.outbox == nil {
		return repository.createWithoutEvent(ctx, postID, userID)
	}

	starter, ok := repository.database.(transactionStarter)
	if !ok {
		return errors.New("create post like: database does not support transactions")
	}
	transaction, err := starter.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return errors.New("begin post like transaction")
	}
	defer transaction.Rollback()

	recipientID, err := post.FindAuthorIDForUpdate(ctx, transaction, postID)
	if errors.Is(err, post.ErrNotFound) {
		return post.ErrNotFound
	}
	if err != nil {
		return errors.New("lock like recipient")
	}

	if _, err := transaction.ExecContext(ctx,
		`INSERT INTO post_likes (post_id, user_id) VALUES (?, ?)`,
		postID,
		userID,
	); err != nil {
		if isDuplicateEntry(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("create post like: %w", err)
	}

	if userID != recipientID {
		event, eventErr := bus.NewPostLiked(repository.now(), userID, recipientID, postID)
		if eventErr != nil {
			return errors.New("create post like event")
		}
		if err := repository.outbox.Insert(ctx, transaction, event); err != nil {
			return errors.New("create post like outbox event")
		}
	}

	if err := transaction.Commit(); err != nil {
		return errors.New("commit post like transaction")
	}
	return nil
}

func (repository *MySQLRepository) createWithoutEvent(ctx context.Context, postID, userID uint64) error {
	_, err := repository.database.ExecContext(ctx,
		`INSERT INTO post_likes (post_id, user_id) VALUES (?, ?)`,
		postID,
		userID,
	)
	if err == nil {
		return nil
	}
	if isDuplicateEntry(err) {
		return ErrAlreadyExists
	}
	return fmt.Errorf("create post like: %w", err)
}

func (repository *MySQLRepository) Delete(ctx context.Context, postID, userID uint64) error {
	if _, err := repository.database.ExecContext(ctx,
		`DELETE FROM post_likes WHERE post_id = ? AND user_id = ?`,
		postID,
		userID,
	); err != nil {
		return fmt.Errorf("delete post like: %w", err)
	}
	return nil
}

func (repository *MySQLRepository) Exists(ctx context.Context, postID, userID uint64) (bool, error) {
	var exists bool
	if err := repository.database.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM post_likes WHERE post_id = ? AND user_id = ?)`,
		postID,
		userID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check post like: %w", err)
	}
	return exists, nil
}

func (repository *MySQLRepository) now() time.Time {
	return repository.clock().UTC()
}

func isDuplicateEntry(err error) bool {
	var mysqlError *mysql.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}
