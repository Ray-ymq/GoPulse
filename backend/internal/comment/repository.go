package comment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

type MySQLRepository struct {
	database database
}

func NewMySQLRepository(database database) *MySQLRepository {
	return &MySQLRepository{database: database}
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
	row := repository.database.QueryRowContext(ctx, commentReadSelect+`
WHERE c.id = ?`, commentID)
	record, err := scanComment(row.Scan)
	if err != nil {
		return Comment{}, fmt.Errorf("find created comment: %w", err)
	}
	return record, nil
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
