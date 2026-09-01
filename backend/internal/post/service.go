package post

import (
	"context"
	"errors"
	"log"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

type Service struct {
	repository Repository
	cache      DetailCache
}

// NewService creates the post application service. Passing one cache enables
// cache-aside only for detail reads; omitting it keeps the MySQL-only behavior.
func NewService(repository Repository, caches ...DetailCache) *Service {
	service := &Service{repository: repository}
	if len(caches) > 0 {
		service.cache = caches[0]
	}
	return service
}

func (service *Service) Create(ctx context.Context, authorID uint64, input CreateInput) (Post, error) {
	normalized, err := NormalizeCreateInput(input)
	if errors.Is(err, ErrInvalidTitle) {
		return Post{}, validationError("title must contain between 1 and 120 characters")
	}
	if errors.Is(err, ErrInvalidContent) {
		return Post{}, validationError("content must contain between 1 and 10000 characters")
	}
	if err != nil {
		return Post{}, apperror.WrapInternal(err)
	}

	record, err := service.repository.Create(ctx, authorID, normalized.Title, normalized.Content)
	if err != nil {
		return Post{}, apperror.WrapInternal(err)
	}
	return record, nil
}

func (service *Service) List(ctx context.Context, viewerID uint64, options ListOptions) (Page, error) {
	if options.Limit < 1 || options.Limit > MaximumLimit {
		return Page{}, validationError("limit must be an integer between 1 and 50")
	}
	if options.Cursor != nil && (options.Cursor.ID == 0 || options.Cursor.CreatedAt.IsZero()) {
		return Page{}, validationError("cursor is invalid")
	}
	records, err := service.repository.List(ctx, viewerID, options)
	if err != nil {
		return Page{}, apperror.WrapInternal(err)
	}

	page := Page{Posts: records}
	if len(records) <= options.Limit {
		return page, nil
	}

	page.Posts = records[:options.Limit]
	last := page.Posts[len(page.Posts)-1]
	nextCursor, err := EncodeCursor(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	if err != nil {
		return Page{}, apperror.WrapInternal(err)
	}
	page.NextCursor = &nextCursor
	return page, nil
}

func (service *Service) Detail(ctx context.Context, postID, viewerID uint64) (Post, error) {
	projection, hit := service.cachedProjection(ctx, postID)
	if !hit {
		var err error
		projection, err = service.repository.FindPublicByID(ctx, postID)
		if errors.Is(err, ErrNotFound) {
			return Post{}, apperror.New(apperror.CodePostNotFound, "post not found")
		}
		if err != nil {
			return Post{}, apperror.WrapInternal(err)
		}
		if service.cache != nil {
			if err := service.cache.Set(ctx, projection); err != nil {
				log.Printf("post detail cache fill failed: post_id=%d", postID)
			}
		}
	}

	likedByMe, err := service.repository.LikedByViewer(ctx, postID, viewerID)
	if err != nil {
		return Post{}, apperror.WrapInternal(err)
	}
	return projection.post(likedByMe), nil
}

func (service *Service) cachedProjection(ctx context.Context, postID uint64) (PublicProjection, bool) {
	if service.cache == nil {
		return PublicProjection{}, false
	}
	projection, hit, err := service.cache.Get(ctx, postID)
	if err != nil {
		log.Printf("post detail cache read failed: post_id=%d", postID)
		return PublicProjection{}, false
	}
	return projection, hit
}

// RequireExists exposes the shared post-existence boundary used by dependent
// fact services without loading the complete viewer-specific post read model.
func (service *Service) RequireExists(ctx context.Context, postID uint64) error {
	exists, err := service.repository.Exists(ctx, postID)
	if err != nil {
		return apperror.WrapInternal(err)
	}
	if !exists {
		return apperror.New(apperror.CodePostNotFound, "post not found")
	}
	return nil
}
