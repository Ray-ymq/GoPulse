package comment

import (
	"context"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/platform/readcache"
	"github.com/Ray-ymq/GoPulse/backend/internal/shared/apperror"
)

type postExistence interface {
	RequireExists(context.Context, uint64) error
}

type Service struct {
	repository       Repository
	posts            postExistence
	invalidateCounts func(context.Context, uint64)
	pages            *readcache.Cache[uint64, Page]
}

func NewService(repository Repository, posts postExistence) *Service {
	return &Service{repository: repository, posts: posts}
}

func (service *Service) WithCountInvalidation(invalidate func(context.Context, uint64)) *Service {
	service.invalidateCounts = invalidate
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
	epoch, err := service.requirePost(ctx, postID, service.pages != nil)
	if err != nil {
		return Comment{}, err
	}

	record, err := service.repository.Create(ctx, postID, authorID, normalized.Content)
	if err != nil {
		return Comment{}, apperror.WrapInternal(err)
	}
	if service.invalidateCounts != nil {
		service.invalidateCounts(readcache.WithEpoch(ctx, epoch), postID)
	}
	service.InvalidatePage(ctx, epoch, postID)
	return record, nil
}

func (service *Service) List(ctx context.Context, postID uint64, options ListOptions) (Page, error) {
	if options.Limit < 1 || options.Limit > MaximumLimit {
		return Page{}, validationError("limit must be an integer between 1 and 50")
	}
	if options.Cursor != nil && options.Cursor.ID == 0 {
		return Page{}, validationError("cursor is invalid")
	}
	cacheable := service.pages != nil && options.Limit == DefaultLimit && options.Cursor == nil
	epoch, err := service.requirePost(ctx, postID, cacheable)
	if err != nil {
		return Page{}, err
	}
	if cacheable && epoch != "" {
		page, found, err := service.firstPage(ctx, epoch, postID, options)
		if err == nil && found {
			return page, nil
		}
		if err != nil && !errors.Is(err, errCommentEpochChanged) {
			return Page{}, apperror.WrapInternal(err)
		}
		if err := service.posts.RequireExists(ctx, postID); err != nil {
			return Page{}, err
		}
	}

	records, err := service.repository.List(ctx, postID, options)
	if err != nil {
		return Page{}, apperror.WrapInternal(err)
	}
	return pageFromComments(records, options.Limit)
}

func pageFromComments(records []Comment, limit int) (Page, error) {
	page := Page{Comments: records}
	if len(records) <= limit {
		return page, nil
	}

	page.Comments = records[:limit]
	nextCursor, err := EncodeCursor(Cursor{ID: page.Comments[len(page.Comments)-1].ID})
	if err != nil {
		return Page{}, apperror.WrapInternal(err)
	}
	page.NextCursor = &nextCursor
	return page, nil
}
