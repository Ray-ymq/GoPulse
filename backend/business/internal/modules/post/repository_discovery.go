package post

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/platform/readcache"
)

const discoveryFirstPage = "first"

type BatchBodyCache interface {
	FetchBodies(context.Context, string, []BodyKey, func(context.Context, []BodyKey) (map[BodyKey]Body, error)) (map[BodyKey]Body, error)
}

type discoveryCache struct {
	ids    *readcache.Cache[string, discoveryCandidates]
	bodies BatchBodyCache
}

type discoveryCandidates struct {
	IDs   []uint64 `json:"ids"`
	Count int      `json:"count"`
}

func (repository *MySQLRepository) WithDiscoveryCache(scope *readcache.Scope, bodies BatchBodyCache) {
	repository.discovery = &discoveryCache{
		ids:    readcache.New(scope, "post:discovery:v1", 5*time.Second, validDiscoveryCandidates),
		bodies: bodies,
	}
}

func validDiscoveryCandidates(_ string, candidates discoveryCandidates) bool {
	ids := candidates.IDs
	if ids == nil || len(ids) > DefaultLimit+1 || len(ids) != candidates.Count {
		return false
	}
	seen := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func (repository *MySQLRepository) invalidateDiscovery(ctx context.Context, epoch string) {
	if repository.discovery != nil {
		repository.discovery.ids.Invalidate(ctx, epoch, discoveryFirstPage)
	}
}

func (repository *MySQLRepository) listDiscovery(ctx context.Context, viewerID uint64, options ListOptions) ([]Post, error) {
	epoch, err := repository.discovery.ids.Epoch(ctx)
	if err != nil {
		return repository.listFromMySQL(ctx, viewerID, options)
	}
	candidates, found, err := repository.discovery.ids.FetchOne(ctx, epoch, discoveryFirstPage,
		func(ctx context.Context) (discoveryCandidates, bool, error) {
			ids, err := repository.discoveryIDs(ctx, epoch)
			return discoveryCandidates{IDs: ids, Count: len(ids)}, err == nil, err
		})
	if err == nil && found {
		records, ok, err := repository.hydrateDiscovery(ctx, epoch, viewerID, candidates.IDs)
		if err == nil && ok {
			return records, nil
		}
		if err == nil {
			repository.invalidateDiscovery(ctx, epoch)
		}
	}
	return repository.listFromMySQL(ctx, viewerID, options)
}

func (repository *MySQLRepository) discoveryIDs(ctx context.Context, epoch string) ([]uint64, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT p.id
FROM business_runtime_state e
LEFT JOIN (SELECT id, created_at FROM posts FORCE INDEX (idx_posts_created_at_id)
           ORDER BY created_at DESC, id DESC LIMIT ?) p ON TRUE
WHERE e.singleton=1 AND e.status='ready' AND e.data_epoch=?
ORDER BY p.created_at DESC, p.id DESC`, DefaultLimit+1, epoch)
	if err != nil {
		return nil, fmt.Errorf("查询发现页候选帖子: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := make([]uint64, 0, DefaultLimit+1)
	ready := false
	for rows.Next() {
		ready = true
		var id sql.Null[uint64]
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id.Valid {
			ids = append(ids, id.V)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !ready {
		return nil, ErrReadContextChanged
	}
	return ids, nil
}

func (repository *MySQLRepository) hydrateDiscovery(ctx context.Context, epoch string, viewerID uint64, ids []uint64) ([]Post, bool, error) {
	records := make([]Post, 0, len(ids))
	if len(ids) == 0 {
		return records, true, nil
	}
	slots := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	versionArgs := make([]any, 0, 1+len(ids))
	versionArgs = append(versionArgs, epoch)
	for _, id := range ids {
		versionArgs = append(versionArgs, id)
	}
	versions, err := repository.discoveryVersions(ctx, slots, versionArgs)
	if err != nil || len(versions) != len(ids) {
		return nil, false, err
	}
	keys := make([]BodyKey, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, BodyKey{Epoch: epoch, PostID: id, ContentRevision: versions[id]})
	}
	bodies, err := repository.discovery.bodies.FetchBodies(ctx, epoch, keys, func(ctx context.Context, missing []BodyKey) (map[BodyKey]Body, error) {
		return repository.discoveryBodies(ctx, epoch, missing)
	})
	if err != nil || len(bodies) != len(keys) {
		return nil, false, err
	}
	for _, key := range keys {
		body := bodies[key]
		records = append(records, Post{ID: body.ID, Title: body.Title, Content: body.Content,
			CreatedAt: body.CreatedAt, UpdatedAt: body.UpdatedAt, EditedAt: body.EditedAt,
			ContentRevision: body.ContentRevision, Author: Author{ID: body.AuthorID}, BookmarkCreatedAt: body.CreatedAt})
	}
	if err := repository.hydrateCounts(ctx, epoch, records); err != nil {
		return nil, false, err
	}
	verified, err := repository.hydrateDiscoveryViewers(ctx, viewerID, records, slots, versionArgs)
	return records, verified, err
}

func (repository *MySQLRepository) discoveryVersions(ctx context.Context, slots string, args []any) (map[uint64]uint64, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT p.id, p.content_revision FROM posts p JOIN business_runtime_state e
ON e.singleton=1 AND e.status='ready' AND e.data_epoch=?
WHERE p.id IN (`+slots+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("查询发现页正文版本: %w", err)
	}
	defer func() { _ = rows.Close() }()
	versions := make(map[uint64]uint64)
	for rows.Next() {
		var id, revision uint64
		if err := rows.Scan(&id, &revision); err != nil {
			return nil, err
		}
		versions[id] = revision
	}
	return versions, rows.Err()
}

