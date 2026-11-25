package post

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Ray-ymq/GoPulse/backend/internal/platform/readcache"
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
    %s,
    EXISTS(
        SELECT 1
        FROM post_likes AS my_like
        WHERE my_like.post_id = p.id AND my_like.user_id = ?
    ) AS liked_by_me,
    EXISTS(SELECT 1 FROM user_follows f WHERE f.follower_id = ? AND f.followed_id = u.id) AS following,
    EXISTS(SELECT 1 FROM post_bookmarks b WHERE b.user_id = ? AND b.post_id = p.id) AS bookmarked_by_me,
    %s AS bookmark_created_at%s
FROM posts AS p %s
INNER JOIN users AS u ON u.id = p.author_id`

const postCountColumns = "(SELECT COUNT(*) FROM comments c WHERE c.post_id=p.id), (SELECT COUNT(*) FROM post_likes l WHERE l.post_id=p.id)"

func countColumns(epoch string) string {
	if epoch != "" {
		return "0, 0"
	}
	return postCountColumns
}

func (repository *MySQLRepository) List(ctx context.Context, viewerID uint64, options ListOptions) ([]Post, error) {
	if repository.discovery != nil && options.Limit == DefaultLimit && options.Cursor == nil && !options.Bookmarks && !options.Following && options.AuthorID == 0 {
		return repository.listDiscovery(ctx, viewerID, options)
	}
	return repository.listFromMySQL(ctx, viewerID, options)
}

func (repository *MySQLRepository) listFromMySQL(ctx context.Context, viewerID uint64, options ListOptions) ([]Post, error) {
	query := func(cached bool) (string, []any) { return listStatement(viewerID, options, cached) }
	records, err := repository.readPosts(ctx, options.Limit+1, query)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	return records, nil
}

const readyEpochColumn = ", COALESCE((SELECT data_epoch FROM business_runtime_state WHERE singleton=1 AND status='ready'), '')"

func postListSelect(cached bool, timestamp, join string) string {
	if cached {
		return fmt.Sprintf(postListReadSelect, "0, 0", timestamp, readyEpochColumn, join)
	}
	return fmt.Sprintf(postListReadSelect, postCountColumns, timestamp, "", join)
}

func listStatement(viewerID uint64, options ListOptions, cached bool) (string, []any) {
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
		query := postListSelect(cached, timestamp, "INNER JOIN ("+page+") AS feed_page ON feed_page.id=p.id") + order
		return query, arguments
	}
	query := postListSelect(cached, timestamp, join) + filter + order + " LIMIT ?"
	return query, arguments
}

// FindMany hydrates one bounded ordered set of search hits from MySQL. The SQL
// query is set based; the requested Elasticsearch order is restored in Go.
func (repository *MySQLRepository) FindMany(ctx context.Context, viewerID uint64, identifiers []uint64) ([]Post, error) {
	if viewerID == 0 || len(identifiers) == 0 || len(identifiers) > MaximumLimit {
		return nil, errors.New("find many posts: invalid arguments")
	}
	placeholders := make([]string, len(identifiers))
	arguments := make([]any, 0, len(identifiers)+3)
	arguments = append(arguments, viewerID, viewerID, viewerID)
	for index, identifier := range identifiers {
		if identifier == 0 {
			return nil, errors.New("find many posts: invalid identifier")
		}
		placeholders[index] = "?"
		arguments = append(arguments, identifier)
	}
	query := func(cached bool) (string, []any) {
		return postListSelect(cached, "p.created_at", "") + `
WHERE p.id IN (` + strings.Join(placeholders, ",") + `)`, arguments
	}
	records, err := repository.readPosts(ctx, len(identifiers), query)
	if err != nil {
		return nil, fmt.Errorf("find many posts: %w", err)
	}
	byID := make(map[uint64]Post, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}
	ordered := make([]Post, 0, len(identifiers))
	for _, identifier := range identifiers {
		if record, ok := byID[identifier]; ok {
			ordered = append(ordered, record)
		}
	}
	return ordered, nil
}

func (repository *MySQLRepository) readPosts(ctx context.Context, capacity int, query func(bool) (string, []any)) ([]Post, error) {
	if repository.counts == nil {
		records, _, err := repository.queryPosts(ctx, capacity, query, false)
		return records, err
	}
	records, epoch, err := repository.queryPosts(ctx, capacity, query, true)
	if err != nil || len(records) == 0 {
		return records, err
	}
	if expected := readcache.RequestEpoch(ctx); expected != "" && epoch != expected {
		return nil, ErrReadContextChanged
	}
	if epoch == "" {
		records, _, err := repository.queryPosts(ctx, capacity, query, false)
		return records, err
	}
	return records, repository.hydrateCounts(ctx, epoch, records)
}

func (repository *MySQLRepository) queryPosts(ctx context.Context, capacity int, statement func(bool) (string, []any), cached bool) ([]Post, string, error) {
	query, arguments := statement(cached)
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rows.Close() }()
	records := make([]Post, 0, capacity)
	var epoch string
	var extra []any
	if cached {
		extra = []any{&epoch}
	}
	for rows.Next() {
		record, err := scanPost(rows.Scan, extra...)
		if err != nil {
			return nil, "", err
		}
		records = append(records, record)
	}
	return records, epoch, rows.Err()
}
