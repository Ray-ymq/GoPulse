package post

import (
	"strings"
	"testing"
)

func TestListQueriesUseTheirOwnFiltersAndCursorOrder(t *testing.T) {
	for _, tc := range []struct {
		name         string
		options      ListOptions
		want, absent string
	}{
		{"global", ListOptions{Limit: 2}, "FORCE INDEX (idx_posts_created_at_id)", "feed_authors"},
		{"author", ListOptions{Limit: 2, AuthorID: 7}, "AND p.author_id = ?", "FORCE INDEX"},
		{"following", ListOptions{Limit: 2, Following: true}, "AS feed_authors ON feed_authors.followed_id=p.author_id", "FORCE INDEX"},
		{"bookmark", ListOptions{Limit: 2, Bookmarks: true}, "ORDER BY b.created_at DESC, p.id DESC", "FORCE INDEX"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, args := listStatement(4, tc.options, false)
			if !strings.Contains(query, tc.want) || strings.Contains(query, tc.absent) || strings.Count(query, "?") != len(args) {
				t.Fatalf("query=%s args=%v", query, args)
			}
		})
	}
}

func TestCachedListStatementsReadEpochWithViewerProjection(t *testing.T) {
	query, args := listStatement(4, ListOptions{Limit: 20}, true)
	if strings.Contains(query, "COUNT(") || !strings.Contains(query, "SELECT data_epoch") ||
		!strings.Contains(query, "p.content_revision") || !strings.Contains(query, "liked_by_me") ||
		strings.Count(query, "?") != len(args) {
		t.Fatalf("计数缓存启用时，列表须在同一条 SQL 中读取帖子、访问者状态和 epoch。SQL = %s", query)
	}
}