func (repository *MySQLRepository) discoveryBodies(ctx context.Context, epoch string, keys []BodyKey) (map[BodyKey]Body, error) {
	tuples := strings.TrimSuffix(strings.Repeat("(?,?),", len(keys)), ",")
	args := make([]any, 0, 1+len(keys)*2)
	args = append(args, epoch)
	for _, key := range keys {
		args = append(args, key.PostID, key.ContentRevision)
	}
	rows, err := repository.database.QueryContext(ctx, `
SELECT p.id, p.author_id, p.title, p.content, p.created_at, p.updated_at, p.edited_at, p.content_revision
FROM posts p JOIN business_runtime_state e ON e.singleton=1 AND e.status='ready' AND e.data_epoch=?
WHERE (p.id,p.content_revision) IN (`+tuples+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("查询发现页正文: %w", err)
	}
	defer func() { _ = rows.Close() }()
	bodies := make(map[BodyKey]Body, len(keys))
	for rows.Next() {
		var body Body
		if err := rows.Scan(&body.ID, &body.AuthorID, &body.Title, &body.Content, &body.CreatedAt, &body.UpdatedAt, &body.EditedAt, &body.ContentRevision); err != nil {
			return nil, err
		}
		key := BodyKey{Epoch: epoch, PostID: body.ID, ContentRevision: body.ContentRevision}
		bodies[key] = body
	}
	return bodies, rows.Err()
}

func (repository *MySQLRepository) hydrateDiscoveryViewers(ctx context.Context, viewerID uint64, records []Post, slots string, versionArgs []any) (bool, error) {
	args := append([]any{viewerID, viewerID, viewerID}, versionArgs...)
	rows, err := repository.database.QueryContext(ctx, `
SELECT p.id, p.content_revision, u.id, u.username, u.display_name,
    EXISTS(SELECT 1 FROM post_likes l WHERE l.post_id=p.id AND l.user_id=?),
    EXISTS(SELECT 1 FROM user_follows f WHERE f.followed_id=p.author_id AND f.follower_id=?),
    EXISTS(SELECT 1 FROM post_bookmarks b WHERE b.post_id=p.id AND b.user_id=?)
FROM posts p JOIN users u ON u.id=p.author_id
JOIN business_runtime_state e ON e.singleton=1 AND e.status='ready' AND e.data_epoch=?
WHERE p.id IN (`+slots+`)`, args...)
	if err != nil {
		return false, fmt.Errorf("查询发现页访问者互动状态: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byID := make(map[uint64]*Post, len(records))
	for i := range records {
		byID[records[i].ID] = &records[i]
	}
	verified := 0
	for rows.Next() {
		var id, revision uint64
		var author Author
		var liked, bookmarked bool
		if err := rows.Scan(&id, &revision, &author.ID, &author.Username, &author.DisplayName, &liked, &author.Following, &bookmarked); err != nil {
			return false, err
		}
		record := byID[id]
		if record == nil || record.ContentRevision != revision || record.Author.ID != author.ID {
			return false, nil
		}
		record.Author, record.LikedByMe, record.BookmarkedByMe = author, liked, bookmarked
		verified++
	}
	return verified == len(records), rows.Err()
}
