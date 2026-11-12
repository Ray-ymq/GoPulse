package post

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const postListReadSelect = `
SELECT
    p.id,
    p.title,
    p.content,
    p.created_at,
    p.updated_at,
    p.edited_at,
    p.content_revision,
    u.id,
    u.username,
    u.display_name,
    (SELECT COUNT(*) FROM comments AS c WHERE c.post_id = p.id) AS comment_count,
    (SELECT COUNT(*) FROM post_likes AS likes WHERE likes.post_id = p.id) AS like_count,
    EXISTS(
        SELECT 1
        FROM post_likes AS my_like
        WHERE my_like.post_id = p.id AND my_like.user_id = ?
    ) AS liked_by_me,
    EXISTS(SELECT 1 FROM user_follows f WHERE f.follower_id = ? AND f.followed_id = u.id) AS following,
    EXISTS(SELECT 1 FROM post_bookmarks b WHERE b.user_id = ? AND b.post_id = p.id) AS bookmarked_by_me,
    %s AS bookmark_created_at
FROM posts AS p %s
INNER JOIN users AS u ON u.id = p.author_id`

func (repository *MySQLRepository) List(ctx context.Context, viewerID uint64, options ListOptions) ([]Post, error) {
	query, arguments := listStatement(viewerID, options)
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
	timestamp := "p.created_at"
	join := ""
	if !options.Bookmarks && !options.Following && options.AuthorID == 0 {
		join = "FORCE INDEX (idx_posts_created_at_id)"
	}
	arguments := []any{viewerID, viewerID, viewerID}
	if options.Bookmarks {
		timestamp = "b.created_at"
		join = "INNER JOIN post_bookmarks b ON b.post_id = p.id AND b.user_id = ?"
		arguments = append(arguments, viewerID)
	}
	if options.Following {
		join += " INNER JOIN (SELECT followed_id FROM user_follows WHERE follower_id=?) AS feed_authors ON feed_authors.followed_id=p.author_id"
		arguments = append(arguments, viewerID)
	}
	filter := " WHERE 1=1"
	if options.AuthorID != 0 {
		filter += " AND p.author_id = ?"
		arguments = append(arguments, options.AuthorID)
	}
	if options.Cursor != nil {
		filter += fmt.Sprintf(" AND (%s < ? OR (%s = ? AND p.id < ?))", timestamp, timestamp)
		arguments = append(arguments, options.Cursor.CreatedAt, options.Cursor.CreatedAt, options.Cursor.ID)
	}
	order := " ORDER BY " + timestamp + " DESC, p.id DESC"
	arguments = append(arguments, options.Limit+1)
	if options.Following && !options.Bookmarks {
		page := "SELECT p.id FROM posts AS p " + join + filter + order + " LIMIT ?"
		query := fmt.Sprintf(postListReadSelect, timestamp, "INNER JOIN ("+page+") AS feed_page ON feed_page.id=p.id") + order
		return query, arguments
	}
	query := fmt.Sprintf(postListReadSelect, timestamp, join) + filter + order + " LIMIT ?"
	return query, arguments
}

// FindMany hydrates one bounded ordered set of search hits from MySQL. The SQL
// query is set based; the requested Elasticsearch order is restored in Go.
func (repository *MySQLRepository) FindMany(ctx context.Context, viewerID uint64, identifiers []uint64) ([]Post, error) {
	if viewerID == 0 || len(identifiers) == 0 || len(identifiers) > MaximumLimit {
		return nil, errors.New("find many posts: invalid arguments")
	}
	placeholders := make([]string, len(identifiers))
	arguments := make([]any, 0, len(identifiers)+2)
	arguments = append(arguments, viewerID, viewerID, viewerID)
	for index, identifier := range identifiers {
		if identifier == 0 {
			return nil, errors.New("find many posts: invalid identifier")
		}
		placeholders[index] = "?"
		arguments = append(arguments, identifier)
	}
	query := fmt.Sprintf(postListReadSelect, "p.created_at", "") + `
WHERE p.id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("find many posts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byID := make(map[uint64]Post, len(identifiers))
	for rows.Next() {
		record, err := scanPost(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("find many posts: %w", err)
		}
		byID[record.ID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("find many posts: %w", err)
	}
	ordered := make([]Post, 0, len(identifiers))
	for _, identifier := range identifiers {
		record, ok := byID[identifier]
		if !ok {
			continue
		}
		ordered = append(ordered, record)
	}
	return ordered, nil
}
