package comment

import (
	"context"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/platform/readcache"
)

func (service *Service) WithPageCache(scope *readcache.Scope) {
	service.pages = readcache.New(scope, "comment:first-page:v1", 5*time.Second, validCachedPage)
}

func (service *Service) InvalidatePage(ctx context.Context, epoch string, postID uint64) {
	if service.pages != nil {
		service.pages.Invalidate(ctx, epoch, postID)
	}
}

func (service *Service) requirePost(ctx context.Context, postID uint64, withEpoch bool) (string, error) {
	if withEpoch {
		if posts, ok := service.posts.(interface {
			RequireExistsWithEpoch(context.Context, uint64) (string, error)
		}); ok {
			return posts.RequireExistsWithEpoch(ctx, postID)
		}
	}
	return "", service.posts.RequireExists(ctx, postID)
}

func (service *Service) firstPage(ctx context.Context, epoch string, postID uint64, options ListOptions) (Page, bool, error) {
	repository, ok := service.repository.(interface {
		list(context.Context, uint64, ListOptions, string) ([]Comment, error)
	})
	if !ok {
		return Page{}, false, nil
	}
	return service.pages.FetchOne(ctx, epoch, postID, func(ctx context.Context) (Page, bool, error) {
		records, err := repository.list(ctx, postID, options, epoch)
		if err != nil {
			return Page{}, false, err
		}
		page, err := pageFromComments(records, options.Limit)
		return page, err == nil, err
	})
}

func validCachedPage(postID uint64, page Page) bool {
	if page.Comments == nil || len(page.Comments) > DefaultLimit {
		return false
	}
	for i, record := range page.Comments {
		if record.ID == 0 || record.PostID != postID || record.Author.ID == 0 || record.Author.Username == "" ||
			record.CreatedAt.IsZero() || record.Content == "" || (i > 0 && page.Comments[i-1].ID <= record.ID) {
			return false
		}
	}
	if page.NextCursor == nil {
		return true
	}
	cursor, err := DecodeCursor(*page.NextCursor)
	return err == nil && len(page.Comments) == DefaultLimit && cursor.ID == page.Comments[DefaultLimit-1].ID
}
