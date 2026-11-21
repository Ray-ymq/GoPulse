package post

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/businessstate"
)

const postPublicReadSelect = `
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
    (SELECT COUNT(*) FROM post_likes AS likes WHERE likes.post_id = p.id) AS like_count
FROM posts AS p %s
INNER JOIN users AS u ON u.id = p.author_id`

func (repository *MySQLRepository) FindPublicByID(ctx context.Context, postID uint64) (PublicProjection, error) {
	return findPublicByID(ctx, repository.database, postID)
}

// ReadContext obtains the epoch, visibility, owner and content revision that
// define one immutable body lookup. The production assembly supplies the
// business-state repository so the epoch read uses the same authoritative
// database connection as the post fact.
func (repository *MySQLRepository) ReadContext(ctx context.Context, postID uint64) (readContext ReadContext, err error) {

	if repository == nil || repository.database == nil || postID == 0 {
		return ReadContext{}, errors.New("read post context: invalid argument")
	}
	var epoch, status string
	var authorID, revision sql.NullInt64
	err = repository.database.QueryRowContext(ctx, `
SELECT e.data_epoch, e.status, p.author_id, p.content_revision
FROM business_runtime_state AS e
LEFT JOIN posts AS p ON p.id=?
WHERE e.singleton=1`, postID).Scan(&epoch, &status, &authorID, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return ReadContext{}, errors.New("business environment is not ready")
	}
	if err != nil {
		return ReadContext{}, fmt.Errorf("read post context: %w", err)
	}
	if status != businessstate.StatusReady {
		return ReadContext{}, ErrReadContextChanged
	}
	if businessstate.ValidateEpoch(epoch) != nil {
		return ReadContext{}, errors.New("business environment epoch is invalid")
	}
	if !authorID.Valid || !revision.Valid || authorID.Int64 <= 0 || revision.Int64 <= 0 {
		return ReadContext{}, ErrNotFound
	}
	readContext = ReadContext{Epoch: epoch, PostID: postID, AuthorID: uint64(authorID.Int64), ContentRevision: uint64(revision.Int64), Visible: true, ReadAt: repository.clock().UTC()}
	return readContext, nil
}

// FindBody is version constrained. A revision mismatch is not silently
// rebound to the current row; the caller must start a new request read.
func (repository *MySQLRepository) FindBody(ctx context.Context, key BodyKey) (body Body, err error) {

	if repository == nil || repository.database == nil || !key.Valid() {
		return Body{}, errors.New("find post body: invalid argument")
	}
	var epoch, status string
	var id, author, revision sql.NullInt64
	var title, content sql.NullString
	var created, updated, edited sql.NullTime
	err = repository.database.QueryRowContext(ctx, `
SELECT e.data_epoch, e.status, p.id, p.author_id, p.title, p.content,
       p.created_at, p.updated_at, p.edited_at, p.content_revision
FROM business_runtime_state AS e
LEFT JOIN posts AS p ON p.id=? AND p.content_revision=?
WHERE e.singleton=1`, key.PostID, key.ContentRevision).Scan(
		&epoch, &status, &id, &author, &title, &content, &created, &updated, &edited, &revision,
	)
	if err != nil {
		return Body{}, fmt.Errorf("find post body: %w", err)
	}
	if epoch != key.Epoch || status != businessstate.StatusReady {
		return Body{}, ErrReadContextChanged
	}
	if !id.Valid {
		var currentRevision uint64
		checkErr := repository.database.QueryRowContext(ctx, `SELECT content_revision FROM posts WHERE id=?`, key.PostID).Scan(&currentRevision)
		if errors.Is(checkErr, sql.ErrNoRows) {
			return Body{}, ErrNotFound
		}
		if checkErr != nil {
			return Body{}, fmt.Errorf("find post body revision: %w", checkErr)
		}
		return Body{}, ErrReadContextChanged
	}
	body = Body{ID: uint64(id.Int64), AuthorID: uint64(author.Int64), Title: title.String, Content: content.String,
		CreatedAt: created.Time, UpdatedAt: updated.Time, ContentRevision: uint64(revision.Int64)}
	if edited.Valid {
		body.EditedAt = &edited.Time
	}
	if !body.ValidForKey(key) {
		return Body{}, ErrReadContextChanged
	}
	return body, nil
}

// FindDynamic keeps mutable author data and interaction counts on the
// authoritative path. Both counts are read by one SQL statement so a detail
// response never turns a failed count into a fabricated zero.
func (repository *MySQLRepository) FindDynamic(ctx context.Context, postID, authorID uint64) (dynamic DynamicProjection, err error) {

	if repository == nil || repository.database == nil || postID == 0 || authorID == 0 {
		return DynamicProjection{}, errors.New("find post dynamic data: invalid argument")
	}
	err = repository.database.QueryRowContext(ctx, `
SELECT u.id, u.username, u.display_name,
       (SELECT COUNT(*) FROM comments AS c WHERE c.post_id=p.id),
       (SELECT COUNT(*) FROM post_likes AS likes WHERE likes.post_id=p.id)
FROM posts AS p INNER JOIN users AS u ON u.id=p.author_id
WHERE p.id=? AND p.author_id=?`, postID, authorID).Scan(
		&dynamic.Author.ID, &dynamic.Author.Username, &dynamic.Author.DisplayName, &dynamic.CommentCount, &dynamic.LikeCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DynamicProjection{}, ErrNotFound
	}
	if err != nil {
		return DynamicProjection{}, fmt.Errorf("find post dynamic data: %w", err)
	}
	return dynamic, nil
}

