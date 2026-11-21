package post

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/platform/readcache"
)

type interactionCounts struct {
	Comments *uint64 `json:"comments"`
	Likes    *uint64 `json:"likes"`
}

func (repository *MySQLRepository) WithCountCache(scope *readcache.Scope) {
	repository.counts = readcache.New(scope, "post:counts:v1", 5*time.Second, func(_ uint64, counts interactionCounts) bool {
		return counts.Comments != nil && counts.Likes != nil
	})
}

func (repository *MySQLRepository) InvalidateCounts(ctx context.Context, postID uint64) {
	repository.invalidateCounts(ctx, "", postID)
}

func (repository *MySQLRepository) invalidateCounts(ctx context.Context, epoch string, postID uint64) {
	if repository.counts != nil {
		repository.counts.Invalidate(ctx, epoch, postID)
	}
}

func (repository *MySQLRepository) countsEpoch(ctx context.Context) string {
	if repository.counts == nil {
		return ""
	}
	epoch, err := repository.counts.Epoch(ctx)
	if err != nil {
		return ""
	}
	return epoch
}

func (repository *MySQLRepository) getCounts(ctx context.Context, epoch string, ids []uint64) (map[uint64]interactionCounts, error) {
	if repository.counts == nil {
		return repository.countInMySQL(ctx, epoch, ids)
	}
	return repository.counts.Fetch(ctx, epoch, ids, func(ctx context.Context, missing []uint64) (map[uint64]interactionCounts, error) {
		return repository.countInMySQL(ctx, epoch, missing)
	})
}

func (repository *MySQLRepository) hydrateCounts(ctx context.Context, epoch string, posts []Post) error {
	if epoch == "" || len(posts) == 0 {
		return nil
	}
	ids := make([]uint64, len(posts))
	for i, record := range posts {
		ids[i] = record.ID
	}
	counts, err := repository.getCounts(ctx, epoch, ids)
	if err != nil {
		return err
	}
	for i := range posts {
		value := counts[posts[i].ID]
		posts[i].CommentCount, posts[i].LikeCount = *value.Comments, *value.Likes
	}
	return nil
}

func (repository *MySQLRepository) countInMySQL(ctx context.Context, epoch string, ids []uint64) (map[uint64]interactionCounts, error) {
	slots := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, 1+2*len(ids))
	args = append(args, epoch)
	for range 2 {
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := repository.database.QueryContext(ctx, `
WITH ready_epoch AS (
	SELECT 1 FROM business_runtime_state WHERE singleton=1 AND status='ready' AND data_epoch=?
)
SELECT 0, 0, 0 FROM ready_epoch
UNION ALL
SELECT 1, c.post_id, COUNT(*) FROM comments c
	WHERE EXISTS (SELECT 1 FROM ready_epoch) AND c.post_id IN (`+slots+`) GROUP BY c.post_id
UNION ALL
SELECT 2, l.post_id, COUNT(*) FROM post_likes l
	WHERE EXISTS (SELECT 1 FROM ready_epoch) AND l.post_id IN (`+slots+`) GROUP BY l.post_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("统计帖子互动数: %w", err)
	}
	defer func() { _ = rows.Close() }()
	counts := make(map[uint64]interactionCounts, len(ids))
	var zero uint64
	for _, id := range ids {
		counts[id] = interactionCounts{Comments: &zero, Likes: &zero}
	}
	var ready bool
	for rows.Next() {
		var kind int
		var id, count uint64
		if err := rows.Scan(&kind, &id, &count); err != nil {
			return nil, fmt.Errorf("读取帖子互动数: %w", err)
		}
		if kind == 0 {
			ready = true
			continue
		}
		value := counts[id]
		if kind == 1 {
			value.Comments = &count
		} else {
			value.Likes = &count
		}
		counts[id] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取帖子互动数: %w", err)
	}
	if !ready {
		return nil, ErrReadContextChanged
	}
	return counts, nil
}
