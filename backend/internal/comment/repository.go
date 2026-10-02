package comment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
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
	var record Comment
	err := platform.RunMySQLTransaction(ctx, func(attemptContext context.Context) (bool, error) {
		var commitAttempted bool
		var err error
		record, commitAttempted, err = repository.createTransactional(attemptContext, starter, postID, authorID, content)
		return !commitAttempted, err
	})
	return record, err
}

func (repository *MySQLRepository) createTransactional(ctx context.Context, starter transactionStarter, postID, authorID uint64, content string) (Comment, bool, error) {
	transaction, err := starter.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Comment{}, false, fmt.Errorf("begin comment transaction: %w", err)
	}
	defer transaction.Rollback()

	recipientID, err := post.FindAuthorIDForUpdate(ctx, transaction, postID)
	if errors.Is(err, post.ErrNotFound) {
		return Comment{}, false, post.ErrNotFound
	}
	if err != nil {
		return Comment{}, false, fmt.Errorf("lock comment recipient: %w", err)
	}

	result, err := transaction.ExecContext(ctx,
		`INSERT INTO comments (post_id, author_id, content) VALUES (?, ?, ?)`,
		postID,
		authorID,
		content,
	)
	if err != nil {
		return Comment{}, false, fmt.Errorf("create comment: %w", err)
	}
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		return Comment{}, false, errors.New("create comment: invalid inserted identifier")
	}

	record, err := findCommentByID(ctx, transaction, uint64(identifier))
	if err != nil {
		return Comment{}, false, err
	}
	eventID := ""
	if authorID != recipientID {
		event, eventErr := bus.NewCommentCreated(repository.now(), authorID, recipientID, postID, uint64(identifier))
		if eventErr != nil {
			return Comment{}, false, errors.New("create comment event")
		}
		eventID = event.EventID
		if err := repository.outbox.Insert(ctx, transaction, event); err != nil {
			return Comment{}, false, fmt.Errorf("create comment outbox event: %w", err)
		}
	}

	if err := transaction.Commit(); err != nil {
		// A lost COMMIT reply does not prove rollback. Read the exact identities
		// through a fresh connection; never replay a possibly committed INSERT.
		if ctx.Err() == nil {
			var committed bool
			checkErr := repository.database.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM comments AS c
    WHERE c.id = ? AND c.post_id = ? AND c.author_id = ?
      AND CAST(c.content AS BINARY) = CAST(? AS BINARY)
      AND (? = '' OR EXISTS(
          SELECT 1 FROM business_outbox AS o
          WHERE o.event_id = ? AND o.event_type = 'comment.created'
      ))
)`, record.ID, postID, authorID, content, eventID, eventID).Scan(&committed)
			if checkErr == nil && committed {
				return record, true, nil
			}
		}
		return Comment{}, true, fmt.Errorf("commit comment transaction: %w", err)
	}
	return record, true, nil
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