func findPublicByID(ctx context.Context, database database, postID uint64) (PublicProjection, error) {
	row := database.QueryRowContext(ctx, fmt.Sprintf(postPublicReadSelect, "")+`
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

func (repository *MySQLRepository) LikedByViewer(ctx context.Context, postID, viewerID uint64) (liked bool, err error) {

	if viewerID == 0 {
		return false, nil
	}
	if err = repository.database.QueryRowContext(ctx,
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
	records := []Post{projection.post(liked)}
	if err := repository.HydrateBookmarks(ctx, records, viewerID); err != nil {
		return Post{}, err
	}
	return records[0], nil
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

func (repository *MySQLRepository) ExistsWithEpoch(ctx context.Context, postID uint64) (bool, string, error) {
	var exists bool
	var epoch string
	err := repository.database.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM posts WHERE id=?),
       COALESCE((SELECT data_epoch FROM business_runtime_state WHERE singleton=1 AND status='ready'), '')`, postID).Scan(&exists, &epoch)
	if err != nil {
		return false, "", fmt.Errorf("检查帖子是否存在并读取数据 epoch: %w", err)
	}
	if businessstate.ValidateEpoch(epoch) != nil {
		epoch = ""
	}
	return exists, epoch, nil
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
		&projection.EditedAt,
		&projection.ContentRevision,
		&projection.Author.ID,
		&projection.Author.Username,
		&projection.Author.DisplayName,
		&projection.CommentCount,
		&projection.LikeCount,
	)
	return projection, err
}

func scanPost(scan scanFunc, extra ...any) (Post, error) {
	var record Post
	destinations := []any{
		&record.ID,
		&record.Title,
		&record.Content,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.EditedAt,
		&record.ContentRevision,
		&record.Author.ID,
		&record.Author.Username,
		&record.Author.DisplayName,
		&record.CommentCount,
		&record.LikeCount,
		&record.LikedByMe,
		&record.Author.Following,
		&record.BookmarkedByMe,
		&record.BookmarkCreatedAt,
	}
	err := scan(append(destinations, extra...)...)
	return record, err
}

// FindAuthorIDForUpdate reads and locks the post author inside the caller's
// transaction. The author is the recipient for comment and like events.
func FindAuthorIDForUpdate(ctx context.Context, database RowQueryer, postID uint64) (uint64, error) {
	if database == nil || postID == 0 {
		return 0, errors.New("find post author: invalid argument")
	}
	var authorID uint64
	if err := database.QueryRowContext(ctx,
		`SELECT author_id FROM posts WHERE id = ? FOR UPDATE`,
		postID,
	).Scan(&authorID); errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	} else if err != nil {
		return 0, fmt.Errorf("find post author: %w", err)
	}
	return authorID, nil
}

// FindViewerProjection 在读取作者和访问者状态时核对正文版本和数据 epoch。启用计数缓存时，用同一 epoch 读取计数。
func (repository *MySQLRepository) FindViewerProjection(ctx context.Context, expected ReadContext, viewer uint64) (projection ViewerProjection, err error) {

	var epoch, status string
	var author, revision uint64
	countsEpoch := ""
	if repository.counts != nil {
		countsEpoch = expected.Epoch
	}
	query := fmt.Sprintf(`
 SELECT e.data_epoch,e.status,p.author_id,p.content_revision,u.id,u.username,u.display_name,
 %s,
 EXISTS(SELECT 1 FROM post_likes l WHERE l.post_id=p.id AND l.user_id=?),
 EXISTS(SELECT 1 FROM user_follows f WHERE f.followed_id=p.author_id AND f.follower_id=?),
 EXISTS(SELECT 1 FROM post_bookmarks b WHERE b.post_id=p.id AND b.user_id=?)
 FROM business_runtime_state e JOIN posts p ON p.id=? JOIN users u ON u.id=p.author_id WHERE e.singleton=1`, countColumns(countsEpoch))
	err = repository.database.QueryRowContext(ctx, query, viewer, viewer, viewer, expected.PostID).Scan(&epoch, &status, &author, &revision, &projection.Author.ID, &projection.Author.Username, &projection.Author.DisplayName, &projection.CommentCount, &projection.LikeCount, &projection.Liked, &projection.Following, &projection.Bookmarked)
	if errors.Is(err, sql.ErrNoRows) {
		return ViewerProjection{}, ErrNotFound
	}
	if err != nil {
		return ViewerProjection{}, fmt.Errorf("find viewer projection: %w", err)
	}
	if epoch != expected.Epoch || status != businessstate.StatusReady || author != expected.AuthorID || revision != expected.ContentRevision {
		return ViewerProjection{}, ErrReadContextChanged
	}
	if countsEpoch != "" {
		values, err := repository.getCounts(ctx, countsEpoch, []uint64{expected.PostID})
		if err != nil {
			return ViewerProjection{}, err
		}
		counts := values[expected.PostID]
		projection.CommentCount, projection.LikeCount = *counts.Comments, *counts.Likes
	}
	return projection, nil
}
