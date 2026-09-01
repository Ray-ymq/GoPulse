package post

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("post not found")

type Repository interface {
	Create(context.Context, uint64, string, string) (Post, error)
	List(context.Context, uint64, ListOptions) ([]Post, error)
	FindPublicByID(context.Context, uint64) (PublicProjection, error)
	LikedByViewer(context.Context, uint64, uint64) (bool, error)
	Exists(context.Context, uint64) (bool, error)
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

const postPublicReadSelect = `
SELECT
    p.id,
    p.title,
    p.content,
    p.created_at,
    p.updated_at,
    u.id,
    u.username,
    (SELECT COUNT(*) FROM comments AS c WHERE c.post_id = p.id) AS comment_count,
    (SELECT COUNT(*) FROM post_likes AS likes WHERE likes.post_id = p.id) AS like_count
FROM posts AS p %s
INNER JOIN users AS u ON u.id = p.author_id`

const postListReadSelect = `
SELECT
    p.id,
    p.title,
    p.content,
    p.created_at,
    p.updated_at,
    u.id,
    u.username,
    (SELECT COUNT(*) FROM comments AS c WHERE c.post_id = p.id) AS comment_count,
    (SELECT COUNT(*) FROM post_likes AS likes WHERE likes.post_id = p.id) AS like_count,
    EXISTS(
        SELECT 1
        FROM post_likes AS my_like
        WHERE my_like.post_id = p.id AND my_like.user_id = ?
    ) AS liked_by_me
FROM posts AS p %s
INNER JOIN users AS u ON u.id = p.author_id`

func (repository *MySQLRepository) Create(ctx context.Context, authorID uint64, title, content string) (Post, error) {
	result, err := repository.database.ExecContext(ctx,
		`INSERT INTO posts (author_id, title, content) VALUES (?, ?, ?)`,
		authorID,
		title,
		content,
	)
	if err != nil {
		return Post{}, fmt.Errorf("create post: %w", err)
	}
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		return Post{}, errors.New("create post: invalid inserted identifier")
	}
	projection, err := repository.FindPublicByID(ctx, uint64(identifier))
	if err != nil {
		return Post{}, err
	}
	return projection.post(false), nil
}

func (repository *MySQLRepository) List(ctx context.Context, viewerID uint64, options ListOptions) ([]Post, error) {
	query, arguments := listStatement(viewerID, options)
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	defer rows.Close()

	posts := make([]Post, 0, options.Limit+1)
	for rows.Next() {
		record, err := scanPost(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("list posts: %w", err)
		}
		posts = append(posts, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	return posts, nil
}

func listStatement(viewerID uint64, options ListOptions) (string, []any) {
	query := fmt.Sprintf(postListReadSelect, "FORCE INDEX (idx_posts_created_at_id)")
	arguments := []any{viewerID}
	if options.Cursor != nil {
		query += `
WHERE p.created_at < ? OR (p.created_at = ? AND p.id < ?)`
		arguments = append(arguments, options.Cursor.CreatedAt, options.Cursor.CreatedAt, options.Cursor.ID)
	}
	query += `
ORDER BY p.created_at DESC, p.id DESC
LIMIT ?`
	arguments = append(arguments, options.Limit+1)
	return query, arguments
}

func (repository *MySQLRepository) FindPublicByID(ctx context.Context, postID uint64) (PublicProjection, error) {
	row := repository.database.QueryRowContext(ctx, fmt.Sprintf(postPublicReadSelect, "")+`
WHERE p.id = ?`, postID)
	projection, err := scanPublicProjection(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return PublicProjection{}, ErrNotFound
	}
	if err != nil {
		return PublicProjection{}, fmt.Errorf("find public post detail: %w", err)
	}
	return projection, nil
}

func (repository *MySQLRepository) LikedByViewer(ctx context.Context, postID, viewerID uint64) (bool, error) {
	var liked bool
	if err := repository.database.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM post_likes WHERE post_id = ? AND user_id = ?)`,
		postID,
		viewerID,
	).Scan(&liked); err != nil {
		return false, fmt.Errorf("check viewer post like: %w", err)
	}
	return liked, nil
}

// FindByID remains as a repository convenience for direct integration checks;
// application detail reads use the explicitly separated public and viewer queries.
func (repository *MySQLRepository) FindByID(ctx context.Context, postID, viewerID uint64) (Post, error) {
	projection, err := repository.FindPublicByID(ctx, postID)
	if err != nil {
		return Post{}, err
	}
	liked, err := repository.LikedByViewer(ctx, postID, viewerID)
	if err != nil {
		return Post{}, err
	}
	return projection.post(liked), nil
}

func (repository *MySQLRepository) Exists(ctx context.Context, postID uint64) (bool, error) {
	var exists bool
	if err := repository.database.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM posts WHERE id = ?)`,
		postID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check post existence: %w", err)
	}
	return exists, nil
}

type scanFunc func(...any) error

func scanPublicProjection(scan scanFunc) (PublicProjection, error) {
	var projection PublicProjection
	err := scan(
		&projection.ID,
		&projection.Title,
		&projection.Content,
		&projection.CreatedAt,
		&projection.UpdatedAt,
		&projection.Author.ID,
		&projection.Author.Username,
		&projection.CommentCount,
		&projection.LikeCount,
	)
	return projection, err
}

func scanPost(scan scanFunc) (Post, error) {
	var record Post
	err := scan(
		&record.ID,
		&record.Title,
		&record.Content,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.Author.ID,
		&record.Author.Username,
		&record.CommentCount,
		&record.LikeCount,
		&record.LikedByMe,
	)
	return record, err
}
