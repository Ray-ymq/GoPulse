package bookmark

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/go-sql-driver/mysql"
)

var ErrAlreadyExists = errors.New("post bookmark already exists")

type Repository interface {
	Create(context.Context, uint64, uint64) error
	Delete(context.Context, uint64, uint64) error
	Exists(context.Context, uint64, uint64) (bool, error)
}

type database interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type MySQLRepository struct{ database database }

func NewMySQLRepository(database database) *MySQLRepository {
	return &MySQLRepository{database: database}
}

func (repository *MySQLRepository) Create(ctx context.Context, postID, userID uint64) error {
	return repository.apply(ctx, postID, userID, true)
}
func (repository *MySQLRepository) Delete(ctx context.Context, postID, userID uint64) error {
	return repository.apply(ctx, postID, userID, false)
}

// Lock the parent so concurrent permanent deletion cannot race the existence check.
// No outbox or cache work belongs to this private relationship transaction.
func (repository *MySQLRepository) apply(ctx context.Context, postID, userID uint64, enabled bool) error {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := post.FindAuthorIDForUpdate(ctx, tx, postID); err != nil {
		return err
	}
	query := `DELETE FROM post_bookmarks WHERE post_id = ? AND user_id = ?`
	if enabled {
		query = `INSERT INTO post_bookmarks (post_id, user_id) VALUES (?, ?)`
	}
	if _, err := tx.ExecContext(ctx, query, postID, userID); err != nil {
		if enabled && isDuplicateEntry(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("write bookmark: %w", err)
	}
	return tx.Commit()
}

func (repository *MySQLRepository) Exists(ctx context.Context, postID, userID uint64) (bool, error) {
	var exists bool
	if err := repository.database.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM post_bookmarks WHERE post_id = ? AND user_id = ?)`,
		postID,
		userID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check post bookmark: %w", err)
	}
	return exists, nil
}

func isDuplicateEntry(err error) bool {
	var mysqlError *mysql.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}
