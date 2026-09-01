package like

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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

type MySQLRepository struct {
	database database
}

func NewMySQLRepository(database database) *MySQLRepository {
	return &MySQLRepository{database: database}
}

func (repository *MySQLRepository) Create(ctx context.Context, postID, userID uint64) error {
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

func isDuplicateEntry(err error) bool {
	var mysqlError *mysql.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}
