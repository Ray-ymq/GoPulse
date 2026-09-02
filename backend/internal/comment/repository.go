package comment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

type Repository interface {
	Create(context.Context, uint64, uint64, string) (Comment, error)
	List(context.Context, uint64, ListOptions) ([]Comment, error)
}

type database interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type transactionStarter interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// RepositoryOptions controls the optional asynchronous event side effect. A
// nil Outbox preserves the pre-Phase-02-02 synchronous repository behavior,
// which is useful for read-only tests and migration tooling.
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
// the Backend application. The separate helper keeps the one-argument
// constructor compatible with existing read and migration tests.
func NewMySQLRepositoryWithOutbox(database database, eventOutbox outbox.Writer) *MySQLRepository {
	return NewMySQLRepository(database, RepositoryOptions{Outbox: eventOutbox})
}

const commentReadSelect = `
SELECT
    c.id,
    c.post_id,
    c.content,
    c.created_at,
    u.id,
    u.username
FROM comments AS c
INNER JOIN users AS u ON u.id = c.author_id`

func (repository *MySQLRepository) Create(ctx context.Context, postID, authorID uint64, content string) (Comment, error) {
	if repository.outbox == nil {
		return repository.createWithoutEvent(ctx, postID, authorID, content)
	}

	starter, ok := repository.database.(transactionStarter)
	if !ok {
		return Comment{}, errors.New("create comment: database does not support transactions")
	}
	transaction, err := starter.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Comment{}, errors.New("begin comment transaction")
	}
	defer transaction.Rollback()

	recipientID, err := post.FindAuthorIDForUpdate(ctx, transaction, postID)
	if errors.Is(err, post.ErrNotFound) {
		return Comment{}, post.ErrNotFound
	}
	if err != nil {
		return Comment{}, errors.New("lock comment recipient")
	}

	result, err := transaction.ExecContext(ctx,
		`INSERT INTO comments (post_id, author_id, content) VALUES (?, ?, ?)`,
		postID,
		authorID,
		content,
	)
	if err != nil {
		return Comment{}, fmt.Errorf("create comment: %w", err)
	}
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		return Comment{}, errors.New("create comment: invalid inserted identifier")
	}

	record, err := findCommentByID(ctx, transaction, uint64(identifier))
	if err != nil {
		return Comment{}, err
	}
	if authorID != recipientID {
		event, eventErr := bus.NewCommentCreated(repository.now(), authorID, recipientID, postID, uint64(identifier))
		if eventErr != nil {
			return Comment{}, errors.New("create comment event")
		}
		if err := repository.outbox.Insert(ctx, transaction, event); err != nil {
			return Comment{}, errors.New("create comment outbox event")
		}
	}

	if err := transaction.Commit(); err != nil {
		return Comment{}, errors.New("commit comment transaction")
	}
	return record, nil
}

func (repository *MySQLRepository) createWithoutEvent(ctx context.Context, postID, authorID uint64, content string) (Comment, error) {
	result, err := repository.database.ExecContext(ctx,
		`INSERT INTO comments (post_id, author_id, content) VALUES (?, ?, ?)`,
		postID,
		authorID,
		content,
	)
	if err != nil {
		return Comment{}, fmt.Errorf("create comment: %w", err)
	}
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		return Comment{}, errors.New("create comment: invalid inserted identifier")
	}
	return repository.findByID(ctx, uint64(identifier))
}

func (repository *MySQLRepository) List(ctx context.Context, postID uint64, options ListOptions) ([]Comment, error) {
	query, arguments := listStatement(postID, options)
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()

	comments := make([]Comment, 0, options.Limit+1)
	for rows.Next() {
		record, err := scanComment(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("list comments: %w", err)
		}
		comments = append(comments, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	return comments, nil
}

func listStatement(postID uint64, options ListOptions) (string, []any) {
	query := commentReadSelect + `
WHERE c.post_id = ?`
	arguments := []any{postID}
	if options.Cursor != nil {
		query += ` AND c.id < ?`
		arguments = append(arguments, options.Cursor.ID)
	}
	query += `
ORDER BY c.id DESC
LIMIT ?`
	arguments = append(arguments, options.Limit+1)
	return query, arguments
}

func (repository *MySQLRepository) findByID(ctx context.Context, commentID uint64) (Comment, error) {
	return findCommentByID(ctx, repository.database, commentID)
}

func findCommentByID(ctx context.Context, database interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, commentID uint64) (Comment, error) {
	row := database.QueryRowContext(ctx, commentReadSelect+`
WHERE c.id = ?`, commentID)
	record, err := scanComment(row.Scan)
	if err != nil {
		return Comment{}, fmt.Errorf("find created comment: %w", err)
	}
	return record, nil
}

func (repository *MySQLRepository) now() time.Time {
	return repository.clock().UTC()
}

type scanFunc func(...any) error

func scanComment(scan scanFunc) (Comment, error) {
	var record Comment
	err := scan(
		&record.ID,
		&record.PostID,
		&record.Content,
		&record.CreatedAt,
		&record.Author.ID,
		&record.Author.Username,
	)
	return record, err
}
