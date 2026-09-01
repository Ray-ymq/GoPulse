package comment

import (
	"context"
	"errors"
	"log"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

type postExistence interface {
	RequireExists(context.Context, uint64) error
}

type detailCacheInvalidator interface {
	Invalidate(context.Context, uint64) error
}

type Service struct {
	repository Repository
	posts      postExistence
	cache      detailCacheInvalidator
}

func NewService(repository Repository, posts postExistence, caches ...detailCacheInvalidator) *Service {
	service := &Service{repository: repository, posts: posts}
	if len(caches) > 0 {
		service.cache = caches[0]
	}
	return service
}

func (service *Service) Create(ctx context.Context, postID, authorID uint64, input CreateInput) (Comment, error) {
	normalized, err := NormalizeCreateInput(input)
	if errors.Is(err, ErrInvalidContent) {
		return Comment{}, validationError("content must contain between 1 and 2000 characters")
	}
	if err != nil {
		return Comment{}, apperror.WrapInternal(err)
	}
	if err := service.posts.RequireExists(ctx, postID); err != nil {
		return Comment{}, err
	}

	record, err := service.repository.Create(ctx, postID, authorID, normalized.Content)
	if err != nil {
		return Comment{}, apperror.WrapInternal(err)
	}
	service.invalidatePostDetail(ctx, postID)
	return record, nil
}

func (service *Service) invalidatePostDetail(ctx context.Context, postID uint64) {
	if service.cache == nil {
		return
	}
	if err := service.cache.Invalidate(ctx, postID); err != nil {
		log.Printf("post detail cache invalidation failed after comment: post_id=%d", postID)
	}
}

func (service *Service) List(ctx context.Context, postID uint64, options ListOptions) (Page, error) {
	if options.Limit < 1 || options.Limit > MaximumLimit {
		return Page{}, validationError("limit must be an integer between 1 and 50")
	}
	if options.Cursor != nil && options.Cursor.ID == 0 {
		return Page{}, validationError("cursor is invalid")
	}
	if err := service.posts.RequireExists(ctx, postID); err != nil {
		return Page{}, err
	}

	records, err := service.repository.List(ctx, postID, options)
	if err != nil {
		return Page{}, apperror.WrapInternal(err)
	}
	page := Page{Comments: records}
	if len(records) <= options.Limit {
		return page, nil
	}

	page.Comments = records[:options.Limit]
	nextCursor, err := EncodeCursor(Cursor{ID: page.Comments[len(page.Comments)-1].ID})
	if err != nil {
		return Page{}, apperror.WrapInternal(err)
	}
	page.NextCursor = &nextCursor
	return page, nil
}
